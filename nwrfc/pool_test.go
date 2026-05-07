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
