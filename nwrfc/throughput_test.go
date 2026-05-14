// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

// newThroughputConn installs a mock backend with an echo
// handler and returns an open Conn. SDK-free; works under
// -tags nwrfc_nosdk.
func newThroughputConn(t *testing.T) *nwrfc.Conn {
	t.Helper()
	mock := nwrfcmock.New()
	mock.HandleFunc("STFC_CONNECTION", func(_ context.Context, in backend.CallParams) (backend.CallParams, error) {
		return backend.CallParams{"ECHOTEXT": in["REQUTEXT"]}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestThroughputAttachAndObserve(t *testing.T) {
	c := newThroughputConn(t)

	tp := nwrfc.NewThroughput()
	if err := tp.Attach(c); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// Counters start at zero.
	if got := tp.Stats().Calls; got != 0 {
		t.Fatalf("Calls before any call = %d, want 0", got)
	}

	const n = 3
	for i := 0; i < n; i++ {
		if _, err := nwrfc.CallMap(context.Background(), c, "STFC_CONNECTION", backend.CallParams{"REQUTEXT": "ping"}); err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}

	stats := tp.Stats()
	if stats.Calls != n {
		t.Errorf("Calls = %d, want %d", stats.Calls, n)
	}
	// Go-side fallback does not populate bytes/timing.
	if stats.BytesSent != 0 || stats.BytesReceived != 0 {
		t.Errorf("bytes counters = (%d,%d), want (0,0) on Go-side fallback", stats.BytesSent, stats.BytesReceived)
	}
	if stats.WallTime <= 0 {
		t.Errorf("WallTime = %v, want > 0", stats.WallTime)
	}
}

func TestThroughputReset(t *testing.T) {
	c := newThroughputConn(t)

	tp := nwrfc.NewThroughput()
	if err := tp.Attach(c); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := nwrfc.CallMap(context.Background(), c, "STFC_CONNECTION", backend.CallParams{"REQUTEXT": "ping"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := tp.Stats().Calls; got != 1 {
		t.Fatalf("Calls = %d, want 1", got)
	}

	before := tp.Stats().WallTime
	time.Sleep(2 * time.Millisecond)
	tp.Reset()

	after := tp.Stats()
	if after.Calls != 0 {
		t.Errorf("Calls after Reset = %d, want 0", after.Calls)
	}
	// Reset re-anchors WallTime: the fresh window must be
	// shorter than the pre-Reset elapsed time.
	if after.WallTime >= before+2*time.Millisecond {
		t.Errorf("WallTime not re-anchored: before=%v after=%v", before, after.WallTime)
	}
}

func TestThroughputConcurrent(t *testing.T) {
	c := newThroughputConn(t)

	tp := nwrfc.NewThroughput()
	if err := tp.Attach(c); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	const goroutines = 8
	const iterations = 100
	var wg sync.WaitGroup

	// Readers: hammer Stats().
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = tp.Stats()
			}
		}()
	}
	// Resetters: hammer Reset() (writes createdAtNano).
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tp.Reset()
			}
		}()
	}
	// Callers: drive observe() via the Conn call path.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, err := nwrfc.CallMap(context.Background(), c, "STFC_CONNECTION", backend.CallParams{"REQUTEXT": "ping"}); err != nil {
					t.Errorf("Call: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// No assertion on the final count — Reset races with the
	// callers by design. The point is the -race detector
	// finding no data race on createdAtNano or the counters.
	_ = tp.Stats()
}

func TestThroughputAttachNilConn(t *testing.T) {
	tp := nwrfc.NewThroughput()
	if err := tp.Attach(nil); err == nil {
		t.Fatal("Attach(nil) = nil, want error")
	}
}
