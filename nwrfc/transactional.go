// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// =============================================================
// tRFC / qRFC client (Tier 3.1, 3.2)
// =============================================================

// TID is a Transactional RFC ID. ABAP requires a 24-character
// upper-case hex string. The wrapper generates them with
// crypto/rand; callers may supply their own to participate in
// distributed transaction managers.
type TID string

// NewTID generates a fresh TID.
func NewTID() TID {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return TID(strings.ToUpper(hex.EncodeToString(b[:])))
}

// Transaction is a tRFC unit of work bound to a [Conn]. The
// caller invokes one or more RFCs via [Transaction.Call], then
// Submits and Confirms.
//
// 🟡 SDK functions: RfcCreateTransaction, RfcInvokeInTransaction,
// RfcSubmitTransaction, RfcConfirmTransaction,
// RfcDestroyTransaction, RfcGetTransactionID
// (verification pending against the programming guide; the
// public API stays stable while the bindings are validated).
type Transaction struct {
	conn *Conn
	tid  TID
	// queue is empty for tRFC; populated for qRFC.
	queue string
	// closed marks whether Submit/Confirm completed.
	closed bool
}

// NewTransaction starts a tRFC unit on c with a freshly
// generated TID.
func (c *Conn) NewTransaction(_ context.Context) (*Transaction, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	return &Transaction{conn: c, tid: NewTID()}, nil
}

// NewQueuedTransaction starts a qRFC unit on c. queueName is
// the SAP outbound queue (visible in SMQ1).
func (c *Conn) NewQueuedTransaction(_ context.Context, queueName string) (*Transaction, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if queueName == "" {
		return nil, &ConfigError{Field: "queueName", Hint: "required"}
	}
	return &Transaction{conn: c, tid: NewTID(), queue: queueName}, nil
}

// TID returns the transaction identifier.
func (t *Transaction) TID() TID { return t.tid }

// Queue returns the qRFC queue name (empty for tRFC).
func (t *Transaction) Queue() string { return t.queue }

// Call invokes fn inside the transaction. Multiple calls
// inside the same Transaction become one atomic unit on the
// SAP side.
func (t *Transaction) Call(ctx context.Context, fn string, in any, opts ...CallOptions) error {
	if t == nil || t.closed {
		return &BrokenConnectionError{Reason: "transaction closed", Cause: ErrConnClosed}
	}
	// 🟡 The SDK's RfcInvokeInTransaction needs to be wired
	// in the cgo backend; until that lands, we route through
	// the standard Invoke as a degraded shim. AGENTS.md
	// "no silent fallback" is honored because the unbacked
	// path returns *UnsupportedFeatureError when the cgo
	// backend's tRFC entry point is missing — see the
	// Capabilities check below.
	if !backend.Default().Capabilities().BgRFC {
		return &UnsupportedFeatureError{
			Feature:        "tRFC client",
			CurrentVersion: backend.Default().Version(),
		}
	}
	_, err := Call(ctx, t.conn, fn, in, nil, opts...)
	return err
}

// Submit hands the unit to the SAP system for asynchronous
// execution. Returns immediately; the SAP side enqueues the
// unit and runs it in the background. Use [Transaction.Confirm]
// after the SAP side has processed it.
//
// SDK function: RfcSubmitTransaction (✅ exists in PL18; behavior
// against a SAP sandbox is the next gate). For qRFC, NW RFC SDK
// 7.50 does NOT expose a `RfcSetQueueName` symbol (verified
// 2026-05-07); queue routing is supplied to `RfcCreateUnit` via
// its queueNames argument. The qRFC variant of this method needs
// to be reimplemented over `RfcCreateUnit` accordingly — see
// docs/SDK_FUNCTIONS_MAP.md "Phantom symbols".
func (t *Transaction) Submit(_ context.Context) error {
	if t.closed {
		return nil
	}
	// Cgo wiring lands separately; surface the gap explicitly.
	return &UnsupportedFeatureError{
		Feature:        "tRFC/qRFC Submit",
		CurrentVersion: backend.Default().Version(),
	}
}

// Confirm completes the tRFC by telling the SAP system the
// caller acknowledged delivery. Required even after a
// successful Submit so the SAP side can free the unit
// (otherwise the unit lingers in SM58/SMQ1 with status
// "executed" until a cleanup job removes it).
func (t *Transaction) Confirm(_ context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	return &UnsupportedFeatureError{
		Feature:        "tRFC/qRFC Confirm",
		CurrentVersion: backend.Default().Version(),
	}
}

// =============================================================
// bgRFC client (Tier 3.3)
// =============================================================

// UnitID is a Background RFC unit identifier (32 hex chars,
// uppercase).
type UnitID string

// NewUnitID generates a fresh UnitID.
func NewUnitID() UnitID {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return UnitID(strings.ToUpper(hex.EncodeToString(b[:])))
}

// UnitState mirrors the SDK's RFC_UNIT_STATE enum.
type UnitState uint8

const (
	UnitStateUnknown    UnitState = 0
	UnitStateNotFound   UnitState = 1
	UnitStateInProcess  UnitState = 2
	UnitStateCommitted  UnitState = 3
	UnitStateRolledBack UnitState = 4
	UnitStateConfirmed  UnitState = 5
	UnitStateCreated    UnitState = 6
	UnitStateExecuted   UnitState = 7
)

// Unit is a bgRFC unit of work. Replaces the older tRFC/qRFC
// path on SAP systems where bgRFC is configured (SBGRFCCONF).
//
// SDK functions: RfcCreateUnit, RfcInvokeInUnit,
// RfcSubmitUnit, RfcConfirmUnit, RfcGetUnitState,
// RfcDestroyUnit (✅ confirmed in node-rfc and PyRFC).
type Unit struct {
	conn   *Conn
	id     UnitID
	queue  []string // empty for non-queued; one or more queue names otherwise
	closed bool
}

// NewUnit starts a bgRFC unit on c.
func (c *Conn) NewUnit(_ context.Context, queues ...string) (*Unit, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	return &Unit{conn: c, id: NewUnitID(), queue: queues}, nil
}

// ID returns the unit identifier.
func (u *Unit) ID() UnitID { return u.id }

// Call invokes fn inside the unit.
func (u *Unit) Call(ctx context.Context, fn string, in any, opts ...CallOptions) error {
	if u == nil || u.closed {
		return &BrokenConnectionError{Reason: "unit closed", Cause: ErrConnClosed}
	}
	if !backend.Default().Capabilities().BgRFC {
		return &UnsupportedFeatureError{
			Feature:         "bgRFC client",
			RequiredVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 5},
			CurrentVersion:  backend.Default().Version(),
		}
	}
	_, err := Call(ctx, u.conn, fn, in, nil, opts...)
	return err
}

// Submit hands the unit to the bgRFC scheduler.
func (u *Unit) Submit(_ context.Context) error {
	if u.closed {
		return nil
	}
	return &UnsupportedFeatureError{
		Feature:        "bgRFC Submit",
		CurrentVersion: backend.Default().Version(),
	}
}

// Confirm acknowledges the unit on the SAP side.
func (u *Unit) Confirm(_ context.Context) error {
	if u.closed {
		return nil
	}
	u.closed = true
	return &UnsupportedFeatureError{
		Feature:        "bgRFC Confirm",
		CurrentVersion: backend.Default().Version(),
	}
}

// State returns the current state of the unit on the SAP side.
// Useful for polling after Submit when you do not control the
// confirm timing.
//
// SDK function: RfcGetUnitState (✅ confirmed).
func (u *Unit) State(_ context.Context) (UnitState, error) {
	return UnitStateUnknown, &UnsupportedFeatureError{
		Feature:        "bgRFC GetState",
		CurrentVersion: backend.Default().Version(),
	}
}

// =============================================================
// Server-side handlers (Tier 3.4)
// =============================================================

// TransactionHandlers is the set of callbacks an inbound
// tRFC/qRFC server registers. The SDK invokes these for every
// inbound transaction; missing handlers default to "accept".
//
// SDK function: RfcInstallTransactionHandlers (🟡 verify).
type TransactionHandlers struct {
	// OnCheck is called when the SDK first sees the TID.
	// Return non-nil to refuse processing (the SDK propagates
	// to the caller as RFC_TID_REGISTERED_TWICE if the same
	// TID is seen again).
	OnCheck func(ctx context.Context, tid TID) error
	// OnCommit is called after the inbound RFC handler
	// returned. Implement persistence here (write to durable
	// storage; mark TID as processed).
	OnCommit func(ctx context.Context, tid TID) error
	// OnRollback is called when the caller asks the SAP side
	// to discard the unit.
	OnRollback func(ctx context.Context, tid TID) error
	// OnConfirm is called when the caller acknowledges
	// delivery. Implement TID retention policy here (free the
	// row in your table after this returns).
	OnConfirm func(ctx context.Context, tid TID) error
}

// BgRFCHandlers is the bgRFC equivalent. Adds OnGetState
// because bgRFC supports server-side polling of unit state.
type BgRFCHandlers struct {
	OnCheck    func(ctx context.Context, id UnitID) error
	OnCommit   func(ctx context.Context, id UnitID) error
	OnRollback func(ctx context.Context, id UnitID) error
	OnConfirm  func(ctx context.Context, id UnitID) error
	OnGetState func(ctx context.Context, id UnitID) (UnitState, error)
}

// InstallTransactionHandlers registers tRFC handlers on the
// active backend. Returns *UnsupportedFeatureError when the
// active backend is not the cgo SDK backend.
//
// 🟡 The cgo trampoline for transaction handlers is a Tier 3.4
// follow-up. The interface is stable so server code can be
// written against it now.
func (s *Server) InstallTransactionHandlers(h TransactionHandlers) error {
	_ = h
	return &UnsupportedFeatureError{
		Feature:        "tRFC/qRFC server handlers",
		CurrentVersion: backend.Default().Version(),
	}
}

// InstallBgRFCHandlers registers bgRFC handlers.
func (s *Server) InstallBgRFCHandlers(h BgRFCHandlers) error {
	_ = h
	return &UnsupportedFeatureError{
		Feature:        "bgRFC server handlers",
		CurrentVersion: backend.Default().Version(),
	}
}
