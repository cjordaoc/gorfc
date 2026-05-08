// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"context"
	"sync"
)

// Registration plumbing.
//
// Two backends ship in this repository: `internal/sdkbackend`
// (cgo + SAP NWRFC SDK, build tag `cgo && !nwrfc_nosdk`) and
// `internal/nosdkbackend` (build tag `!cgo || nwrfc_nosdk`).
// Their `init()` calls [Register] with their `Backend`
// implementation. Build tags ensure exactly one of them is
// compiled into any given binary.
//
// The public `nwrfc/` package retrieves the active backend via
// [Default]. Test code (and the future `nwrfcmock` package) can
// override the registration with [SetTesting] before any `Conn`
// is opened.

var (
	registryMu sync.RWMutex
	active     Backend
)

// Register installs b as the process-wide active backend. It is
// called from package init() of either `sdkbackend` or
// `nosdkbackend`. Calling Register twice is a programming bug
// and panics; build tags must guarantee only one backend is
// linked.
func Register(b Backend) {
	if b == nil {
		panic("nwrfc/internal/backend: Register called with nil Backend")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if active != nil && active.Name() != b.Name() {
		panic("nwrfc/internal/backend: two backends registered (" +
			active.Name() + " and " + b.Name() +
			"); check build tags")
	}
	active = b
}

// Default returns the registered backend. If no backend has
// registered (e.g. the consumer linked neither sdkbackend nor
// nosdkbackend, which should not be possible with default build
// settings), Default returns a sentinel that fails every
// operation with [ErrUnavailable].
func Default() Backend {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if active != nil {
		return active
	}
	return unregisteredBackend{}
}

// SetTesting replaces the active backend for the duration of a
// test. The returned restore function must be called (typically
// via t.Cleanup) to put the original backend back. Calling
// SetTesting from non-test code is allowed but discouraged; see
// the nwrfcmock package for the canonical injection point.
func SetTesting(b Backend) (restore func()) {
	registryMu.Lock()
	defer registryMu.Unlock()
	prev := active
	active = b
	return func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		active = prev
	}
}

// unregisteredBackend is the safety net when no backend has
// registered. It keeps the public API non-nil but every method
// fails explicitly. Should be unreachable in production builds.
type unregisteredBackend struct{}

func (unregisteredBackend) Name() string               { return "unregistered" }
func (unregisteredBackend) Version() Version           { return Version{} }
func (unregisteredBackend) Capabilities() Capabilities { return Capabilities{} }

func (unregisteredBackend) Open(_ context.Context, _ Params) (ConnHandle, error) {
	return 0, ErrUnavailable
}
func (unregisteredBackend) Close(ConnHandle) error                     { return ErrUnavailable }
func (unregisteredBackend) Ping(_ context.Context, _ ConnHandle) error { return ErrUnavailable }
func (unregisteredBackend) Attributes(ConnHandle) (Attributes, error) {
	return Attributes{}, ErrUnavailable
}
func (unregisteredBackend) Reset(_ context.Context, _ ConnHandle) error { return ErrUnavailable }

func (unregisteredBackend) Describe(_ context.Context, _ ConnHandle, _ string) (FunctionDescriptor, error) {
	return FunctionDescriptor{}, ErrUnavailable
}

func (unregisteredBackend) Invoke(_ context.Context, _ ConnHandle, _ string, _ CallParams, _ InvokeOptions) (CallParams, error) {
	return nil, ErrUnavailable
}

func (unregisteredBackend) InvalidateMetadata(string) error { return ErrUnavailable }
