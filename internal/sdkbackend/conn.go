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
	h := C.RfcOpenConnection(paramsPtr, C.uint(len(cParams)), &info)
	if h == nil {
		return nil, errFromInfo(&info, "RfcOpenConnection")
	}
	c := &connHandle{}
	setSDKConnPtr(c, h)
	return c, nil
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
//
// SDK function: RfcPing (✅ confirmed; 7.50 PL3+).
func pingConn(ctx context.Context, c *connHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var info C.RFC_ERROR_INFO
	rc := C.RfcPing(sdkConnPtr(c), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcPing")
	}
	return nil
}

// resetConn implements [backend.Backend.Reset] over
// RfcResetServerContext.
//
// SDK function: RfcResetServerContext (✅ confirmed; 7.50 PL3+).
func resetConn(c *connHandle) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var info C.RFC_ERROR_INFO
	rc := C.RfcResetServerContext(sdkConnPtr(c), &info)
	if rc != C.RFC_OK {
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
