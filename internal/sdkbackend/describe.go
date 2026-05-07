// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include <sapnwrfc.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"sync"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// describeFunction implements [backend.Backend.Describe] using
// RfcGetFunctionDesc + RfcGetParameter*ByIndex iteration.
//
// SDK functions: RfcGetFunctionDesc, RfcGetParameterCount,
// RfcGetParameterDescByIndex, RfcGetTypeName,
// RfcGetFieldCount, RfcGetFieldDescByIndex (✅ confirmed).
//
// 🟡 Memory ownership: RfcGetFunctionDesc returns a handle the
// SDK caches itself; we MUST NOT free it. Verified against the
// programming guide note "Function descriptions are cached and
// shared".
func describeFunction(ctx context.Context, c *connHandle, fn string) (backend.FunctionDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return backend.FunctionDescriptor{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return backend.FunctionDescriptor{}, err
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	fnDesc := C.RfcGetFunctionDesc(sdkConnPtr(c), nameUC, &info)
	if fnDesc == nil {
		return backend.FunctionDescriptor{}, errFromInfo(&info, "RfcGetFunctionDesc("+fn+")")
	}

	var paramCount C.uint
	if rc := C.RfcGetParameterCount(fnDesc, &paramCount, &info); rc != C.RFC_OK {
		return backend.FunctionDescriptor{}, errFromInfo(&info, "RfcGetParameterCount("+fn+")")
	}

	out := backend.FunctionDescriptor{
		Name:       fn,
		Parameters: make([]backend.ParameterDescriptor, 0, paramCount),
	}
	for i := C.uint(0); i < paramCount; i++ {
		var pd C.RFC_PARAMETER_DESC
		if rc := C.RfcGetParameterDescByIndex(fnDesc, i, &pd, &info); rc != C.RFC_OK {
			return backend.FunctionDescriptor{}, errFromInfo(&info, "RfcGetParameterDescByIndex")
		}
		p, err := wrapParameter(&pd)
		if err != nil {
			return backend.FunctionDescriptor{}, err
		}
		out.Parameters = append(out.Parameters, p)
	}
	return out, nil
}

// wrapParameter converts a C.RFC_PARAMETER_DESC to the typed
// backend descriptor.
func wrapParameter(pd *C.RFC_PARAMETER_DESC) (backend.ParameterDescriptor, error) {
	p := backend.ParameterDescriptor{
		Name:      sapUCSliceToString(pd.name[:]),
		Type:      backend.RFCType(pd._type),
		Direction: convertDirection(pd.direction),
		Length:    uint(pd.ucLength),
		Decimals:  uint(pd.decimals),
		Optional:  pd.optional != 0,
	}
	if pd.typeDescHandle != nil && (p.Type == backend.TypeStructure || p.Type == backend.TypeTable) {
		td, err := wrapTypeDesc(pd.typeDescHandle)
		if err != nil {
			return backend.ParameterDescriptor{}, err
		}
		p.TypeDesc = td
	}
	return p, nil
}

// convertDirection maps RFC_DIRECTION enum to the Go
// [backend.Direction] bitmask.
func convertDirection(d C.RFC_DIRECTION) backend.Direction {
	switch d {
	case C.RFC_IMPORT:
		return backend.DirImport
	case C.RFC_EXPORT:
		return backend.DirExport
	case C.RFC_CHANGING:
		return backend.DirChanging
	case C.RFC_TABLES:
		return backend.DirTables
	default:
		return 0
	}
}

// wrapTypeDesc converts a RFC_TYPE_DESC_HANDLE (cached by the
// SDK; do NOT free) to the typed backend descriptor.
func wrapTypeDesc(td C.RFC_TYPE_DESC_HANDLE) (*backend.TypeDescriptor, error) {
	var info C.RFC_ERROR_INFO
	var nameBuf [40]C.SAP_UC
	if rc := C.RfcGetTypeName(td, &nameBuf[0], &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcGetTypeName")
	}
	var fieldCount C.uint
	if rc := C.RfcGetFieldCount(td, &fieldCount, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcGetFieldCount")
	}
	out := &backend.TypeDescriptor{
		Name:   sapUCSliceToString(nameBuf[:]),
		Fields: make([]backend.FieldDescriptor, 0, fieldCount),
	}
	for i := C.uint(0); i < fieldCount; i++ {
		var fd C.RFC_FIELD_DESC
		if rc := C.RfcGetFieldDescByIndex(td, i, &fd, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetFieldDescByIndex")
		}
		f := backend.FieldDescriptor{
			Name:     sapUCSliceToString(fd.name[:]),
			Type:     backend.RFCType(fd._type),
			Length:   uint(fd.ucLength),
			Decimals: uint(fd.decimals),
			Offset:   uint(fd.ucOffset),
		}
		if fd.typeDescHandle != nil && (f.Type == backend.TypeStructure || f.Type == backend.TypeTable) {
			nested, err := wrapTypeDesc(fd.typeDescHandle)
			if err != nil {
				return nil, err
			}
			f.TypeDesc = nested
		}
		out.Fields = append(out.Fields, f)
	}
	return out, nil
}

// metadataInvalidationMu protects RfcRemoveFunctionDesc from
// concurrent calls — the SDK's cache is process-global.
var metadataInvalidationMu sync.Mutex

// invalidateMetadata implements [backend.Backend.InvalidateMetadata]
// over RfcRemoveFunctionDesc.
//
// SDK function: RfcRemoveFunctionDesc (🟡 verify; available
// in 7.50 PL3 according to node-rfc bindings, not yet in the
// programming-guide page link in docs/SDK_FUNCTIONS_MAP.md).
func invalidateMetadata(fn string) error {
	metadataInvalidationMu.Lock()
	defer metadataInvalidationMu.Unlock()

	// 🟡 RfcRemoveFunctionDesc accepts a SYSID + function name
	// in some SDK versions; in others, only a function name.
	// We pass an empty SYSID UC for the broad case until the
	// programming guide is verified.
	sysidUC, err := stringToSAPUC("")
	if err != nil {
		return err
	}
	defer C.goFreeU(sysidUC)
	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return err
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	rc := C.RfcRemoveFunctionDesc(sysidUC, nameUC, &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcRemoveFunctionDesc("+fn+")")
	}
	return nil
}
