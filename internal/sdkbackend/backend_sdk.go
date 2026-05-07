// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// Package sdkbackend implements [backend.Backend] over the SAP
// NetWeaver RFC SDK via cgo.
//
// This file registers the package with the backend registry
// (init) and exposes the package-level entry point.
//
// Real cgo bindings live in this directory:
//
//   - conn.go     — Open / Close / Ping / Attributes / Reset
//   - describe.go — Function and type descriptors
//   - invoke.go   — Invoke + ctx-cancel watcher
//   - fill.go     — Go → ABAP marshaling
//   - wrap.go     — ABAP → Go marshaling
//   - errors.go   — RFC_ERROR_INFO → typed nwrfc errors
//   - version.go  — RfcGetVersion / capability detection
//
// 🟡 Verification status: bindings compile against SAP
// NetWeaver RFC SDK 7.50 PL12+ when CGO_CFLAGS / CGO_LDFLAGS
// resolve `<sapnwrfc.h>`. Each binding site marks its precise
// SDK function reference; runtime validation against a live SAP
// system is per-PR work tracked in docs/SDK_FUNCTIONS_MAP.md.
package sdkbackend

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func init() {
	backend.Register(theBackend)
}

// theBackend is the singleton instance registered with the
// backend registry. The struct holds no state beyond the
// connection registry; per-Conn state lives in connHandle.
var theBackend = &sdkBackend{
	conns: connRegistry{m: make(map[backend.ConnHandle]*connHandle)},
}

// sdkBackend implements [backend.Backend] for the cgo SDK
// binding. Methods delegate to per-Conn helpers in conn.go,
// describe.go, and invoke.go.
type sdkBackend struct {
	conns connRegistry

	// initOnce gates RfcInit which the SDK requires before any
	// other call. We never call RfcInit explicitly because the
	// SDK calls it lazily on the first RfcOpenConnection; this
	// flag is documented for future tracing hooks (PLAN.md §6).
	initOnce sync.Once
}

func (*sdkBackend) Name() string { return "sdk" }

// connHandle is the per-connection state owned by the backend.
// The opaque [backend.ConnHandle] handed to callers is a uint64
// key into [connRegistry]; the actual SDK pointer
// (RFC_CONNECTION_HANDLE) lives in the cgo file conn.go and is
// keyed by the same uint64 to avoid leaking unsafe.Pointer
// across the public boundary.
type connHandle struct {
	// id is the opaque key handed back to callers.
	id backend.ConnHandle

	// state: 0=open, 1=closed. Read with atomics; written under
	// mu from the call sites in conn.go.
	state atomic.Uint32

	// sdkPtr is the cgo-wrapped RFC_CONNECTION_HANDLE. Stored
	// as a unsafe.Pointer-sized uintptr so this file does NOT
	// import "C" (the cgo file conn.go owns the actual type).
	sdkPtr uintptr

	// mu serializes SDK calls on the same handle (the SDK's
	// thread-safety contract).
	mu sync.Mutex
}

// connRegistry maps opaque IDs to *connHandle. Centralizes the
// "one ID issuer per process" invariant.
type connRegistry struct {
	mu   sync.Mutex
	next uint64
	m    map[backend.ConnHandle]*connHandle
}

func (r *connRegistry) put(c *connHandle) backend.ConnHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id := backend.ConnHandle(r.next)
	c.id = id
	r.m[id] = c
	return id
}

func (r *connRegistry) get(id backend.ConnHandle) (*connHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.m[id]
	if !ok {
		return nil, fmt.Errorf("nwrfc/sdkbackend: unknown connection handle %d", id)
	}
	return c, nil
}

func (r *connRegistry) remove(id backend.ConnHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

// =============================================================
// Backend interface methods. The bodies dispatch to the cgo
// file (conn.go, describe.go, invoke.go) which has access to
// "C" via cgo.
// =============================================================

func (b *sdkBackend) Open(ctx context.Context, p backend.Params) (backend.ConnHandle, error) {
	c, err := openConn(ctx, p)
	if err != nil {
		return 0, err
	}
	return b.conns.put(c), nil
}

func (b *sdkBackend) Close(h backend.ConnHandle) error {
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	defer b.conns.remove(h)
	return closeConn(c)
}

func (b *sdkBackend) Ping(ctx context.Context, h backend.ConnHandle) error {
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	return pingConn(ctx, c)
}

func (b *sdkBackend) Attributes(h backend.ConnHandle) (backend.Attributes, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return backend.Attributes{}, err
	}
	return connAttributes(c)
}

func (b *sdkBackend) Reset(h backend.ConnHandle) error {
	c, err := b.conns.get(h)
	if err != nil {
		return err
	}
	return resetConn(c)
}

func (b *sdkBackend) Describe(ctx context.Context, h backend.ConnHandle, fn string) (backend.FunctionDescriptor, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return backend.FunctionDescriptor{}, err
	}
	return describeFunction(ctx, c, fn)
}

func (b *sdkBackend) Invoke(ctx context.Context, h backend.ConnHandle, fn string, in backend.CallParams, opts backend.InvokeOptions) (backend.CallParams, error) {
	c, err := b.conns.get(h)
	if err != nil {
		return nil, err
	}
	return invokeFunction(ctx, c, fn, in, opts)
}

func (b *sdkBackend) InvalidateMetadata(fn string) error {
	return invalidateMetadata(fn)
}
