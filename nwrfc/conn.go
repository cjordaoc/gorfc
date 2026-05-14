// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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

	// tp is the optional Throughput collector bound via
	// [Throughput.Attach]. When non-nil, a successful [Call]
	// feeds the Go-side fallback counter. Set at setup time;
	// read on the call path.
	tp *Throughput
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

	// Capability hardlock for WebSocket RFC. Silent downgrade
	// to a non-WSRFC connection on an SDK PL that does not
	// support it would be an information-leak / connectivity
	// risk; fail-fast instead. AGENTS.md "no silent fallback".
	if err := enforceCapabilities(p, b); err != nil {
		fireEvent(EventBroken, p.Dest, err)
		return nil, err
	}

	// Honor process-global MaxTraceLevel cap if set on this
	// Params. Stamping the cap here means the cap is in place
	// for the lifetime of the process from the first Open
	// onwards; subsequent Params{MaxTraceLevel: X} only
	// tightens (a looser value cannot relax the cap).
	if p.MaxTraceLevel > 0 {
		setMaxTraceLevelFloor(p.MaxTraceLevel)
	}

	bp := p.toBackendParams()
	h, err := b.Open(ctx, bp)
	if err != nil {
		mapped := mapBackendError(err)
		fireEvent(EventBroken, p.Dest, mapped)
		return nil, mapped
	}
	c := &Conn{
		handle:  h,
		backend: b,
		dest:    p.Dest,
	}
	fireEvent(EventOpened, p.Dest, nil)
	return c, nil
}

// OpenDest establishes a connection using a destination name.
// Resolution order:
//
//  1. The registered DestinationProvider, if any.
//  2. The registered IniFS, if any.
//  3. The SDK's built-in sapnwrfc.ini lookup.
//
// Equivalent to populating Params{Dest: name} when neither a
// provider nor an IniFS is registered.
func OpenDest(ctx context.Context, name string) (*Conn, error) {
	if name == "" {
		return nil, &ConfigError{Field: "dest", Hint: "destination name is required"}
	}
	// 1. DestinationProvider takes precedence.
	if got, err := resolveDestination(ctx, name); err != nil {
		return nil, err
	} else if got.AsHost != "" || got.MsHost != "" || got.WSHost != "" {
		return Open(ctx, got)
	}
	// 2. IniFS lookup.
	if got, ok, err := resolveIniDest(ctx, name); err != nil {
		return nil, err
	} else if ok {
		return Open(ctx, got)
	}
	// 3. SDK fallback (the SDK reads sapnwrfc.ini from disk).
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
		mapped := mapBackendError(err)
		fireEvent(EventClosed, c.dest, mapped)
		return mapped
	}
	fireEvent(EventClosed, c.dest, nil)
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
//
// Honors ctx via the same cancel-watcher contract as Ping /
// Invoke: a cancelled ctx returns *TimeoutError or
// *CancelledError; otherwise the backend's RC is mapped
// through the typed error hierarchy.
func (c *Conn) Reset(ctx context.Context) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.backend.Reset(ctx, c.handle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Cancel asks the SDK to interrupt any RFC call currently
// blocking on this Conn (Open, Ping, Reset, Describe, Invoke).
// Idempotent: repeated calls return nil. Safe to call from a
// goroutine other than the one in flight — that is the entire
// point. See docs/EVIDENCE/sdk-cancel.md for the SDK contract.
//
// Cancel does NOT close the connection. The blocked goroutine
// unblocks with *CancelledError; the caller is then expected
// to call Close to release the SDK handle.
//
// SAP CAVEAT: cancelling a mid-flight Update / Insert / Delete
// BAPI or any mutating function module may leave the ABAP side
// in an indeterminate state. The ABAP work process may have
// committed the change, may have rolled back, or may be holding
// a partial transaction. For mutating operations, prefer:
//
//   - generous per-call ctx deadlines instead of mid-call cancel,
//   - explicit transactional design (tRFC / qRFC / bgRFC),
//   - SAP-side state confirmation via a separate read after a
//     cancellation.
//
// Cancel is safe and recommended for read-only / idempotent
// FMs, the handshake during Open, Ping, Describe, Reset, and
// any operation the operator has classified as `read` or
// `idempotent` (see docs/EVIDENCE/SCHEMA.md).
//
// If the active backend does not advertise the
// [backend.Cancellable] capability (the no-SDK stub or a
// mock), Cancel returns *UnsupportedFeatureError.
func (c *Conn) Cancel() error {
	if c == nil {
		return nil
	}
	// Use after Close: idempotent no-op. The SDK call would
	// also be safe, but skipping it avoids a registry miss in
	// the cgo backend's bookkeeping.
	if c.state.Load() == connStateClosed {
		return nil
	}
	cancellable, ok := c.backend.(backend.Cancellable)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "Cancel",
			CurrentVersion: c.backend.Version(),
		}
	}
	if err := cancellable.Cancel(c.handle); err != nil {
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
	// ctx-cancellation sentinels from the cgo invoke watcher.
	if errors.Is(err, backend.ErrTimeout) {
		return &TimeoutError{Function: extractFunctionName(err)}
	}
	if errors.Is(err, backend.ErrCancelled) {
		return &CancelledError{Function: extractFunctionName(err), Cause: err}
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

// extractFunctionName pulls "RfcInvoke(FOO)" out of the
// backend's wrapped error. Best-effort; returns "(unknown)"
// if the format does not match.
func extractFunctionName(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "RfcInvoke("); i >= 0 {
		rest := msg[i+len("RfcInvoke("):]
		if j := strings.Index(rest, ")"); j >= 0 {
			return rest[:j]
		}
	}
	return "(unknown)"
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
		// Refine into PasswordExpired / UserLocked /
		// InvalidCredentials / UnknownLogonFailure based on
		// the SDK-reported Key + Message. The classifier
		// preserves errors.Is(err, ErrLogon) for every subtype
		// (see nwrfc/errors_logon.go).
		return buildLogonError(info, LogonErrorContext{})
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
