// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Session is an explicit ABAP stateful context (LUW) on top of
// a [Conn]. Calls inside the same Session see the same SAP
// session state; the SAP system treats the calls as one logical
// unit of work until [Session.Commit] or [Session.Rollback].
//
// Outside a Session, every Call resets the ABAP context
// implicitly (the SDK does this on each invocation), so two
// calls cannot share state. Inside a Session, the SDK is
// instructed to keep the context alive between calls.
//
// Concurrency: a Session pins a Conn; other goroutines must
// not Call on the same Conn for the duration of the Session.
// Because Session itself uses the Conn's mutex for each Call,
// two goroutines sharing a Session is safe but pointless — the
// calls serialize.
type Session struct {
	conn   *Conn
	closed bool
}

// NewSession opens a stateful session on c. The returned
// Session is bound to c for its lifetime; closing the Conn
// invalidates the Session.
func NewSession(ctx context.Context, c *Conn) (*Session, error) {
	if c == nil || !c.Alive() {
		return nil, &BrokenConnectionError{Reason: "nil or closed Conn", Cause: ErrConnClosed}
	}
	// 🟡 The SDK does not have an "open session" call; the
	// session is implicit in keeping the same Conn alive
	// between two RFC calls without an interleaving
	// RfcResetServerContext. Here we only mark the Session
	// open; Commit / Rollback issue the BAPI calls that close
	// the LUW.
	return &Session{conn: c}, nil
}

// Call is a stateful Call: the ABAP context is preserved between
// calls within this Session.
func (s *Session) Call(ctx context.Context, fn string, in, out any, opts ...CallOptions) (backend.CallParams, error) {
	if s == nil || s.closed {
		return nil, &BrokenConnectionError{Reason: "session closed", Cause: ErrConnClosed}
	}
	return Call(ctx, s.conn, fn, in, out, opts...)
}

// Commit closes the session by invoking BAPI_TRANSACTION_COMMIT
// on the SAP system. If WithWait is true, the BAPI is called
// with WAIT='X' so the COMMIT WORK AND WAIT semantic applies.
func (s *Session) Commit(ctx context.Context, withWait bool) error {
	if s == nil || s.closed {
		return nil
	}
	in := backend.CallParams{}
	if withWait {
		in["WAIT"] = "X"
	}
	resp, err := s.conn.backend.Invoke(ctx, s.conn.handle, "BAPI_TRANSACTION_COMMIT", in, backend.InvokeOptions{})
	if err != nil {
		return err
	}
	// Reset SAP-side context so the next call on this Conn
	// starts clean.
	if rerr := s.conn.backend.Reset(s.conn.handle); rerr != nil {
		err = errors.Join(err, rerr)
	}
	s.closed = true
	if returnErr := bapiReturnAsError(resp); returnErr != nil {
		return returnErr
	}
	return err
}

// Rollback closes the session by invoking
// BAPI_TRANSACTION_ROLLBACK and resetting the server context.
func (s *Session) Rollback(ctx context.Context) error {
	if s == nil || s.closed {
		return nil
	}
	resp, err := s.conn.backend.Invoke(ctx, s.conn.handle, "BAPI_TRANSACTION_ROLLBACK", backend.CallParams{}, backend.InvokeOptions{})
	if err != nil {
		_ = s.conn.backend.Reset(s.conn.handle)
		s.closed = true
		return err
	}
	if rerr := s.conn.backend.Reset(s.conn.handle); rerr != nil {
		err = errors.Join(err, rerr)
	}
	s.closed = true
	if returnErr := bapiReturnAsError(resp); returnErr != nil {
		return returnErr
	}
	return err
}

// bapiReturnAsError inspects a RETURN structure for
// type='A' (abort) / type='E' (error) and synthesizes an
// *ABAPApplicationError if found. The full BAPIRet2 helper
// (T2.1) handles list-of-returns, message variables, and
// classification flags; this minimal version is enough for
// commit/rollback.
func bapiReturnAsError(resp backend.CallParams) error {
	r, ok := resp["RETURN"]
	if !ok {
		return nil
	}
	m, ok := r.(map[string]any)
	if !ok {
		return nil
	}
	typ, _ := m["TYPE"].(string)
	if typ != "A" && typ != "E" {
		return nil
	}
	msg, _ := m["MESSAGE"].(string)
	cls, _ := m["ID"].(string)
	num, _ := m["NUMBER"].(string)
	return &ABAPApplicationError{
		SDKErrorInfo: SDKErrorInfo{
			Key:           "BAPI_RETURN",
			Message:       msg,
			AbapMsgClass:  cls,
			AbapMsgType:   typ,
			AbapMsgNumber: num,
		},
	}
}
