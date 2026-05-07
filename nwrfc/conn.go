// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Conn is a single SAP RFC client connection. Construct via
// [Open] (with explicit Params) or [OpenDest] (with a name
// resolved through sapnwrfc.ini).
//
// Concurrency contract (matches SDK contract):
//
//   - A single Conn MUST NOT be used from multiple goroutines
//     for an in-flight RFC call. The Conn keeps an internal
//     mutex that serializes Ping / Call / Describe / Reset to
//     enforce this. Callers that need parallelism should use a
//     [Pool] (T1.10) instead.
//   - [Conn.Close] is goroutine-safe and idempotent. After
//     Close returns, any subsequent operation on the Conn
//     returns *BrokenConnectionError wrapping ErrConnClosed.
//   - Cancel is goroutine-safe and runs from a separate
//     watcher goroutine arranged by the call sites that need
//     it (T1.9). The watcher does NOT acquire the Conn mutex
//     before invoking RfcCancel — by SDK design, RfcCancel
//     can be called from a different thread than the one
//     blocked in RfcInvoke.
type Conn struct {
	// Immutable after construction.
	handle  backend.ConnHandle
	backend backend.Backend
	dest    string // populated by OpenDest, empty otherwise

	// state == 1 once Close has been called. Read with
	// atomic.LoadUint32 from any goroutine; written under mu.
	state atomic.Uint32

	// Serializes all SDK calls on this Conn (ABAP context
	// continuity, SDK thread-safety contract).
	mu sync.Mutex
}

const (
	connStateOpen   = 0
	connStateClosed = 1
)

// Open establishes a connection using the typed Params. The
// active backend (cgo / nosdk / mock) handles the actual
// network setup; this function only enforces validation,
// redaction, and lifecycle bookkeeping.
//
// ctx may carry a deadline; the backend honors it (the cgo
// backend arranges a watcher goroutine that calls RfcCancel
// when ctx is cancelled).
//
// On error, no Conn is returned and no resources need cleanup
// — the backend handles partial-Open rollback internally.
func Open(ctx context.Context, p Params) (*Conn, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	b := backend.Default()
	bp := p.toBackendParams()
	h, err := b.Open(ctx, bp)
	if err != nil {
		return nil, mapBackendError(err)
	}
	return &Conn{
		handle:  h,
		backend: b,
		dest:    p.Dest,
	}, nil
}

// OpenDest establishes a connection using a destination name
// resolved through sapnwrfc.ini (or the active IniFS in T2.5).
// Equivalent to populating Params{Dest: name}.
func OpenDest(ctx context.Context, name string) (*Conn, error) {
	if name == "" {
		return nil, &ConfigError{Field: "dest", Hint: "destination name is required"}
	}
	return Open(ctx, Params{Dest: name})
}

// Close releases the connection. Idempotent; safe to call from
// any goroutine. Returns nil on second and later calls.
//
// Close is the canonical pattern for freeing the SDK
// connection handle; the wrapper deliberately does NOT install
// a runtime.SetFinalizer to release the handle, because the
// SDK's reentrancy rules mean we cannot guarantee the goroutine
// running the finalizer is one the handle hasn't been used by.
// Forgetting to Close leaks the handle until process exit.
func (c *Conn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.CompareAndSwap(connStateOpen, connStateClosed) {
		return nil
	}
	if err := c.backend.Close(c.handle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Ping sends an RFC ping over the connection. Returns nil on
// success; otherwise an error of category Communication or
// BrokenConn.
func (c *Conn) Ping(ctx context.Context) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.backend.Ping(ctx, c.handle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Reset clears the ABAP session state on the SAP system
// (RfcResetServerContext). Used between Pool checkouts to
// prevent LUW state leaking across callers.
func (c *Conn) Reset() error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.backend.Reset(c.handle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Attributes returns the SAP-side connection metadata
// populated after Open.
func (c *Conn) Attributes() (backend.Attributes, error) {
	if err := c.checkOpen(); err != nil {
		return backend.Attributes{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.backend.Attributes(c.handle)
	if err != nil {
		return backend.Attributes{}, mapBackendError(err)
	}
	return a, nil
}

// Alive reports whether Close has not been called on this Conn.
// It does NOT round-trip to the SAP system; for a liveness
// probe use Ping. Cheap, lock-free.
func (c *Conn) Alive() bool {
	if c == nil {
		return false
	}
	return c.state.Load() == connStateOpen
}

// Backend exposes the active backend that owns this Conn. Used
// by tests to drive the mock; production callers should not
// reach for this.
func (c *Conn) Backend() backend.Backend { return c.backend }

// Handle exposes the opaque connection handle. Used by the
// internal Call / Describe path; external code does not need
// to look at this.
func (c *Conn) Handle() backend.ConnHandle { return c.handle }

// Lock and Unlock manage the per-Conn mutex from outside the
// package. Used by the internal Call / Describe path
// implemented in T1.8 to serialize SDK access. Public for the
// benefit of the Pool (T1.10) which composes Conn with its own
// queue. NOT part of the stable user API; will become internal
// once T1.8 lands.
func (c *Conn) Lock()   { c.mu.Lock() }
func (c *Conn) Unlock() { c.mu.Unlock() }

// LogValue redacts the connection identity in logs. Emits the
// destination name (if any), the system ID (if attributes were
// fetched), and the open/closed state.
func (c *Conn) LogValue() slog.Value {
	state := "open"
	if !c.Alive() {
		state = "closed"
	}
	return slog.GroupValue(
		slog.String("backend", c.backend.Name()),
		slog.String("dest", c.dest),
		slog.String("state", state),
	)
}

// checkOpen returns *BrokenConnectionError when the Conn has
// been closed.
func (c *Conn) checkOpen() error {
	if c == nil || c.state.Load() == connStateClosed {
		return &BrokenConnectionError{Reason: "use after Close", Cause: ErrConnClosed}
	}
	return nil
}

// mapBackendError converts a backend-level error into the
// public typed error hierarchy.
//
// The cgo backend emits *backend.SDKError carrying the decoded
// RFC_ERROR_INFO; we dispatch on Group to construct the right
// typed struct (LogonError, ABAPApplicationError, ...).
//
// The no-SDK backend wraps backend.ErrUnavailable, which we
// translate into *SDKUnavailableError so callers can match
// against ErrSDKUnavailable.
func mapBackendError(err error) error {
	if err == nil {
		return nil
	}
	// Already a typed RFCError? Pass through unchanged.
	if _, ok := err.(RFCError); ok {
		return err
	}
	// SDK-side error from the cgo backend.
	var sdkErr *backend.SDKError
	if errors.As(err, &sdkErr) {
		return sdkErrorToTyped(sdkErr)
	}
	// no-SDK / sdk-pending: surface as SDKUnavailableError so
	// callers can branch via errors.Is(err, ErrSDKUnavailable).
	if errors.Is(err, backend.ErrUnavailable) {
		return &SDKUnavailableError{Reason: err.Error()}
	}
	if errors.Is(err, backend.ErrUnsupported) {
		return &UnsupportedFeatureError{Feature: "(unspecified)", CurrentVersion: backend.Default().Version()}
	}
	return err
}

// sdkErrorToTyped maps *backend.SDKError to the right typed
// nwrfc error struct based on the SDK error group.
func sdkErrorToTyped(e *backend.SDKError) error {
	info := SDKErrorInfo{
		Code:          e.Info.Code,
		Group:         e.Info.Group,
		Key:           e.Info.Key,
		Message:       e.Info.Message,
		AbapMsgClass:  e.Info.AbapMsgClass,
		AbapMsgType:   e.Info.AbapMsgType,
		AbapMsgNumber: e.Info.AbapMsgNumber,
		AbapMsgV1:     e.Info.AbapMsgV1,
		AbapMsgV2:     e.Info.AbapMsgV2,
		AbapMsgV3:     e.Info.AbapMsgV3,
		AbapMsgV4:     e.Info.AbapMsgV4,
	}
	switch e.Info.Group {
	case backend.GroupLogonFailure:
		return &LogonError{SDKErrorInfo: info}
	case backend.GroupCommunicationFailure:
		return &CommunicationError{SDKErrorInfo: info}
	case backend.GroupAbapApplicationFailure:
		return &ABAPApplicationError{SDKErrorInfo: info, Function: e.Op}
	case backend.GroupAbapRuntimeFailure:
		return &ABAPRuntimeError{SDKErrorInfo: info, Function: e.Op}
	case backend.GroupExternalAuthorizationFailure:
		return &ExternalAuthorizationError{SDKErrorInfo: info}
	case backend.GroupExternalApplicationFailure:
		return &ExternalApplicationError{SDKErrorInfo: info, Function: e.Op}
	case backend.GroupExternalRuntimeFailure:
		return &ExternalRuntimeError{SDKErrorInfo: info}
	default:
		// Unknown group: surface as ExternalRuntime so the
		// caller still gets a typed error; the Code/Key are
		// preserved for diagnosis.
		return &ExternalRuntimeError{SDKErrorInfo: info}
	}
}
