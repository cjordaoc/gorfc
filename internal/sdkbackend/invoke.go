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

	"github.com/cjordaoc/gorfc/internal/backend"
)

// invokeFunction implements [backend.Backend.Invoke] over
// RfcCreateFunction + (optional) fill + RfcInvoke + wrap +
// RfcDestroyFunction. Honors ctx via a cancel watcher.
//
// SDK functions: RfcCreateFunction, RfcInvoke,
// RfcDestroyFunction (✅ confirmed).
func invokeFunction(ctx context.Context, c *connHandle, fn string, in backend.CallParams, opts backend.InvokeOptions) (backend.CallParams, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fetch (cached) descriptor; required to allocate function
	// container.
	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return nil, err
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	desc := C.RfcGetFunctionDesc(sdkConnPtr(c), nameUC, &info)
	if desc == nil {
		return nil, errFromInfo(&info, "RfcGetFunctionDesc("+fn+")")
	}

	fh := C.RfcCreateFunction(desc, &info)
	if fh == nil {
		return nil, errFromInfo(&info, "RfcCreateFunction("+fn+")")
	}
	defer C.RfcDestroyFunction(fh, &info)

	// Fill IMPORT + CHANGING parameters.
	if err := fillFunctionParameters(fh, desc, in); err != nil {
		return nil, err
	}

	// Honor notRequested: mark listed params inactive.
	for _, name := range opts.NotRequested {
		uc, err := stringToSAPUC(name)
		if err != nil {
			return nil, err
		}
		rc := C.goSetParamActive(fh, uc, 0, &info)
		C.goFreeU(uc)
		if rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcSetParameterActive("+name+")")
		}
	}

	// Watch ctx cancel via the shared helper (cancel.go). The
	// watcher calls RfcCancel from a separate goroutine —
	// thread-safe with respect to RfcInvoke per the SDK
	// programming guide; it does NOT take the per-Conn mutex
	// (which is held by the caller). See
	// docs/EVIDENCE/sdk-cancel.md for the SDK contract.
	cleanup := withCancelWatcher(ctx, c)
	rc := C.RfcInvoke(sdkConnPtr(c), fh, &info)
	cleanup()

	if rc != C.RFC_OK {
		// If ctx was cancelled, surface a sentinel that nwrfc
		// translates to *TimeoutError / *CancelledError.
		if err := ctxErrorIfFired(ctx, "RfcInvoke("+fn+")"); err != nil {
			return nil, err
		}
		return nil, errFromInfo(&info, "RfcInvoke("+fn+")")
	}

	// Wrap EXPORT/CHANGING/TABLES/RETURN parameters.
	out, err := wrapFunctionParameters(fh, desc, opts)
	if err != nil {
		return nil, err
	}

	// ReturnImportParams: echo the IMPORTs into the result.
	if opts.ReturnImportParams {
		for k, v := range in {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out, nil
}
