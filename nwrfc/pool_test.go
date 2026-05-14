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

func TestPool_BasicAcquireRelease(t *testing.T) {
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MinSize: 0,
		MaxSize: 4,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	ctx := context.Background()
	c1, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !c1.Alive() {
		t.Error("acquired Conn not alive")
	}
	stats := p.Stats()
	if stats.Open != 1 || stats.Idle != 0 {
		t.Errorf("stats=%+v", stats)
	}
	p.Release(c1, false)
	stats = p.Stats()
	if stats.Idle != 1 {
		t.Errorf("after release stats=%+v", stats)
	}

	// Re-acquire returns the same connection (LIFO).
	c2, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	if c2 != c1 {
		t.Error("LIFO not preserved")
	}
	p.Release(c2, false)
}

func TestPool_MaxSizeBlocksUntilRelease(t *testing.T) {
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	p, _ := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 2,
	})
	t.Cleanup(func() { _ = p.Close() })

	ctx := context.Background()
	c1, _ := p.Acquire(ctx)
	c2, _ := p.Acquire(ctx)

	// Third acquire blocks; release c1 in 50ms.
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Release(c1, false)
		close(released)
	}()
	c3, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire while waiting: %v", err)
	}
	<-released
	if c3 != c1 {
		t.Error("did not get the released Conn back")
	}
	p.Release(c2, false)
	p.Release(c3, false)
}

func TestPool_AcquireCtxCancel(t *testing.T) {
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	p, _ := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 1,
	})
	t.Cleanup(func() { _ = p.Close() })

	c1, _ := p.Acquire(context.Background())
	defer p.Release(c1, false)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Acquire(ctx); err == nil {
		t.Error("Acquire returned nil error after deadline")
	}
}

func TestPool_AcquireCancelledBeforeOpenDoesNotDial(t *testing.T) {
	b := &countingBackend{}
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 1,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = p.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire err=%v want context.Canceled", err)
	}
	if got := b.opens.Load(); got != 0 {
		t.Fatalf("backend Open calls=%d want 0", got)
	}
}

func TestPool_ReleasePreservesCreatedAtForMaxLifetime(t *testing.T) {
	prev := backend.SetTesting(&countingBackend{})
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     1,
		MaxLifetime: 70 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c1, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire c1: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	p.Release(c1, false)

	c2, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire c2: %v", err)
	}
	if c2 != c1 {
		t.Fatal("connection expired before MaxLifetime elapsed")
	}

	time.Sleep(40 * time.Millisecond)
	p.Release(c2, false)

	c3, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire c3: %v", err)
	}
	if c3 == c2 {
		t.Fatal("connection survived past MaxLifetime; createdAt was not preserved")
	}
	p.Release(c3, false)
}

func TestPool_IdleCleanupTickerRemovesExpiredConnections(t *testing.T) {
	prev := backend.SetTesting(&countingBackend{})
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     1,
		IdleTimeout: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	p.Release(c, false)
	if stats := p.Stats(); stats.Open != 1 || stats.Idle != 1 {
		t.Fatalf("stats after release=%+v want open=1 idle=1", stats)
	}

	waitFor(t, 300*time.Millisecond, func() bool {
		stats := p.Stats()
		return stats.Open == 0 && stats.Idle == 0
	})
}

func TestPool_IdleCleanupKeepsExpiredConnectionCountedUntilClosed(t *testing.T) {
	b := newBlockingCloseBackend()
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     1,
		IdleTimeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire initial: %v", err)
	}
	p.Release(c, false)

	select {
	case <-b.closeStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("idle cleanup did not start closing expired connection")
	}

	acquired := make(chan *nwrfc.Conn, 1)
	acquireErr := make(chan error, 1)
	go func() {
		c, err := p.Acquire(context.Background())
		if err != nil {
			acquireErr <- err
			return
		}
		acquired <- c
	}()

	select {
	case c := <-acquired:
		p.Release(c, false)
		t.Fatal("Acquire returned while the expired connection was still physically open")
	case err := <-acquireErr:
		t.Fatalf("Acquire returned error while waiting for cleanup close: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if got := b.maxPhysical.Load(); got > 1 {
		t.Fatalf("physically open connections=%d exceeded MaxSize=1", got)
	}

	close(b.unblockClose)

	select {
	case c := <-acquired:
		p.Release(c, false)
	case err := <-acquireErr:
		t.Fatalf("Acquire after cleanup close: %v", err)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Acquire did not proceed after cleanup close freed capacity")
	}
	if got := b.maxPhysical.Load(); got > 1 {
		t.Fatalf("physically open connections=%d exceeded MaxSize=1", got)
	}
}

func TestPool_IdleCleanupAccountsEachExpiredCloseBeforeNextClose(t *testing.T) {
	b := newStagedCloseBackend(2)
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     2,
		IdleTimeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c1, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire c1: %v", err)
	}
	c2, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire c2: %v", err)
	}
	p.Release(c1, false)
	p.Release(c2, false)
	waitFor(t, 300*time.Millisecond, func() bool {
		stats := p.Stats()
		return stats.Open == 2 && stats.Idle == 2 && b.physical.Load() == 2
	})

	select {
	case <-b.closeStarted[0]:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("idle cleanup did not start closing first expired connection")
	}

	acquired := make(chan *nwrfc.Conn, 1)
	acquireErr := make(chan error, 1)
	go func() {
		c, err := p.Acquire(context.Background())
		if err != nil {
			acquireErr <- err
			return
		}
		acquired <- c
	}()

	select {
	case c := <-acquired:
		p.Release(c, false)
		t.Fatal("Acquire returned while the first expired connection was still physically open")
	case err := <-acquireErr:
		t.Fatalf("Acquire returned error while waiting for cleanup close: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	assertPoolOpenMatchesPhysical(t, p, b.physical.Load())

	close(b.unblockClose[0])

	var c3 *nwrfc.Conn
	select {
	case c3 = <-acquired:
	case err := <-acquireErr:
		t.Fatalf("Acquire after first cleanup close: %v", err)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Acquire did not proceed after the first cleanup close freed one slot")
	}
	defer p.Release(c3, false)
	assertPoolOpenMatchesPhysical(t, p, b.physical.Load())

	select {
	case <-b.closeStarted[1]:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("idle cleanup did not start closing second expired connection")
	}
	assertPoolOpenMatchesPhysical(t, p, b.physical.Load())

	close(b.unblockClose[1])
	waitFor(t, 300*time.Millisecond, func() bool {
		return p.Stats().Open == 1 && b.physical.Load() == 1
	})
	if got := p.Stats().Open; got != 1 {
		t.Fatalf("Open after second expired close=%d want 1 checked-out connection", got)
	}
}

func TestPool_DoReleasesOnError(t *testing.T) {
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	p, _ := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 2,
	})
	t.Cleanup(func() { _ = p.Close() })

	wantErr := nwrfc.ErrLogon
	err := p.Do(context.Background(), func(c *nwrfc.Conn) error {
		return wantErr
	})
	if err == nil || err.Error() != "RFC logon failed" {
		t.Errorf("Do err=%v want %v", err, wantErr)
	}
	// Conn should be discarded, openCount back to 0.
	if got := p.Stats().Open; got != 0 {
		t.Errorf("Open after Do error=%d want 0", got)
	}
}

// TestPool_AlwaysReset_CallsResetBeforeAfterAcquire asserts the
// fixed ordering: Reset → AfterAcquire → return Conn. We track
// the order via a backend that increments a counter on Reset
// and an AfterAcquire that records the counter value at hook
// time; AfterAcquire's seen-counter must be > 0 (Reset already
// ran).
func TestPool_AlwaysReset_CallsResetBeforeAfterAcquire(t *testing.T) {
	rb := &resetTrackingBackend{}
	prev := backend.SetTesting(rb)
	t.Cleanup(prev)

	var afterAcquireResetCount int32
	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     2,
		AlwaysReset: true,
		AfterAcquire: func(_ context.Context, _ *nwrfc.Conn) error {
			afterAcquireResetCount = rb.resetCount.Load()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer p.Release(c, false)

	if afterAcquireResetCount == 0 {
		t.Errorf("Reset did not run before AfterAcquire (counter at AfterAcquire=%d)",
			afterAcquireResetCount)
	}
	if got := rb.resetCount.Load(); got < 1 {
		t.Errorf("Reset count=%d; want >= 1", got)
	}
}

// TestPool_AlwaysReset_DefaultFalseSkipsReset: with AlwaysReset
// off (default), the pool does not call Reset. Back-compat with
// v0.1.
func TestPool_AlwaysReset_DefaultFalseSkipsReset(t *testing.T) {
	rb := &resetTrackingBackend{}
	prev := backend.SetTesting(rb)
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	p.Release(c, false)

	if got := rb.resetCount.Load(); got != 0 {
		t.Errorf("Reset count=%d; want 0 with AlwaysReset=false", got)
	}
}

// TestPool_AlwaysReset_ResetErrorSurfacesToCaller: a Reset that
// fails terminates the Acquire with the wrapped error. The
// pool does NOT loop and does NOT silently hand a Conn whose
// state could not be cleared.
func TestPool_AlwaysReset_ResetErrorSurfacesToCaller(t *testing.T) {
	rb := &resetTrackingBackend{resetErr: errSampleReset}
	prev := backend.SetTesting(rb)
	t.Cleanup(prev)

	p, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:      nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize:     2,
		AlwaysReset: true,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Acquire(context.Background())
	if err == nil {
		t.Fatal("Acquire returned nil error when Reset failed")
	}
}

// errSampleReset is a sentinel used by the test above; the
// resetTrackingBackend returns this verbatim from Reset when
// configured with resetErr != nil.
var errSampleReset = errSentinel("reset failed for test")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// resetTrackingBackend extends happyBackend with a per-call
// counter and a configurable Reset error. Used by the
// AlwaysReset tests.
type resetTrackingBackend struct {
	happyBackend
	resetCount atomic.Int32
	resetErr   error
}

func (b *resetTrackingBackend) Reset(_ context.Context, _ backend.ConnHandle) error {
	b.resetCount.Add(1)
	return b.resetErr
}

func TestPool_ConcurrentLoad(t *testing.T) {
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	p, _ := nwrfc.NewPool(nwrfc.PoolConfig{
		Params:  nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p"},
		MaxSize: 8,
	})
	t.Cleanup(func() { _ = p.Close() })

	const goroutines = 16
	const callsPerG = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerG; j++ {
				c, err := p.Acquire(context.Background())
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				_ = c.Ping(context.Background())
				p.Release(c, false)
			}
		}()
	}
	wg.Wait()
	if got := p.Stats().Open; got > 8 {
		t.Errorf("Open=%d exceeded MaxSize=8", got)
	}
}

type countingBackend struct {
	opens atomic.Uint64
}

func (b *countingBackend) Name() string { return "counting" }
func (b *countingBackend) Version() backend.Version {
	return backend.Version{Major: 7, Minor: 50, PatchLevel: 12}
}
func (b *countingBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (b *countingBackend) Open(ctx context.Context, _ backend.Params) (backend.ConnHandle, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return backend.ConnHandle(b.opens.Add(1)), nil
}
func (b *countingBackend) Close(backend.ConnHandle) error                     { return nil }
func (b *countingBackend) Ping(_ context.Context, _ backend.ConnHandle) error { return nil }
func (b *countingBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{SysID: "TST"}, nil
}
func (b *countingBackend) Reset(backend.ConnHandle) error { return nil }
func (b *countingBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (b *countingBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (b *countingBackend) InvalidateMetadata(string) error { return nil }

type blockingCloseBackend struct {
	nextHandle   atomic.Uint64
	physical     atomic.Int64
	maxPhysical  atomic.Int64
	closeBlocked atomic.Bool
	closeStarted chan struct{}
	unblockClose chan struct{}
}

func newBlockingCloseBackend() *blockingCloseBackend {
	return &blockingCloseBackend{
		closeStarted: make(chan struct{}),
		unblockClose: make(chan struct{}),
	}
}

func (b *blockingCloseBackend) Name() string { return "blocking-close" }
func (b *blockingCloseBackend) Version() backend.Version {
	return backend.Version{Major: 7, Minor: 50, PatchLevel: 12}
}
func (b *blockingCloseBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}
func (b *blockingCloseBackend) Open(ctx context.Context, _ backend.Params) (backend.ConnHandle, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cur := b.physical.Add(1)
	for {
		max := b.maxPhysical.Load()
		if cur <= max || b.maxPhysical.CompareAndSwap(max, cur) {
			break
		}
	}
	return backend.ConnHandle(b.nextHandle.Add(1)), nil
}
func (b *blockingCloseBackend) Close(backend.ConnHandle) error {
	if b.closeBlocked.CompareAndSwap(false, true) {
		close(b.closeStarted)
		<-b.unblockClose
	}
	b.physical.Add(-1)
	return nil
}
func (b *blockingCloseBackend) Ping(_ context.Context, _ backend.ConnHandle) error {
	return nil
}
func (b *blockingCloseBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{SysID: "TST"}, nil
}
func (b *blockingCloseBackend) Reset(backend.ConnHandle) error { return nil }
func (b *blockingCloseBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (b *blockingCloseBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (b *blockingCloseBackend) InvalidateMetadata(string) error { return nil }

type stagedCloseBackend struct {
	nextHandle  atomic.Uint64
	physical    atomic.Int64
	maxPhysical atomic.Int64
	closeSeq    atomic.Uint64

	closeStarted []chan struct{}
	unblockClose []chan struct{}
}

func newStagedCloseBackend(blockingCloses int) *stagedCloseBackend {
	b := &stagedCloseBackend{
		closeStarted: make([]chan struct{}, blockingCloses),
		unblockClose: make([]chan struct{}, blockingCloses),
	}
	for i := 0; i < blockingCloses; i++ {
		b.closeStarted[i] = make(chan struct{})
		b.unblockClose[i] = make(chan struct{})
	}
	return b
}

func (b *stagedCloseBackend) Name() string { return "staged-close" }
func (b *stagedCloseBackend) Version() backend.Version {
	return backend.Version{Major: 7, Minor: 50, PatchLevel: 12}
}
func (b *stagedCloseBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}
func (b *stagedCloseBackend) Open(ctx context.Context, _ backend.Params) (backend.ConnHandle, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cur := b.physical.Add(1)
	for {
		max := b.maxPhysical.Load()
		if cur <= max || b.maxPhysical.CompareAndSwap(max, cur) {
			break
		}
	}
	return backend.ConnHandle(b.nextHandle.Add(1)), nil
}
func (b *stagedCloseBackend) Close(backend.ConnHandle) error {
	seq := int(b.closeSeq.Add(1) - 1)
	if seq < len(b.closeStarted) {
		close(b.closeStarted[seq])
		<-b.unblockClose[seq]
	}
	b.physical.Add(-1)
	return nil
}
func (b *stagedCloseBackend) Ping(_ context.Context, _ backend.ConnHandle) error {
	return nil
}
func (b *stagedCloseBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{SysID: "TST"}, nil
}
func (b *stagedCloseBackend) Reset(backend.ConnHandle) error { return nil }
func (b *stagedCloseBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (b *stagedCloseBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (b *stagedCloseBackend) InvalidateMetadata(string) error { return nil }

func assertPoolOpenMatchesPhysical(t *testing.T, p *nwrfc.Pool, physical int64) {
	t.Helper()
	if got := p.Stats().Open; got != int(physical) {
		t.Fatalf("Pool.Stats().Open=%d want physical-open count %d", got, physical)
	}
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
