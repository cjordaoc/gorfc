// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// blockingBackend simulates an SDK backend whose operations
// block on channels until the test releases them. Drives the
// Cancel / ctx-cancel test surface without needing a real SDK.
//
// The "cancel" channel is closed by [Cancel]; every blocking
// operation selects on (`releaseCh`, `cancelCh`, `ctx.Done()`).
// When `cancelCh` fires, the op returns ErrCancelled wrapped
// per backend convention so nwrfc.mapBackendError translates
// to *CancelledError.
type blockingBackend struct {
	mu              sync.Mutex
	openInFlight    chan struct{} // cleared after Open returns
	pingInFlight    chan struct{}
	resetInFlight   chan struct{}
	descrInFlight   chan struct{}
	invokeInFlight  chan struct{}
	cancelCh        chan struct{}
	cancelCallCount atomic.Int32
}

func newBlockingBackend() *blockingBackend {
	return &blockingBackend{
		cancelCh: make(chan struct{}),
	}
}

// wrapCtxErr translates a fired context error into the
// backend-level sentinel the real cgo backend returns. The
// nwrfc.mapBackendError translates these into the typed
// public errors (TimeoutError / CancelledError).
func wrapCtxErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return backend.ErrTimeout
	}
	return backend.ErrCancelled
}

func (b *blockingBackend) closeChan(ch chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func (*blockingBackend) Name() string { return "blocking" }
func (*blockingBackend) Version() backend.Version {
	return backend.Version{Major: 7, Minor: 50, PatchLevel: 18}
}
func (*blockingBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{WebSocketRFC: true}
}

func (b *blockingBackend) Open(ctx context.Context, _ backend.Params) (backend.ConnHandle, error) {
	if b.openInFlight == nil {
		// Synchronous Open path used by happy-path tests.
		return 1, nil
	}
	select {
	case <-b.openInFlight:
		return 1, nil
	case <-b.cancelCh:
		return 0, backend.ErrCancelled
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (b *blockingBackend) Close(backend.ConnHandle) error { return nil }

func (b *blockingBackend) Ping(ctx context.Context, _ backend.ConnHandle) error {
	if b.pingInFlight == nil {
		return nil
	}
	select {
	case <-b.pingInFlight:
		return nil
	case <-b.cancelCh:
		return backend.ErrCancelled
	case <-ctx.Done():
		return wrapCtxErr(ctx.Err())
	}
}

func (b *blockingBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, nil
}

func (b *blockingBackend) Reset(ctx context.Context, _ backend.ConnHandle) error {
	if b.resetInFlight == nil {
		return nil
	}
	select {
	case <-b.resetInFlight:
		return nil
	case <-b.cancelCh:
		return backend.ErrCancelled
	case <-ctx.Done():
		return wrapCtxErr(ctx.Err())
	}
}

func (b *blockingBackend) Describe(ctx context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	if b.descrInFlight == nil {
		return backend.FunctionDescriptor{}, nil
	}
	select {
	case <-b.descrInFlight:
		return backend.FunctionDescriptor{}, nil
	case <-b.cancelCh:
		return backend.FunctionDescriptor{}, backend.ErrCancelled
	case <-ctx.Done():
		return backend.FunctionDescriptor{}, wrapCtxErr(ctx.Err())
	}
}

func (b *blockingBackend) Invoke(ctx context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	if b.invokeInFlight == nil {
		return backend.CallParams{}, nil
	}
	select {
	case <-b.invokeInFlight:
		return backend.CallParams{}, nil
	case <-b.cancelCh:
		return nil, backend.ErrCancelled
	case <-ctx.Done():
		return nil, wrapCtxErr(ctx.Err())
	}
}

func (*blockingBackend) InvalidateMetadata(string) error { return nil }

// Cancel implements [backend.Cancellable]. Idempotent: closing
// an already-closed channel would panic; the helper guards
// against that.
func (b *blockingBackend) Cancel(_ backend.ConnHandle) error {
	b.cancelCallCount.Add(1)
	b.closeChan(b.cancelCh)
	return nil
}

// TestCancel_OnConnection_StopsInFlightInvoke: a blocked Invoke
// terminates with *CancelledError when Conn.Cancel is called
// from another goroutine. errors.Is(err, ErrCancelled) returns
// true; the Cancel itself is idempotent.
func TestCancel_StopsInFlightInvoke(t *testing.T) {
	b := newBlockingBackend()
	b.invokeInFlight = make(chan struct{})
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	errCh := make(chan error, 1)
	go func() {
		_, err := nwrfc.Call(context.Background(), c, "STFC_PING", nil, nil)
		errCh <- err
	}()

	// Brief settle so Invoke is actually blocking before we cancel.
	time.Sleep(20 * time.Millisecond)
	if err := c.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, nwrfc.ErrCancelled) {
			t.Errorf("err=%v; want errors.Is ErrCancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Invoke did not return after Cancel")
	}
}

// TestCancel_Idempotent: 3× Cancel on the same Conn returns
// nil each time and does not panic. The backend's cancelCh is
// closed only once.
func TestCancel_Idempotent(t *testing.T) {
	b := newBlockingBackend()
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	for i := 0; i < 3; i++ {
		if err := c.Cancel(); err != nil {
			t.Fatalf("Cancel call %d returned %v", i+1, err)
		}
	}
}

// TestCancel_AfterClose_NoOp: Cancel after Close returns nil
// without touching the backend. The Conn is in the closed
// state; Cancel cannot do useful work and must not panic.
func TestCancel_AfterCloseIsNoOp(t *testing.T) {
	b := newBlockingBackend()
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Cancel(); err != nil {
		t.Errorf("Cancel after Close: %v", err)
	}
	if got := b.cancelCallCount.Load(); got != 0 {
		t.Errorf("Cancel after Close hit the backend %d times; want 0", got)
	}
}

// TestCancel_CloseAfterCancel: the documented sequence — Cancel
// to interrupt, then Close to release. Both succeed.
func TestCancel_CloseAfterCancel(t *testing.T) {
	b := newBlockingBackend()
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close after Cancel: %v", err)
	}
	if c.Alive() {
		t.Error("Conn alive after Close")
	}
}

// TestCancel_CtxCancel_AlsoCancels: even without calling
// Conn.Cancel, a ctx cancellation surfaces *CancelledError on
// the in-flight op. The Conn remains usable (the SDK contract).
func TestCancel_CtxCancelTriggersCancellation(t *testing.T) {
	b := newBlockingBackend()
	b.invokeInFlight = make(chan struct{})
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = nwrfc.Call(ctx, c, "STFC_PING", nil, nil)
	if err == nil {
		t.Fatal("Call did not return after ctx deadline")
	}
	if !errors.Is(err, nwrfc.ErrCancelled) && !errors.Is(err, nwrfc.ErrTimeout) {
		t.Errorf("err=%v; want ErrCancelled or ErrTimeout", err)
	}
}

// TestCancel_NosdkBackendUnsupported: against a backend that
// does not implement [backend.Cancellable], Conn.Cancel returns
// *UnsupportedFeatureError. Demonstrates the AGENTS.md "no
// silent fallback" contract — we do NOT pretend to cancel
// when we cannot.
type noCancelBackend struct{ blockingBackend }

func (b *noCancelBackend) Cancel() {} // wrong signature → not Cancellable

// Override embedded interface helpers so type assertions don't
// pick up the embedded Cancel.
func TestCancel_BackendWithoutCancelCapability(t *testing.T) {
	// We cannot easily build a backend that does NOT implement
	// Cancellable from blockingBackend (Cancel is on the
	// pointer). Instead, use a brand-new backend that mirrors
	// the surface but lacks Cancel. Defined inline.
	prev := backend.SetTesting(&noCapBackend{})
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	err = c.Cancel()
	if err == nil {
		t.Fatal("Cancel returned nil on backend without Cancellable")
	}
	if !errors.Is(err, nwrfc.ErrUnsupported) {
		t.Errorf("err=%v; want ErrUnsupported", err)
	}
}

// noCapBackend is the minimum [backend.Backend] implementation
// that lacks [backend.Cancellable]. Used by
// TestCancel_BackendWithoutCancelCapability.
type noCapBackend struct{}

func (*noCapBackend) Name() string                       { return "nocap" }
func (*noCapBackend) Version() backend.Version           { return backend.Version{} }
func (*noCapBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (*noCapBackend) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 1, nil
}
func (*noCapBackend) Close(backend.ConnHandle) error                     { return nil }
func (*noCapBackend) Ping(_ context.Context, _ backend.ConnHandle) error { return nil }
func (*noCapBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, nil
}
func (*noCapBackend) Reset(_ context.Context, _ backend.ConnHandle) error { return nil }
func (*noCapBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (*noCapBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (*noCapBackend) InvalidateMetadata(string) error { return nil }
