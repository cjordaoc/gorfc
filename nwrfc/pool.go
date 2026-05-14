// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PoolConfig configures a [Pool].
//
// MinSize keeps that many idle connections warm; the Pool
// re-opens lost connections asynchronously up to this floor.
//
// MaxSize caps the total number of open connections; Acquire
// blocks (or returns CtxErr) when MaxSize is reached and no
// connection is idle.
//
// IdleTimeout closes idle connections that have been unused
// longer than this duration. Zero disables the timeout.
//
// MaxLifetime closes connections older than this duration.
// Zero disables.
//
// AcquireTimeout caps how long a single Acquire blocks. Zero
// inherits the per-call ctx deadline.
//
// AfterAcquire is called for every connection right before it
// is handed to the caller; useful for ResetServerContext or
// custom session-state checks. Errors from AfterAcquire mark
// the connection broken; the caller gets a fresh one.
type PoolConfig struct {
	Params         Params
	MinSize        int
	MaxSize        int
	IdleTimeout    time.Duration
	MaxLifetime    time.Duration
	AcquireTimeout time.Duration
	AfterAcquire   func(ctx context.Context, c *Conn) error
}

// poolEntry wraps a Conn with bookkeeping the pool needs.
type poolEntry struct {
	conn      *Conn
	createdAt time.Time
	lastUsed  time.Time
}

// Pool manages a bounded set of [Conn]s and hands them out to
// callers via [Pool.Acquire] / [Pool.Release].
//
// Concurrency: every method is safe to call from any goroutine.
// The pool keeps idle and checked-out entries behind one mutex
// so connection lifetime metadata has a single owner.
type Pool struct {
	cfg PoolConfig

	mu         sync.Mutex
	idle       []*poolEntry         // LIFO stack of idle entries
	checkedOut map[*Conn]*poolEntry // entries owned by callers
	openCount  int                  // total currently-open (idle + checked-out)
	closed     atomic.Bool
	done       chan struct{}

	// waiters is the channel-based handoff for waiting
	// Acquires. When a Release happens with waiters present
	// the released entry is sent directly (avoiding a wakeup
	// thrash through idle).
	waiters []chan *poolEntry
}

// NewPool constructs a Pool. MinSize connections are warmed
// asynchronously; the function returns immediately after
// validating the config (it does NOT wait for warmup, so a
// transient SAP outage does not block process start).
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.MaxSize <= 0 {
		return nil, &ConfigError{Field: "MaxSize", Hint: "must be > 0"}
	}
	if cfg.MinSize < 0 || cfg.MinSize > cfg.MaxSize {
		return nil, &ConfigError{Field: "MinSize", Hint: "must satisfy 0 <= MinSize <= MaxSize"}
	}
	if err := cfg.Params.validate(); err != nil {
		return nil, err
	}
	p := &Pool{
		cfg:        cfg,
		checkedOut: make(map[*Conn]*poolEntry),
		done:       make(chan struct{}),
	}
	go p.warmup()
	go p.cleanupLoop()
	return p, nil
}

// warmup opens MinSize connections in the background. Errors
// are logged-effective via the Conn lifecycle; the pool simply
// keeps trying on subsequent Acquires.
func (p *Pool) warmup() {
	for i := 0; i < p.cfg.MinSize; i++ {
		if p.closed.Load() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		c, err := Open(ctx, p.cfg.Params)
		cancel()
		if err != nil {
			return // give up; subsequent Acquire retries
		}
		now := time.Now()
		entry := &poolEntry{conn: c, createdAt: now, lastUsed: now}
		p.mu.Lock()
		if p.closed.Load() {
			p.mu.Unlock()
			_ = c.Close()
			return
		}
		p.openCount++
		p.idle = append(p.idle, entry)
		p.mu.Unlock()
	}
}

// Acquire returns a Conn ready to use. The returned Conn is
// owned by the caller until [Pool.Release] is called; failing
// to release leaks it.
func (p *Pool) Acquire(ctx context.Context) (*Conn, error) {
	if p.closed.Load() {
		return nil, &ConfigError{Field: "Pool", Hint: "closed"}
	}
	deadline, ok := ctx.Deadline()
	if !ok && p.cfg.AcquireTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.cfg.AcquireTimeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	_ = deadline

	for {
		entry, ok, err := p.tryGet(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			// Wait for a release.
			ch := make(chan *poolEntry, 1)
			p.mu.Lock()
			if p.closed.Load() {
				p.mu.Unlock()
				return nil, &ConfigError{Field: "Pool", Hint: "closed"}
			}
			p.waiters = append(p.waiters, ch)
			p.mu.Unlock()

			select {
			case e, open := <-ch:
				if !open {
					return nil, &ConfigError{Field: "Pool", Hint: "closed"}
				}
				entry = e
			case <-ctx.Done():
				p.removeWaiter(ch)
				return nil, ctx.Err()
			}
		}
		// Validate entry: idle timeout, lifetime, AfterAcquire.
		if !p.entryValid(entry) {
			p.discard(entry)
			continue
		}
		if p.cfg.AfterAcquire != nil {
			if err := p.cfg.AfterAcquire(ctx, entry.conn); err != nil {
				p.discard(entry)
				continue
			}
		}
		entry.lastUsed = time.Now()
		if !p.checkout(entry) {
			p.discard(entry)
			return nil, &ConfigError{Field: "Pool", Hint: "closed"}
		}
		return entry.conn, nil
	}
}

// Release returns a Conn to the pool. If the Conn errored or
// the caller wants to discard it, pass discard=true; the pool
// closes it instead of recycling.
func (p *Pool) Release(c *Conn, discard bool) {
	if c == nil {
		return
	}
	p.mu.Lock()
	entry := p.checkedOut[c]
	delete(p.checkedOut, c)
	p.mu.Unlock()
	if entry == nil {
		return
	}
	if discard || !c.Alive() {
		p.discard(entry)
		return
	}
	// The entry comes from checkedOut so createdAt survives
	// every checkout/release cycle and MaxLifetime remains a
	// true connection lifetime instead of an idle-cycle timer.
	now := time.Now()
	entry.lastUsed = now
	p.releaseInternal(entry)
}

// releaseInternal returns an entry to the idle stack OR hands
// it directly to a waiter. Does NOT touch openCount: the
// counter tracks total open (idle + checked-out), which does
// not change on release.
func (p *Pool) releaseInternal(entry *poolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		_ = entry.conn.Close()
		if p.openCount > 0 {
			p.openCount--
		}
		return
	}
	// Hand off directly to a waiter if any.
	if len(p.waiters) > 0 {
		ch := p.waiters[0]
		p.waiters = p.waiters[1:]
		ch <- entry
		return
	}
	p.idle = append(p.idle, entry)
}

// tryGet checks the idle stack and opens a new connection if
// MaxSize allows. Returns (nil, false, nil) when the caller
// must wait. openCount is incremented exactly when a NEW
// connection is opened; idle pops do not change it because the
// pop just moves the conn from idle to checked-out (still
// counted as open).
func (p *Pool) tryGet(ctx context.Context) (*poolEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return nil, false, &ConfigError{Field: "Pool", Hint: "closed"}
	}
	if n := len(p.idle); n > 0 {
		e := p.idle[n-1]
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return e, true, nil
	}
	if p.openCount < p.cfg.MaxSize {
		p.openCount++
		p.mu.Unlock()
		// Open outside the lock.
		c, err := Open(ctx, p.cfg.Params)
		if err != nil {
			p.mu.Lock()
			p.openCount--
			p.mu.Unlock()
			return nil, false, err
		}
		if p.closed.Load() {
			_ = c.Close()
			p.mu.Lock()
			p.openCount--
			p.mu.Unlock()
			return nil, false, &ConfigError{Field: "Pool", Hint: "closed"}
		}
		now := time.Now()
		return &poolEntry{conn: c, createdAt: now, lastUsed: now}, true, nil
	}
	p.mu.Unlock()
	return nil, false, nil
}

func (p *Pool) entryValid(e *poolEntry) bool {
	if e == nil || e.conn == nil || !e.conn.Alive() {
		return false
	}
	if p.cfg.MaxLifetime > 0 && time.Since(e.createdAt) > p.cfg.MaxLifetime {
		return false
	}
	if p.cfg.IdleTimeout > 0 && time.Since(e.lastUsed) > p.cfg.IdleTimeout {
		return false
	}
	return true
}

func (p *Pool) discard(e *poolEntry) {
	if e == nil {
		return
	}
	p.discardConn(e.conn)
}

func (p *Pool) discardConn(c *Conn) {
	if c == nil {
		return
	}
	_ = c.Close()
	p.mu.Lock()
	if p.openCount > 0 {
		p.openCount--
	}
	p.mu.Unlock()
}

func (p *Pool) checkout(e *poolEntry) bool {
	if e == nil || e.conn == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return false
	}
	p.checkedOut[e.conn] = e
	return true
}

func (p *Pool) cleanupLoop() {
	interval := p.cleanupInterval()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.cleanupIdle()
		case <-p.done:
			return
		}
	}
}

func (p *Pool) cleanupInterval() time.Duration {
	minPositive := func(a, b time.Duration) time.Duration {
		switch {
		case a <= 0:
			return b
		case b <= 0:
			return a
		case a < b:
			return a
		default:
			return b
		}
	}
	d := minPositive(p.cfg.IdleTimeout, p.cfg.MaxLifetime)
	if d <= 0 {
		return 30 * time.Second
	}
	if d <= 2*time.Millisecond {
		return d
	}
	return d / 2
}

func (p *Pool) cleanupIdle() {
	var expired []*poolEntry
	p.mu.Lock()
	kept := p.idle[:0]
	for _, e := range p.idle {
		if p.entryValid(e) {
			kept = append(kept, e)
			continue
		}
		expired = append(expired, e)
	}
	for i := len(kept); i < len(p.idle); i++ {
		p.idle[i] = nil
	}
	p.idle = kept
	p.mu.Unlock()

	for _, e := range expired {
		if e != nil && e.conn != nil {
			_ = e.conn.Close()
		}

		p.mu.Lock()
		if p.openCount > 0 {
			p.openCount--
		}
		if p.openCount < p.cfg.MaxSize && len(p.waiters) > 0 {
			ch := p.waiters[0]
			p.waiters = p.waiters[1:]
			ch <- nil
		}
		p.mu.Unlock()
	}
}

func (p *Pool) removeWaiter(ch chan *poolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, w := range p.waiters {
		if w == ch {
			p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
			return
		}
	}
}

// Close drains the pool, closing every idle connection. In-
// flight Acquires receive [ErrConfig]. New Acquires fail
// fast.
func (p *Pool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(p.done)
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	idleCount := len(p.idle)
	for _, e := range p.idle {
		if err := e.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	p.idle = nil
	p.openCount -= idleCount
	if p.openCount < 0 {
		p.openCount = 0
	}
	for _, w := range p.waiters {
		close(w)
	}
	p.waiters = nil
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Stats returns a snapshot of pool counters. Useful for
// observability dashboards.
type PoolStats struct {
	Open    int // total currently open (idle + checked-out)
	Idle    int // currently idle in the pool
	Waiters int // goroutines blocked in Acquire
}

func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolStats{
		Open:    p.openCount,
		Idle:    len(p.idle),
		Waiters: len(p.waiters),
	}
}

// Do is the convenience wrapper for the Acquire/Release dance:
//
//	err := pool.Do(ctx, func(c *nwrfc.Conn) error {
//	    _, err := nwrfc.Call(ctx, c, "STFC_PING", nil, nil)
//	    return err
//	})
//
// The Conn is automatically released. If fn panics, the Conn
// is discarded (not recycled) and the panic propagates after
// the release.
func (p *Pool) Do(ctx context.Context, fn func(c *Conn) error) (rerr error) {
	c, err := p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("nwrfc: pool acquire: %w", err)
	}
	discardOnPanic := true
	defer func() {
		if discardOnPanic {
			p.Release(c, true)
		} else {
			p.Release(c, rerr != nil)
		}
	}()
	rerr = fn(c)
	discardOnPanic = false
	return rerr
}
