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

// Transaction is a tRFC/qRFC unit of work bound to a [Conn].
// The caller invokes one or more RFCs via [Transaction.Call],
// then Submits and Confirms.
//
// SDK functions: RfcCreateTransaction, RfcInvokeInTransaction,
// RfcSubmitTransaction, RfcConfirmTransaction,
// RfcDestroyTransaction (✅ exist in PL18; behavior against a
// SAP system is the next gate).
//
// qRFC vs tRFC: per sapnwrfc.h §2120 the SDK distinguishes
// these by the queueName argument to RfcCreateTransaction —
// NULL means tRFC, non-NULL means qRFC. There is no separate
// `RfcSetQueueName` symbol (earlier docs that named one were
// wrong).
type Transaction struct {
	conn *Conn
	tid  TID
	// queue is empty for tRFC; populated for qRFC.
	queue string
	// txHandle is the backend's opaque key for the underlying
	// RFC_TRANSACTION_HANDLE. Zero before CreateTransaction
	// (not yet allocated) and after DestroyTransaction (freed).
	txHandle backend.TxHandle
	// closed marks whether Submit/Confirm completed.
	closed bool
}

// NewTransaction starts a tRFC unit on c with a freshly
// generated TID. The cgo backend allocates the underlying
// RFC_TRANSACTION_HANDLE here; failure paths return any backend
// error verbatim.
func (c *Conn) NewTransaction(_ context.Context) (*Transaction, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	tx, err := newTransaction(c, NewTID(), "")
	if err != nil {
		return nil, err
	}
	return tx, nil
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
	tx, err := newTransaction(c, NewTID(), queueName)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// newTransaction is the shared client-side allocator. Returns
// *UnsupportedFeatureError when the active backend does not
// satisfy [backend.TransactionalBackend] (pure-Go mock,
// nosdkbackend).
func newTransaction(c *Conn, tid TID, queueName string) (*Transaction, error) {
	tb, ok := backend.Default().(backend.TransactionalBackend)
	if !ok {
		return nil, &UnsupportedFeatureError{
			Feature:        "tRFC/qRFC client",
			CurrentVersion: backend.Default().Version(),
		}
	}
	h, err := tb.CreateTransaction(c.handle, string(tid), queueName)
	if err != nil {
		return nil, mapBackendError(err)
	}
	return &Transaction{
		conn:     c,
		tid:      tid,
		queue:    queueName,
		txHandle: h,
	}, nil
}

// TID returns the transaction identifier.
func (t *Transaction) TID() TID { return t.tid }

// Queue returns the qRFC queue name (empty for tRFC).
func (t *Transaction) Queue() string { return t.queue }

// Call invokes fn inside the transaction. Multiple calls inside
// the same Transaction become one atomic unit on the SAP side.
// Per sapnwrfc.h §2139 the EXPORTING / CHANGING / TABLES return
// values are NOT propagated back from a tRFC/qRFC call; the
// caller is expected to read effects via separate sRFC calls.
//
// SDK function: RfcInvokeInTransaction.
func (t *Transaction) Call(ctx context.Context, fn string, in any, opts ...CallOptions) error {
	if t == nil || t.closed {
		return &BrokenConnectionError{Reason: "transaction closed", Cause: ErrConnClosed}
	}
	tb, ok := backend.Default().(backend.TransactionalBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "tRFC/qRFC Call",
			CurrentVersion: backend.Default().Version(),
		}
	}
	params, err := marshalInput(in)
	if err != nil {
		return err
	}
	var co CallOptions
	if len(opts) > 0 {
		co = opts[0]
	}
	invokeOpts := co.toBackend()
	if err := tb.InvokeInTransaction(ctx, t.conn.handle, t.txHandle, fn, params, invokeOpts); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Submit hands the unit to the SAP system for asynchronous
// execution. Returns immediately; the SAP side enqueues the
// unit and runs it in the background. Use [Transaction.Confirm]
// after the SAP side has processed it.
//
// SDK function: RfcSubmitTransaction. For qRFC, NW RFC SDK 7.50
// does NOT expose a separate `RfcSetQueueName` symbol (verified
// 2026-05-07); queue routing is supplied to RfcCreateTransaction
// at creation time, not before Submit.
func (t *Transaction) Submit(_ context.Context) error {
	if t == nil || t.closed {
		return nil
	}
	tb, ok := backend.Default().(backend.TransactionalBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "tRFC/qRFC Submit",
			CurrentVersion: backend.Default().Version(),
		}
	}
	if err := tb.SubmitTransaction(t.txHandle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Confirm completes the tRFC/qRFC by telling the SAP system the
// caller acknowledged delivery. Required even after a successful
// Submit so the SAP side can free the unit (otherwise the unit
// lingers in SM58/SMQ1 with status "executed" until a cleanup
// job removes it).
//
// SDK function: RfcConfirmTransaction.
func (t *Transaction) Confirm(_ context.Context) error {
	if t == nil || t.closed {
		return nil
	}
	tb, ok := backend.Default().(backend.TransactionalBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "tRFC/qRFC Confirm",
			CurrentVersion: backend.Default().Version(),
		}
	}
	if err := tb.ConfirmTransaction(t.txHandle); err != nil {
		return mapBackendError(err)
	}
	t.closed = true
	return nil
}

// Destroy releases the local transaction container without
// telling the SAP side. Use this when CreateTransaction
// succeeded but Submit was never attempted (e.g. validation
// failed Go-side after creation). Idempotent.
//
// SDK function: RfcDestroyTransaction.
func (t *Transaction) Destroy() error {
	if t == nil || t.txHandle == 0 {
		return nil
	}
	tb, ok := backend.Default().(backend.TransactionalBackend)
	if !ok {
		return nil
	}
	err := tb.DestroyTransaction(t.txHandle)
	t.txHandle = 0
	t.closed = true
	if err != nil {
		return mapBackendError(err)
	}
	return nil
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
	UnitStateUnknown    UnitState = UnitState(backend.UnitStateUnknown)
	UnitStateNotFound   UnitState = UnitState(backend.UnitStateNotFound)
	UnitStateInProcess  UnitState = UnitState(backend.UnitStateInProcess)
	UnitStateCommitted  UnitState = UnitState(backend.UnitStateCommitted)
	UnitStateRolledBack UnitState = UnitState(backend.UnitStateRolledBack)
	UnitStateConfirmed  UnitState = UnitState(backend.UnitStateConfirmed)
	UnitStateCreated    UnitState = UnitState(backend.UnitStateCreated)
	UnitStateExecuted   UnitState = UnitState(backend.UnitStateExecuted)
)

// Unit is a bgRFC unit of work. Replaces the older tRFC/qRFC
// path on SAP systems where bgRFC is configured (SBGRFCCONF).
//
// Empty queues at creation time → synchronous unit (type 'T');
// non-empty → asynchronous queued unit (type 'Q'). The unit
// type is fixed at creation and cannot be changed afterwards.
//
// SDK functions: RfcCreateUnit, RfcInvokeInUnit, RfcSubmitUnit,
// RfcConfirmUnit, RfcGetUnitState, RfcDestroyUnit (✅ exist in
// PL18).
type Unit struct {
	conn       *Conn
	id         UnitID
	queue      []string
	uHandle    backend.UnitHandle
	identifier backend.UnitIdentifier
	closed     bool
}

// NewUnit starts a bgRFC unit on c.
func (c *Conn) NewUnit(_ context.Context, queues ...string) (*Unit, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return nil, &UnsupportedFeatureError{
			Feature:        "bgRFC client",
			CurrentVersion: backend.Default().Version(),
		}
	}
	uid := NewUnitID()
	h, ident, err := bb.CreateUnit(c.handle, string(uid), queues)
	if err != nil {
		return nil, mapBackendError(err)
	}
	return &Unit{
		conn:       c,
		id:         uid,
		queue:      queues,
		uHandle:    h,
		identifier: ident,
	}, nil
}

// ID returns the unit identifier.
func (u *Unit) ID() UnitID { return u.id }

// Type returns 'T' for synchronous units (no queues) and 'Q'
// for queued asynchronous units. Determined by the SDK at
// creation time based on the queue list.
func (u *Unit) Type() byte { return u.identifier.Type }

// Call invokes fn inside the unit.
//
// SDK function: RfcInvokeInUnit.
func (u *Unit) Call(ctx context.Context, fn string, in any, opts ...CallOptions) error {
	if u == nil || u.closed {
		return &BrokenConnectionError{Reason: "unit closed", Cause: ErrConnClosed}
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:         "bgRFC Call",
			RequiredVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 5},
			CurrentVersion:  backend.Default().Version(),
		}
	}
	params, err := marshalInput(in)
	if err != nil {
		return err
	}
	var co CallOptions
	if len(opts) > 0 {
		co = opts[0]
	}
	invokeOpts := co.toBackend()
	if err := bb.InvokeInUnit(ctx, u.conn.handle, u.uHandle, fn, params, invokeOpts); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Submit hands the unit to the bgRFC scheduler.
//
// SDK function: RfcSubmitUnit.
func (u *Unit) Submit(_ context.Context) error {
	if u == nil || u.closed {
		return nil
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "bgRFC Submit",
			CurrentVersion: backend.Default().Version(),
		}
	}
	if err := bb.SubmitUnit(u.uHandle); err != nil {
		return mapBackendError(err)
	}
	return nil
}

// Confirm acknowledges the unit on the SAP side.
//
// SDK function: RfcConfirmUnit. Per sapnwrfc.h §2305: in
// three-tier architectures, do NOT bundle Submit and Confirm.
// Confirm only after Submit has succeeded end-to-end.
func (u *Unit) Confirm(_ context.Context) error {
	if u == nil || u.closed {
		return nil
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return &UnsupportedFeatureError{
			Feature:        "bgRFC Confirm",
			CurrentVersion: backend.Default().Version(),
		}
	}
	if err := bb.ConfirmUnit(u.conn.handle, u.identifier); err != nil {
		return mapBackendError(err)
	}
	u.closed = true
	return nil
}

// State returns the current state of the unit on the SAP side.
// Useful for polling after Submit when you do not control the
// confirm timing.
//
// SDK function: RfcGetUnitState.
func (u *Unit) State(_ context.Context) (UnitState, error) {
	if u == nil {
		return UnitStateUnknown, nil
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return UnitStateUnknown, &UnsupportedFeatureError{
			Feature:        "bgRFC GetState",
			CurrentVersion: backend.Default().Version(),
		}
	}
	s, err := bb.GetUnitState(u.conn.handle, u.identifier)
	if err != nil {
		return UnitStateUnknown, mapBackendError(err)
	}
	return UnitState(s), nil
}

// Destroy releases the local unit container without telling the
// SAP side. Idempotent.
//
// SDK function: RfcDestroyUnit.
func (u *Unit) Destroy() error {
	if u == nil || u.uHandle == 0 {
		return nil
	}
	bb, ok := backend.Default().(backend.BgRFCBackend)
	if !ok {
		return nil
	}
	err := bb.DestroyUnit(u.uHandle)
	u.uHandle = 0
	u.closed = true
	if err != nil {
		return mapBackendError(err)
	}
	return nil
}

// =============================================================
// Server-side handlers (Tier 3.4)
// =============================================================

// TransactionHandlers is the set of callbacks an inbound
// tRFC/qRFC server registers. The SDK invokes these for every
// inbound transaction; missing handlers default to "accept".
//
// SDK function: RfcInstallTransactionHandlers (✅ exists in
// PL18; the cgo trampoline that bridges Go callbacks into
// SDK-compatible C function pointers is a Tier-3.4 follow-up).
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
// active backend. Returns *UnsupportedFeatureError until the
// cgo trampoline lands.
//
// 🟡 Tier 3.4 follow-up. The Go interface is stable so server
// code can be written against it now.
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
