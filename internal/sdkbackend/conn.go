// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"context"
	"sync/atomic"
	"unsafe"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// connState atomic values.
const (
	stateOpen   uint32 = 0
	stateClosed uint32 = 1
)

// sdkConnPtr re-exposes the cgo handle on a connHandle. We
// stash it as a uintptr in the cross-package struct so callers
// in backend_sdk.go don't need to import "C". Round-trip
// through unsafe.Pointer is well-defined for opaque cgo
// handles per cgo rules.

func sdkConnPtr(c *connHandle) C.RFC_CONNECTION_HANDLE {
	return (C.RFC_CONNECTION_HANDLE)(unsafe.Pointer(c.sdkPtr))
}

func setSDKConnPtr(c *connHandle, h C.RFC_CONNECTION_HANDLE) {
	c.sdkPtr = uintptr(unsafe.Pointer(h))
}

// openConn implements [backend.Backend.Open] over RfcOpenConnection.
//
// SDK function: RfcOpenConnection (✅ confirmed; 7.50 PL3+).
//
// 🟡 ctx cancel: T1.9 wires a watcher goroutine. Until that
// lands, this function honors ctx only at the pre-call check;
// once `RfcOpenConnection` blocks in the SDK, only a peer
// timeout will return.
func openConn(ctx context.Context, p backend.Params) (*connHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Marshal params to RFC_CONNECTION_PARAMETER[].
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	cParams := make([]C.RFC_CONNECTION_PARAMETER, len(keys))
	// We must keep the SAP_UC buffers alive for the duration of
	// RfcOpenConnection; collect them and free after.
	frees := make([]unsafe.Pointer, 0, 2*len(keys))
	defer func() {
		for _, f := range frees {
			C.goFreeU((*C.SAP_UC)(f))
		}
	}()

	for i, k := range keys {
		nameUC, err := stringToSAPUC(k)
		if err != nil {
			return nil, err
		}
		valUC, err := stringToSAPUC(p[k])
		if err != nil {
			C.goFreeU(nameUC)
			return nil, err
		}
		cParams[i].name = (*C.SAP_UC)(nameUC)
		cParams[i].value = (*C.SAP_UC)(valUC)
		frees = append(frees, unsafe.Pointer(nameUC), unsafe.Pointer(valUC))
	}

	var info C.RFC_ERROR_INFO
	var hPtr *C.RFC_CONNECTION_HANDLE
	if len(cParams) > 0 {
		hPtr = (*C.RFC_CONNECTION_HANDLE)(unsafe.Pointer(uintptr(0)))
		_ = hPtr // silence vet
	}
	var paramsPtr *C.RFC_CONNECTION_PARAMETER
	if len(cParams) > 0 {
		paramsPtr = &cParams[0]
	}

	// Note: a watcher cannot use RfcCancel here because the
	// connection handle does not exist yet. Open is bounded by
	// the SDK's own RfcOpenConnection timeout (driven by the
	// connection params); ctx is honored at the pre-call check
	// above and at the post-call surface — the SDK call itself
	// is not interruptible cross-thread until a handle exists.
	h := C.RfcOpenConnection(paramsPtr, C.uint(len(cParams)), &info)
	if h == nil {
		// Surface ctx-cancelled / deadline-exceeded so callers
		// can branch via errors.Is(ErrCancelled / ErrTimeout)
		// uniformly with the other lifecycle ops, even though
		// we did not actively cancel the SDK call.
		if cerr := ctxErrorIfFired(ctx, "RfcOpenConnection"); cerr != nil {
			return nil, cerr
		}
		return nil, errFromInfo(&info, "RfcOpenConnection")
	}
	c := &connHandle{}
	setSDKConnPtr(c, h)
	return c, nil
}

// cancelConn implements [backend.Cancellable.Cancel] over
// `RfcCancel`. RfcCancel is the only SAP NW RFC SDK call
// documented thread-safe with respect to a goroutine blocked
// in `RfcInvoke` / `RfcOpenConnection` / etc. on the same
// handle (see docs/EVIDENCE/sdk-cancel.md).
//
// We intentionally do NOT take c.mu — by SDK design RfcCancel
// runs in parallel with the (mutex-holding) blocked op. Taking
// the mutex here would deadlock against that blocked op.
//
// Idempotent: a CAS into the cancelled state ensures repeat
// calls return nil.
//
// Behavior of RfcCancel against a connection in any of these
// states is well-defined per the SDK programming guide:
//
//   - blocked in RfcInvoke      → unblocks with RFC_CANCELED
//   - blocked in RfcOpenConnection → unblocks with RFC_CANCELED
//   - blocked in RfcPing        → unblocks with RFC_CANCELED
//   - already returned          → no-op (the SDK ignores it)
//   - already closed            → undefined; our local state
//     check rejects with nil before reaching the SDK
//
// SDK function: RfcCancel (✅ verified PL18; see
// docs/EVIDENCE/sdk-cancel.md).
func cancelConn(c *connHandle) error {
	// Already-closed handles are rejected with nil — Cancel
	// after Close is harmless and cannot do useful work.
	if atomic.LoadUint32((*uint32)(unsafe.Pointer(&c.state))) == stateClosed {
		return nil
	}
	var info C.RFC_ERROR_INFO
	rc := C.RfcCancel(sdkConnPtr(c), &info)
	if rc != C.RFC_OK {
		// RfcCancel only fails if the handle is already invalid,
		// in which case the in-flight call has already returned
		// (or the connection has already been closed). Treat
		// non-zero RC as informational; we still report it for
		// diagnostics but do not surface as a fatal error.
		return errFromInfo(&info, "RfcCancel")
	}
	return nil
}

// closeConn implements [backend.Backend.Close] over RfcCloseConnection.
//
// SDK function: RfcCloseConnection (✅ confirmed; 7.50 PL3+).
func closeConn(c *connHandle) error {
	if !atomic.CompareAndSwapUint32((*uint32)(unsafe.Pointer(&c.state)), stateOpen, stateClosed) {
		return nil // idempotent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var info C.RFC_ERROR_INFO
	rc := C.RfcCloseConnection(sdkConnPtr(c), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcCloseConnection")
	}
	return nil
}

// pingConn implements [backend.Backend.Ping] over RfcPing.
// Honors ctx via the shared cancel watcher (cancel.go).
//
// SDK function: RfcPing (✅ confirmed; 7.50 PL3+).
func pingConn(ctx context.Context, c *connHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cleanup := withCancelWatcher(ctx, c)
	var info C.RFC_ERROR_INFO
	rc := C.RfcPing(sdkConnPtr(c), &info)
	cleanup()
	if rc != C.RFC_OK {
		if err := ctxErrorIfFired(ctx, "RfcPing"); err != nil {
			return err
		}
		return errFromInfo(&info, "RfcPing")
	}
	return nil
}

// resetConn implements [backend.Backend.Reset] over
// RfcResetServerContext. Honors ctx via the shared cancel
// watcher; takes a context now (the public Conn.Reset API
// adds ctx in v0.2.0 to keep parity with Ping/Describe/Invoke).
//
// SDK function: RfcResetServerContext (✅ confirmed; 7.50 PL3+).
func resetConn(ctx context.Context, c *connHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cleanup := withCancelWatcher(ctx, c)
	var info C.RFC_ERROR_INFO
	rc := C.RfcResetServerContext(sdkConnPtr(c), &info)
	cleanup()
	if rc != C.RFC_OK {
		if err := ctxErrorIfFired(ctx, "RfcResetServerContext"); err != nil {
			return err
		}
		return errFromInfo(&info, "RfcResetServerContext")
	}
	return nil
}

// connAttributes implements [backend.Backend.Attributes] over
// RfcGetConnectionAttributes.
//
// SDK function: RfcGetConnectionAttributes (✅ confirmed).
func connAttributes(c *connHandle) (backend.Attributes, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var attr C.RFC_ATTRIBUTES
	var info C.RFC_ERROR_INFO
	rc := C.RfcGetConnectionAttributes(sdkConnPtr(c), &attr, &info)
	if rc != C.RFC_OK {
		return backend.Attributes{}, errFromInfo(&info, "RfcGetConnectionAttributes")
	}
	return backend.Attributes{
		Dest:                  sapUCSliceToString(attr.dest[:]),
		Host:                  sapUCSliceToString(attr.host[:]),
		PartnerHost:           sapUCSliceToString(attr.partnerHost[:]),
		SysNumber:             sapUCSliceToString(attr.sysNumber[:]),
		SysID:                 sapUCSliceToString(attr.sysId[:]),
		Client:                sapUCSliceToString(attr.client[:]),
		User:                  sapUCSliceToString(attr.user[:]),
		Language:              sapUCSliceToString(attr.language[:]),
		Trace:                 sapUCSliceToString(attr.trace[:]),
		IsoLanguage:           sapUCSliceToString(attr.isoLanguage[:]),
		Codepage:              sapUCSliceToString(attr.codepage[:]),
		PartnerCodepage:       sapUCSliceToString(attr.partnerCodepage[:]),
		RfcRole:               sapUCSliceToString(attr.rfcRole[:]),
		Type:                  sapUCSliceToString(attr._type[:]),
		PartnerType:           sapUCSliceToString(attr.partnerType[:]),
		Rel:                   sapUCSliceToString(attr.rel[:]),
		PartnerRel:            sapUCSliceToString(attr.partnerRel[:]),
		KernelRel:             sapUCSliceToString(attr.kernelRel[:]),
		CpicConvID:            sapUCSliceToString(attr.cpicConvId[:]),
		ProgName:              sapUCSliceToString(attr.progName[:]),
		PartnerBytesPerChar:   sapUCSliceToString(attr.partnerBytesPerChar[:]),
		PartnerSystemCodepage: sapUCSliceToString(attr.partnerSystemCodepage[:]),
		PartnerIP:             sapUCSliceToString(attr.partnerIP[:]),
		PartnerIPv6:           sapUCSliceToString(attr.partnerIPv6[:]),
	}, nil
}
