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
	"sync"
	"sync/atomic"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// HandlerFunc processes a single mocked Invoke. Returning a
// non-nil error becomes the [backend.Backend.Invoke] error;
// return *backend.SDKError to simulate SDK-level failures.
type HandlerFunc func(ctx context.Context, in backend.CallParams) (backend.CallParams, error)

// Mock is a pure-Go [backend.Backend] for tests.
type Mock struct {
	mu          sync.RWMutex
	handlers    map[string]HandlerFunc
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

func (m *Mock) Reset(h backend.ConnHandle) error {
	if _, ok := m.conns.Load(h); !ok {
		return fmt.Errorf("nwrfcmock: Reset on unknown handle %d", h)
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

func (m *Mock) InvalidateMetadata(fn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.descriptors, fn)
	return nil
}
