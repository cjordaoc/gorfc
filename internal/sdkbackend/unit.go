// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// bgRFC client bindings.
//
// bgRFC supersedes tRFC/qRFC on systems where SBGRFCCONF is
// configured. The API surface is broader than the legacy path:
// units carry a list of queue names (not a single one), the
// backend distinguishes synchronous (type 'T', no queues) from
// asynchronous (type 'Q', with queues) lifecycles, and the
// caller manipulates a UnitIdentifier instead of just a TID.
//
// Per sapnwrfc.h §2261:
//
//	RFC_UNIT_HANDLE RfcCreateUnit(
//	    RFC_CONNECTION_HANDLE rfcHandle,
//	    RFC_UNITID uid,
//	    SAP_UC const *queueNames[],
//	    unsigned queueNameCount,
//	    const RFC_UNIT_ATTRIBUTES *unitAttr,
//	    RFC_UNIT_IDENTIFIER *identifier,
//	    RFC_ERROR_INFO *errorInfo)

package sdkbackend

/*
#include "helpers.h"

// fillUnitID copies a 32-char ASCII unit ID into a SAP_UC[33]
// buffer in place, using the SDK's own conversion. Used by
// CreateUnit so we do not allocate (the SDK reads from the
// caller-provided RFC_UNITID slot).
//
// Implemented inline because SAP_UC arrays cross the cgo
// boundary as fixed-size: encoding has to happen in one shot.
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// unitHandle is the per-unit state owned by the backend.
type unitHandle struct {
	id     backend.UnitHandle
	connID backend.ConnHandle
	sdkPtr uintptr // C.RFC_UNIT_HANDLE
	// identifier is the (type, uid) pair the SDK populated at
	// create time. We retain it so subsequent Confirm/State
	// calls don't depend on the caller round-tripping it
	// correctly through the public API.
	identifier backend.UnitIdentifier
	mu         sync.Mutex
}

type unitRegistry struct {
	mu   sync.Mutex
	next uint64
	m    map[backend.UnitHandle]*unitHandle
}

func (r *unitRegistry) put(u *unitHandle) backend.UnitHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id := backend.UnitHandle(r.next)
	u.id = id
	r.m[id] = u
	return id
}

func (r *unitRegistry) get(id backend.UnitHandle) (*unitHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.m[id]
	if !ok {
		return nil, fmt.Errorf("nwrfc/sdkbackend: unknown unit handle %d", id)
	}
	return u, nil
}

func (r *unitRegistry) remove(id backend.UnitHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

var units = unitRegistry{m: make(map[backend.UnitHandle]*unitHandle)}

func sdkUnitPtr(u *unitHandle) C.RFC_UNIT_HANDLE {
	return (C.RFC_UNIT_HANDLE)(unsafe.Pointer(u.sdkPtr))
}

// CreateUnit implements [backend.BgRFCBackend.CreateUnit].
//
// SDK function: RfcCreateUnit (sapnwrfc.h §2261).
func (b *sdkBackend) CreateUnit(h backend.ConnHandle, uid string, queues []string) (backend.UnitHandle, backend.UnitIdentifier, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return 0, backend.UnitIdentifier{}, err
	}
	if len(uid) != 32 {
		return 0, backend.UnitIdentifier{}, fmt.Errorf("nwrfc/sdkbackend: UnitID must be exactly 32 chars, got %d", len(uid))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	uidUC, err := stringToSAPUC(uid)
	if err != nil {
		return 0, backend.UnitIdentifier{}, err
	}
	defer C.goFreeU(uidUC)

	// Encode queue list. Each queue gets its own SAP_UC buffer;
	// we collect them so we can free after the SDK call.
	// queueNamesPtr is a (SAP_UC**) → cgo wants []*C.SAP_UC.
	var queuePtrs []*C.SAP_UC
	for _, q := range queues {
		if q == "" {
			return 0, backend.UnitIdentifier{}, fmt.Errorf("nwrfc/sdkbackend: empty queue name in queues[%d]", len(queuePtrs))
		}
		qUC, err := stringToSAPUC(q)
		if err != nil {
			for _, p := range queuePtrs {
				C.goFreeU(p)
			}
			return 0, backend.UnitIdentifier{}, err
		}
		queuePtrs = append(queuePtrs, qUC)
	}
	defer func() {
		for _, p := range queuePtrs {
			C.goFreeU(p)
		}
	}()

	var queueArr **C.SAP_UC
	if len(queuePtrs) > 0 {
		queueArr = (**C.SAP_UC)(unsafe.Pointer(&queuePtrs[0]))
	}

	// Zero-initialized RFC_UNIT_ATTRIBUTES: per sapnwrfc.h
	// §283-§285 the SDK fills sensible defaults for empty
	// fields (current OS user, "000" client, etc.). The bgRFC
	// scheduler accepts this without complaint.
	var attr C.RFC_UNIT_ATTRIBUTES
	var ident C.RFC_UNIT_IDENTIFIER
	var info C.RFC_ERROR_INFO

	uHandle := C.RfcCreateUnit(
		sdkConnPtr(c),
		(*C.SAP_UC)(unsafe.Pointer(uidUC)),
		queueArr,
		C.uint(len(queuePtrs)),
		&attr,
		&ident,
		&info,
	)
	if uHandle == nil {
		return 0, backend.UnitIdentifier{}, errFromInfo(&info, "RfcCreateUnit")
	}

	// Decode the SDK-populated UnitIdentifier.
	idStr := sapUCSliceToString(ident.unitID[:])
	// unitType is a single SAP_UC code unit ('T' or 'Q'). For
	// ASCII letters the low byte equals the rune.
	idType := byte(ident.unitType)
	if idType == 0 {
		// Some compilers see unitType as a 2-byte SAP_UC; use
		// the conversion via sapUCSliceToString of length 1.
		s := sapUCSliceToString([]C.SAP_UC{ident.unitType})
		if len(s) > 0 {
			idType = s[0]
		}
	}

	u := &unitHandle{
		connID: h,
		sdkPtr: uintptr(unsafe.Pointer(uHandle)),
		identifier: backend.UnitIdentifier{
			Type: idType,
			ID:   idStr,
		},
	}
	return units.put(u), u.identifier, nil
}

// InvokeInUnit implements [backend.BgRFCBackend.InvokeInUnit].
//
// SDK function: RfcInvokeInUnit (sapnwrfc.h §2280).
func (b *sdkBackend) InvokeInUnit(ctx context.Context, h backend.ConnHandle, uHandle backend.UnitHandle, fn string, in backend.CallParams, opts backend.InvokeOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	u, err := units.get(uHandle)
	if err != nil {
		return err
	}
	if u.connID != h {
		return fmt.Errorf("nwrfc/sdkbackend: unit %d belongs to a different connection", uHandle)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	u.mu.Lock()
	defer u.mu.Unlock()

	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return err
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	desc := C.RfcGetFunctionDesc(sdkConnPtr(c), nameUC, &info)
	if desc == nil {
		return errFromInfo(&info, "RfcGetFunctionDesc("+fn+")")
	}

	fh := C.RfcCreateFunction(desc, &info)
	if fh == nil {
		return errFromInfo(&info, "RfcCreateFunction("+fn+")")
	}
	defer C.RfcDestroyFunction(fh, &info)

	if err := fillFunctionParameters(fh, desc, in); err != nil {
		return err
	}

	for _, name := range opts.NotRequested {
		uc, err := stringToSAPUC(name)
		if err != nil {
			return err
		}
		rc := C.goSetParamActive(fh, uc, 0, &info)
		C.goFreeU(uc)
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetParameterActive("+name+")")
		}
	}

	cleanup := withCancelWatcher(ctx, c)
	rc := C.RfcInvokeInUnit(sdkUnitPtr(u), fh, &info)
	cleanup()

	if rc != C.RFC_OK {
		if err := ctxErrorIfFired(ctx, "RfcInvokeInUnit("+fn+")"); err != nil {
			return err
		}
		return errFromInfo(&info, "RfcInvokeInUnit("+fn+")")
	}
	return nil
}

// SubmitUnit implements [backend.BgRFCBackend.SubmitUnit].
//
// SDK function: RfcSubmitUnit (sapnwrfc.h §2303).
func (b *sdkBackend) SubmitUnit(uHandle backend.UnitHandle) error {
	u, err := units.get(uHandle)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	var info C.RFC_ERROR_INFO
	rc := C.RfcSubmitUnit(sdkUnitPtr(u), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSubmitUnit")
	}
	return nil
}

// ConfirmUnit implements [backend.BgRFCBackend.ConfirmUnit].
//
// SDK function: RfcConfirmUnit (sapnwrfc.h §2331).
func (b *sdkBackend) ConfirmUnit(h backend.ConnHandle, id backend.UnitIdentifier) error {
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	if len(id.ID) != 32 {
		return fmt.Errorf("nwrfc/sdkbackend: UnitID must be exactly 32 chars, got %d", len(id.ID))
	}
	if id.Type != 'T' && id.Type != 'Q' {
		return fmt.Errorf("nwrfc/sdkbackend: UnitIdentifier.Type=%q must be 'T' or 'Q'", id.Type)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cid, err := goUnitIdentifierToC(id)
	if err != nil {
		return err
	}

	var info C.RFC_ERROR_INFO
	rc := C.RfcConfirmUnit(sdkConnPtr(c), &cid, &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcConfirmUnit")
	}
	return nil
}

// DestroyUnit implements [backend.BgRFCBackend.DestroyUnit].
// Removes the entry from the registry on success.
//
// SDK function: RfcDestroyUnit (sapnwrfc.h §2342).
func (b *sdkBackend) DestroyUnit(uHandle backend.UnitHandle) error {
	u, err := units.get(uHandle)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	var info C.RFC_ERROR_INFO
	rc := C.RfcDestroyUnit(sdkUnitPtr(u), &info)
	units.remove(uHandle)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcDestroyUnit")
	}
	return nil
}

// GetUnitState implements [backend.BgRFCBackend.GetUnitState].
//
// SDK function: RfcGetUnitState (sapnwrfc.h §2357).
func (b *sdkBackend) GetUnitState(h backend.ConnHandle, id backend.UnitIdentifier) (backend.UnitState, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return backend.UnitStateUnknown, err
	}
	if len(id.ID) != 32 {
		return backend.UnitStateUnknown, fmt.Errorf("nwrfc/sdkbackend: UnitID must be exactly 32 chars, got %d", len(id.ID))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cid, err := goUnitIdentifierToC(id)
	if err != nil {
		return backend.UnitStateUnknown, err
	}

	var state C.RFC_UNIT_STATE
	var info C.RFC_ERROR_INFO
	rc := C.RfcGetUnitState(sdkConnPtr(c), &cid, &state, &info)
	if rc != C.RFC_OK {
		return backend.UnitStateUnknown, errFromInfo(&info, "RfcGetUnitState")
	}
	return backend.UnitState(state), nil
}

// goUnitIdentifierToC builds a C.RFC_UNIT_IDENTIFIER from the
// Go-side struct. The C struct has a fixed-size SAP_UC[33]
// embedded array for the unit ID; we need to encode our 32-char
// hex string into it without allocating heap memory the SDK
// would later read past the call.
func goUnitIdentifierToC(id backend.UnitIdentifier) (C.RFC_UNIT_IDENTIFIER, error) {
	var cid C.RFC_UNIT_IDENTIFIER
	cid.unitType = C.SAP_UC(id.Type)

	// Encode id.ID into a temporary SAP_UC buffer, then memcpy
	// into the embedded array. The buffer length matches the
	// SDK's RFC_UNITID_LN+1 = 33.
	idUC, err := stringToSAPUC(id.ID)
	if err != nil {
		return cid, err
	}
	defer C.goFreeU(idUC)

	// The unitID field is a SAP_UC[33]. We copy 33 code units
	// (32 chars + null) by walking pointers.
	dst := (*C.SAP_UC)(unsafe.Pointer(&cid.unitID[0]))
	for i := 0; i < 33; i++ {
		// stringToSAPUC allocates len+1 = 33 code units for a
		// 32-char input, so reading [0..33) is in-bounds.
		src := (*C.SAP_UC)(unsafe.Add(unsafe.Pointer(idUC), uintptr(i)*unsafe.Sizeof(*idUC)))
		*((*C.SAP_UC)(unsafe.Add(unsafe.Pointer(dst), uintptr(i)*unsafe.Sizeof(*dst)))) = *src
	}
	return cid, nil
}
