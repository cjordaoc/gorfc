// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package backend defines the contract every gorfc backend must
// satisfy: the cgo-driven SAP NetWeaver RFC SDK backend
// (`internal/sdkbackend`), the SDK-free stub
// (`internal/nosdkbackend`), and the user-facing test mock
// (`nwrfcmock`, Tier 4) all implement [Backend].
//
// The contract is deliberately coarse-grained: a connection handle
// is opaque to callers, and RFC parameters cross the boundary as
// dynamic [CallParams] maps. Typed marshaling — struct tags,
// generics, ABAP type policy — sits in the public `nwrfc/` package
// and is composed on top of this contract. Keeping the boundary
// dynamic means a pure-Go mock can satisfy the same interface
// without re-implementing the SAP type system in C.
//
// Concurrency: a [ConnHandle] is owned by a single caller at a
// time. Backends MUST serialize SDK calls per handle (see
// docs/PLAN.md §6.5) but MAY be invoked from any goroutine when
// the handle is not in flight. [Backend.Cancel] is the explicit
// cross-goroutine signal: it MUST be safe to call while another
// goroutine is blocked in [Backend.Invoke] on the same handle.
//
// Lifetime: a [ConnHandle] is valid from the moment
// [Backend.Open] returns until [Backend.Close] returns. Callers
// MUST NOT use the handle after [Backend.Close]; backends MAY
// detect and reject use-after-close with an error of category
// CategoryBrokenConn but are not required to.
package backend

import (
	"context"
	"errors"
)

// Backend is the contract that the cgo SDK bindings, the no-SDK
// stub, and the test mock all satisfy.
//
// Errors returned by Backend methods carry one of the categories
// listed in [Category]; callers can branch on the category via
// [errors.Is] / [errors.As] against the sentinels exported by the
// public `nwrfc` package. Backends should never panic; cgo
// trampolines in particular MUST recover and convert to error
// (see docs/PLAN.md §6.6).
type Backend interface {
	// Name identifies the backend in logs and traces. Stable
	// across versions for a given backend.
	Name() string

	// Version returns the SAP NWRFC SDK version reported by
	// `RfcGetVersion`. Returns a zero Version for backends that
	// do not link against the SDK (the no-SDK stub and the mock).
	Version() Version

	// Capabilities returns the set of features the SDK at the
	// detected patch level can offer. Used by the public API to
	// gate WebSocket RFC, throughput, bgRFC, and similar
	// capability-detected features.
	Capabilities() Capabilities

	// Open establishes a client connection using the parameters
	// in p. The returned [ConnHandle] is opaque to callers.
	//
	// Open MUST honor ctx for cancellation: implementations
	// should arrange for `RfcCancel` (or the equivalent
	// shutdown signal) to fire when ctx is cancelled before the
	// connection completes.
	Open(ctx context.Context, p Params) (ConnHandle, error)

	// Close releases the connection. After Close returns, the
	// [ConnHandle] is invalid; using it with any other Backend
	// method MUST return an error.
	//
	// Close is idempotent: calling it twice on the same handle
	// is allowed and the second call returns nil.
	Close(h ConnHandle) error

	// Ping sends an RFC ping over the connection. Returns nil
	// if the SAP system answered, or an error categorized
	// CategoryBroken / CategoryCommunication / CategoryTimeout.
	Ping(ctx context.Context, h ConnHandle) error

	// Attributes returns the connection metadata the SDK
	// populates after a successful Open: peer host, system ID,
	// codepage, role, and the rest documented in
	// `RfcGetConnectionAttributes`.
	Attributes(h ConnHandle) (Attributes, error)

	// Reset clears the ABAP session state on the peer
	// (`RfcResetServerContext`). Used between calls in a Pool
	// to avoid LUW leakage.
	Reset(h ConnHandle) error

	// Describe fetches the metadata for the named RFC function.
	// Backends MAY cache the result; callers can force a refetch
	// via [Backend.InvalidateMetadata].
	Describe(ctx context.Context, h ConnHandle, fn string) (FunctionDescriptor, error)

	// Invoke executes the named RFC function. in carries the
	// IMPORT and CHANGING parameters; the EXPORT, CHANGING, and
	// TABLES results are returned in the response CallParams.
	//
	// Invoke MUST honor ctx: if ctx is cancelled while the SDK
	// call is in flight, the implementation must call
	// `RfcCancel` on the handle from a separate goroutine and
	// return an error of category [CategoryCancelled] or
	// [CategoryTimeout].
	Invoke(ctx context.Context, h ConnHandle, fn string, in CallParams, opts InvokeOptions) (CallParams, error)

	// InvalidateMetadata removes the cached descriptor for fn so
	// the next Describe / Invoke refetches from the SAP system.
	InvalidateMetadata(fn string) error
}

// Trace is an optional capability-extension for backends that can
// adjust SDK trace settings at runtime. The cgo backend implements
// it; the mock and no-SDK stub do not.
type Trace interface {
	SetTraceLevel(level int) error
	SetTraceDir(dir string) error
}

// IniReloader is an optional capability-extension for backends
// that can reload `sapnwrfc.ini` (`RfcReloadIniFile`).
type IniReloader interface {
	SetIniPath(dir string) error
	ReloadIniFile() error
}

// LanguageConverter is an optional capability-extension for
// backends that wrap `RfcLanguageIsoToSap` and the inverse.
type LanguageConverter interface {
	LanguageISOToSAP(iso string) (string, error)
	LanguageSAPToISO(sap string) (string, error)
}

// CryptoLoader is an optional capability-extension for backends
// that wrap `RfcLoadCryptoLibrary` for SNC / WebSocket TLS.
type CryptoLoader interface {
	LoadCryptoLibrary(path string) error
}

// ErrUnavailable is returned by the no-SDK stub backend on every
// operation and may be checked with [errors.Is].
var ErrUnavailable = errors.New("nwrfc: SAP NetWeaver RFC SDK is not available in this build")

// ErrUnsupported indicates the active SDK lacks the capability
// the caller asked for (e.g. WebSocket RFC on PL < 7.50 PL10).
var ErrUnsupported = errors.New("nwrfc: feature not supported by this SDK version")

// ErrUnknownType is wrapped by marshal failures when a future
// SDK release reports an RFCTYPE this library does not handle.
// The public nwrfc package re-exports the same sentinel value.
var ErrUnknownType = errors.New("nwrfc: unknown ABAP RFC type")

// ErrTimeout is set as Op on backend.SDKError when ctx hit its
// deadline mid-call. The public nwrfc.mapBackendError detects
// it and returns *nwrfc.TimeoutError.
var ErrTimeout = errors.New("nwrfc: ctx deadline exceeded mid-call")

// ErrCancelled is set as Op on backend.SDKError when ctx was
// cancelled mid-call. Public nwrfc.mapBackendError translates
// to *nwrfc.CancelledError.
var ErrCancelled = errors.New("nwrfc: ctx cancelled mid-call")
