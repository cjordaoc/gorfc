// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// Cgo-side cancel watcher plumbing for the SAP NWRFC SDK
// backend. Centralizes the goroutine pattern that
// `internal/sdkbackend/{invoke,transaction,unit}.go` were
// duplicating: spin a watcher that calls `RfcCancel` when
// `ctx.Done()` fires, then close the watcher when the SDK call
// returns.
//
// Calling RfcCancel from a watcher goroutine is the documented
// thread-safe interrupt path for the SDK (see
// docs/EVIDENCE/sdk-cancel.md). The watcher does NOT take any
// per-Conn mutex — by design the blocked op holds it.
//
// SDK function: RfcCancel (✅ verified PL18; min PL 7.50 PL3).

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// withCancelWatcher arranges a goroutine that calls RfcCancel
// on c if ctx fires before the call body returns. The body's
// `rc` (return code) and `info` (error info) are inspected
// after the watcher is taken down: if the SDK reported
// non-success AND ctx is done, we synthesize the right
// timeout / cancelled sentinel; otherwise the SDK error is
// returned verbatim to the caller's `errFromInfo` path.
//
// Usage (typical):
//
//	cleanup := withCancelWatcher(ctx, c)
//	rc := C.RfcInvoke(sdkConnPtr(c), fh, &info)
//	cleanup()
//	if rc != C.RFC_OK {
//	    if err := ctxErrorIfFired(ctx, op); err != nil {
//	        return err
//	    }
//	    return errFromInfo(&info, op)
//	}
//
// The two-step pattern (cleanup() + ctxErrorIfFired()) is
// preferred over a single helper that swallows the SDK call
// because the existing call sites need the full SDK return
// code path for their out-params (function-handle creation,
// fill, wrap), and forcing them through a closure complicates
// the cgo memory dance.
func withCancelWatcher(ctx context.Context, c *connHandle) (cleanup func()) {
	if ctx == nil || ctx.Done() == nil {
		// No deadline / no cancellation: nothing to watch.
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// RfcCancel is the SDK-documented thread-safe
			// interrupt path; safe to invoke without holding
			// the per-Conn mutex.
			var ci C.RFC_ERROR_INFO
			C.RfcCancel(sdkConnPtr(c), &ci)
		case <-done:
		}
	}()
	return func() { close(done) }
}

// ctxErrorIfFired translates a fired ctx into the right
// backend sentinel error. Returns nil when ctx is still alive,
// so the caller can fall through to the SDK's own error path
// (`errFromInfo`).
//
// op is the human-readable operation name, e.g. "RfcInvoke(FOO)";
// it travels in the wrapped error so nwrfc.mapBackendError can
// extract the function name for the public TimeoutError /
// CancelledError types.
func ctxErrorIfFired(ctx context.Context, op string) error {
	cerr := ctx.Err()
	if cerr == nil {
		return nil
	}
	if errors.Is(cerr, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", op, backend.ErrTimeout)
	}
	return fmt.Errorf("%s: %w", op, backend.ErrCancelled)
}
