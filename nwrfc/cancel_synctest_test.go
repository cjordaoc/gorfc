// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Deterministic cancel/timeout/concurrency tests via
// testing/synctest. Promoted to a stable API in Go 1.25;
// our toolchain directive (go.mod:toolchain go1.25.0) pins
// every build to a Go version that has it. Documented in
// docs/BUILD.md and docs/ROADMAP_NEXUS_INTEGRATION.md §P2.6.
//
// These tests run in a synctest "bubble" — a virtual clock and
// scheduler that lets us assert exact orderings without flaky
// real-time sleeps. The tradeoff: every helper called inside
// the bubble must be synctest-compatible (channels, select,
// `time` package — all fine; OS syscalls and the network — not).

package nwrfc_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestCancel_Synctest_CtxDeadlineFiresPredictably uses a
// synctest bubble to exercise the deadline path with no real
// wall-clock waiting. Without synctest the test relies on
// time.Sleep settling delays; with synctest it is exact.
//
// The blockingBackend's Invoke selects on:
//   - the inFlight channel (released by the test for happy
//     paths)
//   - cancelCh (closed by Conn.Cancel)
//   - ctx.Done()
//
// We exercise the ctx.Done() branch by setting a deadline,
// advancing the bubble's clock past it, and asserting the
// returned error is *TimeoutError.
func TestCancel_Synctest_CtxDeadlineFiresPredictably(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBlockingBackend()
		b.invokeInFlight = make(chan struct{}) // never released
		prev := backend.SetTesting(b)
		t.Cleanup(prev)

		c, err := nwrfc.Open(context.Background(), nwrfc.Params{
			AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Drive the call from a separate goroutine inside the
		// bubble so the bubble can schedule both the call and
		// the watcher deterministically.
		errCh := make(chan error, 1)
		go func() {
			_, err := nwrfc.Call(ctx, c, "STFC_PING", nil, nil)
			errCh <- err
		}()

		select {
		case err := <-errCh:
			if !errors.Is(err, nwrfc.ErrTimeout) && !errors.Is(err, nwrfc.ErrCancelled) {
				t.Errorf("err=%v; want errors.Is ErrTimeout or ErrCancelled", err)
			}
		case <-time.After(time.Second):
			// In the bubble, time.After advances the virtual
			// clock; reaching this branch means the call
			// blocked past the deadline without surfacing,
			// which is the bug this test guards against.
			t.Fatalf("Call blocked past virtual deadline")
		}
	})
}

// TestCancel_Synctest_CancelImmediatelyUnblocksCall: with
// synctest's deterministic scheduler we can assert that a
// Conn.Cancel from a sibling goroutine immediately unblocks
// the in-flight Call, with no real-time sleep slop.
func TestCancel_Synctest_CancelImmediatelyUnblocksCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBlockingBackend()
		b.invokeInFlight = make(chan struct{}) // never released
		prev := backend.SetTesting(b)
		t.Cleanup(prev)

		c, err := nwrfc.Open(context.Background(), nwrfc.Params{
			AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		ready := make(chan struct{})
		errCh := make(chan error, 1)
		go func() {
			close(ready)
			_, err := nwrfc.Call(context.Background(), c, "STFC_PING", nil, nil)
			errCh <- err
		}()

		<-ready
		// Wait for the call goroutine to actually be blocking
		// inside Invoke before we cancel; synctest.Wait
		// blocks until every other goroutine in the bubble is
		// durably blocked.
		synctest.Wait()

		if err := c.Cancel(); err != nil {
			t.Fatalf("Cancel: %v", err)
		}

		select {
		case err := <-errCh:
			if !errors.Is(err, nwrfc.ErrCancelled) {
				t.Errorf("err=%v; want errors.Is ErrCancelled", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("Cancel did not unblock Call")
		}
	})
}
