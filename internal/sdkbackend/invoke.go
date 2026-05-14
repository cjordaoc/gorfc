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
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

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

// invokeTableStream is the lazy TABLES counterpart to
// invokeFunction. It keeps the RFC_FUNCTION_HANDLE alive until
// the returned stream is closed. The caller owns both the
// public Conn lock and this backend connHandle mutex until
// Close, so the SAP connection cannot be reused while rows are
// still being read.
func invokeTableStream(ctx context.Context, c *connHandle, fn string, table string, in backend.CallParams, opts backend.InvokeOptions) (backend.TableStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()

	fail := func(err error) (backend.TableStream, error) {
		c.mu.Unlock()
		return nil, err
	}

	nameUC, err := stringToSAPUC(fn)
	if err != nil {
		return fail(err)
	}
	defer C.goFreeU(nameUC)

	var info C.RFC_ERROR_INFO
	desc := C.RfcGetFunctionDesc(sdkConnPtr(c), nameUC, &info)
	if desc == nil {
		return fail(errFromInfo(&info, "RfcGetFunctionDesc("+fn+")"))
	}

	fh := C.RfcCreateFunction(desc, &info)
	if fh == nil {
		return fail(errFromInfo(&info, "RfcCreateFunction("+fn+")"))
	}

	destroyAndFail := func(err error) (backend.TableStream, error) {
		C.RfcDestroyFunction(fh, &info)
		c.mu.Unlock()
		return nil, err
	}

	if err := fillFunctionParameters(fh, desc, in); err != nil {
		return destroyAndFail(err)
	}

	for _, name := range opts.NotRequested {
		uc, err := stringToSAPUC(name)
		if err != nil {
			return destroyAndFail(err)
		}
		rc := C.goSetParamActive(fh, uc, 0, &info)
		C.goFreeU(uc)
		if rc != C.RFC_OK {
			return destroyAndFail(errFromInfo(&info, "RfcSetParameterActive("+name+")"))
		}
	}

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			var ci C.RFC_ERROR_INFO
			C.RfcCancel(sdkConnPtr(c), &ci)
		case <-cancelDone:
		}
	}()

	rc := C.RfcInvoke(sdkConnPtr(c), fh, &info)
	close(cancelDone)
	if rc != C.RFC_OK {
		if cerr := ctx.Err(); cerr != nil {
			if errors.Is(cerr, context.DeadlineExceeded) {
				return destroyAndFail(fmt.Errorf("RfcInvoke(%s): %w", fn, backend.ErrTimeout))
			}
			return destroyAndFail(fmt.Errorf("RfcInvoke(%s): %w", fn, backend.ErrCancelled))
		}
		return destroyAndFail(errFromInfo(&info, "RfcInvoke("+fn+")"))
	}

	pd, err := findTableParameter(desc, table)
	if err != nil {
		return destroyAndFail(err)
	}
	var tbl C.RFC_TABLE_HANDLE
	if rc := C.RfcGetTable(fh, &pd.name[0], &tbl, &info); rc != C.RFC_OK {
		return destroyAndFail(errFromInfo(&info, "RfcGetTable("+table+")"))
	}
	var rowCount C.uint
	if rc := C.RfcGetRowCount(tbl, &rowCount, &info); rc != C.RFC_OK {
		return destroyAndFail(errFromInfo(&info, "RfcGetRowCount("+table+")"))
	}
	return &sdkTableStream{
		conn:     c,
		fh:       fh,
		table:    tbl,
		typeDesc: pd.typeDescHandle,
		opts:     opts,
		parent:   sapUCSliceToString(pd.name[:]),
		rowCount: rowCount,
	}, nil
}

func findTableParameter(desc C.RFC_FUNCTION_DESC_HANDLE, table string) (C.RFC_PARAMETER_DESC, error) {
	var paramCount C.uint
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetParameterCount(desc, &paramCount, &info); rc != C.RFC_OK {
		return C.RFC_PARAMETER_DESC{}, errFromInfo(&info, "RfcGetParameterCount")
	}
	for i := C.uint(0); i < paramCount; i++ {
		var pd C.RFC_PARAMETER_DESC
		if rc := C.RfcGetParameterDescByIndex(desc, i, &pd, &info); rc != C.RFC_OK {
			return C.RFC_PARAMETER_DESC{}, errFromInfo(&info, "RfcGetParameterDescByIndex")
		}
		name := sapUCSliceToString(pd.name[:])
		if strings.EqualFold(name, table) {
			if pd._type != C.RFCTYPE_TABLE {
				return C.RFC_PARAMETER_DESC{}, errMarshal(table, "TABLES", backend.RFCType(pd._type).String(), nil)
			}
			return pd, nil
		}
	}
	return C.RFC_PARAMETER_DESC{}, errMarshal(table, "TABLES", "missing parameter", nil)
}

type sdkTableStream struct {
	mu       sync.Mutex
	conn     *connHandle
	fh       C.RFC_FUNCTION_HANDLE
	table    C.RFC_TABLE_HANDLE
	typeDesc C.RFC_TYPE_DESC_HANDLE
	opts     backend.InvokeOptions
	parent   string

	rowCount C.uint
	index    C.uint
	started  bool
	closed   bool
}

func (s *sdkTableStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, io.ErrClosedPipe
	}
	if s.index >= s.rowCount {
		return nil, io.EOF
	}

	var info C.RFC_ERROR_INFO
	if !s.started {
		if rc := C.RfcMoveToFirstRow(s.table, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcMoveToFirstRow")
		}
		s.started = true
	} else if rc := C.RfcMoveToNextRow(s.table, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcMoveToNextRow")
	}

	row := C.RfcGetCurrentRow(s.table, &info)
	if row == nil {
		return nil, errFromInfo(&info, "RfcGetCurrentRow")
	}
	m, err := wrapStructure(s.typeDesc, row, s.opts, s.parent)
	if err != nil {
		return nil, err
	}
	s.index++
	return m, nil
}

func (s *sdkTableStream) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var info C.RFC_ERROR_INFO
	rc := C.RfcDestroyFunction(s.fh, &info)
	s.conn.mu.Unlock()
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcDestroyFunction")
	}
	return nil
}
