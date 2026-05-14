// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package nwrfcmock is a pure-Go implementation of
// [backend.Backend] that simulates the SAP NetWeaver RFC SDK
// without cgo or a SAP system. Use it to test code that calls
// [nwrfc.Call] / [nwrfc.Conn] / [nwrfc.Pool] / [nwrfc.Session]
// without any SAP infrastructure.
//
// Tier 4.1 deliverable per docs/PLAN.md §10.
//
// Example:
//
//	func TestMyService(t *testing.T) {
//	    mock := nwrfcmock.New()
//	    mock.HandleFunc("STFC_CONNECTION", func(in backend.CallParams) (backend.CallParams, error) {
//	        return backend.CallParams{
//	            "ECHOTEXT": in["REQUTEXT"],
//	            "RESPTEXT": "pong",
//	        }, nil
//	    })
//	    restore := nwrfcmock.Install(mock)
//	    t.Cleanup(restore)
//
//	    // Now nwrfc.Call() against any "Conn" routes to the mock.
//	}
//
// Threading: all methods are safe for concurrent use. Handlers
// run on the goroutine performing the call; long-running or
// blocking work in a handler should respect ctx.
package nwrfcmock

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// CallParams is the public mock-side alias for dynamic RFC parameters.
// It lets generated tests name the map shape without importing the
// library's internal backend package.
type CallParams = backend.CallParams

// HandlerFunc processes a single mocked Invoke. Returning a
// non-nil error becomes the [backend.Backend.Invoke] error;
// return *backend.SDKError to simulate SDK-level failures.
type HandlerFunc func(ctx context.Context, in CallParams) (CallParams, error)

// StreamHandlerFunc processes a mocked lazy table invocation.
// It returns a backend.TableStream whose Close method is owned
// by the nwrfc.TableStream returned to callers.
type StreamHandlerFunc func(ctx context.Context, in backend.CallParams) (backend.TableStream, error)

// Mock is a pure-Go [backend.Backend] for tests.
type Mock struct {
	mu          sync.RWMutex
	handlers    map[string]HandlerFunc
	streams     map[string]map[string]StreamHandlerFunc
	descriptors map[string]backend.FunctionDescriptor

	// Counters: useful in tests to assert "this RFC was called N times".
	callCount  atomic.Int64
	openCount  atomic.Int64
	closeCount atomic.Int64

	nextHandle atomic.Uint64
	conns      sync.Map // backend.ConnHandle → struct{} (open only)

	// Version + Capabilities are configurable so tests can
	// simulate different SDK releases.
	version      backend.Version
	capabilities backend.Capabilities
}

// New constructs a Mock with sane defaults: SDK 7.50 PL18, all
// capabilities enabled.
func New() *Mock {
	return &Mock{
		handlers:    make(map[string]HandlerFunc),
		streams:     make(map[string]map[string]StreamHandlerFunc),
		descriptors: make(map[string]backend.FunctionDescriptor),
		version: backend.Version{
			Major: 7, Minor: 50, PatchLevel: 18,
		},
		capabilities: backend.Capabilities{
			WebSocketRFC: true, Throughput: true, BgRFC: true,
			UTCLong: true, FastSerialization: true,
		},
	}
}

// Install replaces the active backend with m and returns a
// restore function. Use with t.Cleanup.
func Install(m *Mock) (restore func()) {
	return backend.SetTesting(m)
}

// HandleFunc registers a handler for the named RFC.
func (m *Mock) HandleFunc(fn string, h HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[fn] = h
}

// HandleTableStreamFunc registers a lazy TABLES handler for fn/table.
func (m *Mock) HandleTableStreamFunc(fn string, table string, h StreamHandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streams[fn] == nil {
		m.streams[fn] = make(map[string]StreamHandlerFunc)
	}
	m.streams[fn][table] = h
}

// SetDescriptor pre-populates the metadata for fn. nwrfc.Conn.Describe
// will return this without calling the (missing) SAP system.
func (m *Mock) SetDescriptor(fn string, d backend.FunctionDescriptor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.descriptors[fn] = d
}

// SetVersion overrides the simulated SDK version.
func (m *Mock) SetVersion(v backend.Version) { m.version = v }

// SetCapabilities overrides the simulated capability set.
func (m *Mock) SetCapabilities(c backend.Capabilities) { m.capabilities = c }

// CallCount returns the total number of Invokes since New().
func (m *Mock) CallCount() int64 { return m.callCount.Load() }

// OpenCount / CloseCount likewise.
func (m *Mock) OpenCount() int64  { return m.openCount.Load() }
func (m *Mock) CloseCount() int64 { return m.closeCount.Load() }

// =============================================================
// backend.Backend implementation
// =============================================================

func (m *Mock) Name() string                       { return "mock" }
func (m *Mock) Version() backend.Version           { return m.version }
func (m *Mock) Capabilities() backend.Capabilities { return m.capabilities }

func (m *Mock) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	m.openCount.Add(1)
	h := backend.ConnHandle(m.nextHandle.Add(1))
	m.conns.Store(h, struct{}{})
	return h, nil
}

func (m *Mock) Close(h backend.ConnHandle) error {
	m.closeCount.Add(1)
	m.conns.Delete(h)
	return nil
}

func (m *Mock) Ping(_ context.Context, h backend.ConnHandle) error {
	if _, ok := m.conns.Load(h); !ok {
		return fmt.Errorf("nwrfcmock: Ping on unknown handle %d", h)
	}
	return nil
}

func (m *Mock) Attributes(h backend.ConnHandle) (backend.Attributes, error) {
	if _, ok := m.conns.Load(h); !ok {
		return backend.Attributes{}, fmt.Errorf("nwrfcmock: Attributes on unknown handle %d", h)
	}
	return backend.Attributes{
		SysID:    "MCK",
		Client:   "100",
		User:     "MOCK",
		Language: "EN",
		RfcRole:  "C", // client
	}, nil
}

func (m *Mock) Reset(_ context.Context, h backend.ConnHandle) error {
	if _, ok := m.conns.Load(h); !ok {
		return fmt.Errorf("nwrfcmock: Reset on unknown handle %d", h)
	}
	return nil
}

// Cancel implements [backend.Cancellable]. The mock has no
// real SDK call to interrupt, so Cancel is a typed no-op for
// known handles and surfaces an error for unknown handles —
// matching Reset / Ping shape so tests catch handle-leak bugs.
func (m *Mock) Cancel(h backend.ConnHandle) error {
	if _, ok := m.conns.Load(h); !ok {
		return fmt.Errorf("nwrfcmock: Cancel on unknown handle %d", h)
	}
	return nil
}

func (m *Mock) Describe(_ context.Context, h backend.ConnHandle, fn string) (backend.FunctionDescriptor, error) {
	if _, ok := m.conns.Load(h); !ok {
		return backend.FunctionDescriptor{}, fmt.Errorf("nwrfcmock: Describe on unknown handle %d", h)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.descriptors[fn]; ok {
		return d, nil
	}
	return backend.FunctionDescriptor{}, fmt.Errorf("nwrfcmock: no descriptor for %s (use SetDescriptor)", fn)
}

func (m *Mock) Invoke(ctx context.Context, h backend.ConnHandle, fn string, in backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	m.callCount.Add(1)
	if _, ok := m.conns.Load(h); !ok {
		return nil, fmt.Errorf("nwrfcmock: Invoke on unknown handle %d", h)
	}
	m.mu.RLock()
	handler := m.handlers[fn]
	m.mu.RUnlock()
	if handler == nil {
		return nil, fmt.Errorf("nwrfcmock: no handler for %s (use HandleFunc)", fn)
	}
	return handler(ctx, in)
}

func (m *Mock) InvokeTableStream(ctx context.Context, h backend.ConnHandle, fn string, table string, in backend.CallParams, _ backend.InvokeOptions) (backend.TableStream, error) {
	m.callCount.Add(1)
	if _, ok := m.conns.Load(h); !ok {
		return nil, fmt.Errorf("nwrfcmock: InvokeTableStream on unknown handle %d", h)
	}
	m.mu.RLock()
	handler := m.streams[fn][table]
	m.mu.RUnlock()
	if handler == nil {
		return nil, fmt.Errorf("nwrfcmock: no stream handler for %s/%s (use HandleTableStreamFunc)", fn, table)
	}
	return handler(ctx, in)
}

func (m *Mock) InvalidateMetadata(fn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.descriptors, fn)
	return nil
}

// RowFactory builds a row for zero-based index i. The returned
// map escapes to the caller and is never pooled or reused by
// the mock.
type RowFactory func(i int) map[string]any

// TableRows returns a lazy stream of n rows. It is useful for
// SDK-free tests and benchmarks of streaming callers.
func TableRows(n int, row RowFactory) backend.TableStream {
	return &rowStream{n: n, row: row}
}

type rowStream struct {
	mu     sync.Mutex
	n      int
	i      int
	row    RowFactory
	closed bool
}

func (s *rowStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, io.ErrClosedPipe
	}
	if s.i >= s.n {
		return nil, io.EOF
	}
	row := s.row(s.i)
	s.i++
	return row, nil
}

func (s *rowStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
