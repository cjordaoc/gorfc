// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// Behavior tests for the tRFC/qRFC/bgRFC client wiring. These
// stay below the SAP-system gate: they exercise input validation,
// interface satisfaction, and the error path that runs when no
// SAP system is reachable. The full Submit/Confirm round-trip
// against SM58 / SBGRFCMON is the live-system gate (Categoria B
// per CHANGES).

package nwrfc_test

import (
	"context"
	"strings"
	"testing"
	"unicode"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestSDKBackend_SatisfiesTransactionalInterface confirms the
// active backend satisfies the backend.TransactionalBackend
// extension so the public API does not silently fall through to
// *UnsupportedFeatureError. With the cgo+SDK backend wired in,
// this assertion succeeds; with the no-SDK stub or a pure-Go
// mock it would fail (and those builds use a different test
// file).
func TestSDKBackend_SatisfiesTransactionalInterface(t *testing.T) {
	b := backend.Default()
	if _, ok := b.(backend.TransactionalBackend); !ok {
		t.Fatalf("active backend %q does not satisfy backend.TransactionalBackend; tRFC/qRFC client wiring is missing", b.Name())
	}
}

// TestSDKBackend_SatisfiesBgRFCInterface same for bgRFC.
func TestSDKBackend_SatisfiesBgRFCInterface(t *testing.T) {
	b := backend.Default()
	if _, ok := b.(backend.BgRFCBackend); !ok {
		t.Fatalf("active backend %q does not satisfy backend.BgRFCBackend; bgRFC client wiring is missing", b.Name())
	}
}

// TestNewTID_ShapeContract: the generator returns 24 uppercase
// hex chars per ABAP's RFC_TID convention. SAP rejects anything
// else with RFC_INVALID_PARAMETER on RfcCreateTransaction.
func TestNewTID_ShapeContract(t *testing.T) {
	for i := 0; i < 100; i++ {
		tid := string(nwrfc.NewTID())
		if len(tid) != 24 {
			t.Fatalf("NewTID len=%d want 24 (got %q)", len(tid), tid)
		}
		for _, r := range tid {
			if !isUpperHex(r) {
				t.Fatalf("NewTID contains non-uppercase-hex %q in %q", r, tid)
			}
		}
	}
}

// TestNewUnitID_ShapeContract: 32 uppercase hex chars per
// RFC_UNITID_LN convention.
func TestNewUnitID_ShapeContract(t *testing.T) {
	for i := 0; i < 100; i++ {
		uid := string(nwrfc.NewUnitID())
		if len(uid) != 32 {
			t.Fatalf("NewUnitID len=%d want 32 (got %q)", len(uid), uid)
		}
		for _, r := range uid {
			if !isUpperHex(r) {
				t.Fatalf("NewUnitID contains non-uppercase-hex %q in %q", r, uid)
			}
		}
	}
}

// TestNewTID_Uniqueness: 1 000 generations must be all distinct.
// 12-byte random with crypto/rand has ~10^-26 collision risk per
// pair, so any collision in this loop is a real bug.
func TestNewTID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		tid := string(nwrfc.NewTID())
		if seen[tid] {
			t.Fatalf("NewTID returned duplicate %q at iteration %d", tid, i)
		}
		seen[tid] = true
	}
}

// TestUnitState_EnumValuesMatchBackend: the public-API constants
// must match the backend-package values exactly so no mapping
// loses a state when the SDK reports it.
func TestUnitState_EnumValuesMatchBackend(t *testing.T) {
	cases := map[nwrfc.UnitState]backend.UnitState{
		nwrfc.UnitStateUnknown:    backend.UnitStateUnknown,
		nwrfc.UnitStateNotFound:   backend.UnitStateNotFound,
		nwrfc.UnitStateInProcess:  backend.UnitStateInProcess,
		nwrfc.UnitStateCommitted:  backend.UnitStateCommitted,
		nwrfc.UnitStateRolledBack: backend.UnitStateRolledBack,
		nwrfc.UnitStateConfirmed:  backend.UnitStateConfirmed,
		nwrfc.UnitStateCreated:    backend.UnitStateCreated,
		nwrfc.UnitStateExecuted:   backend.UnitStateExecuted,
	}
	for pub, internal := range cases {
		if uint8(pub) != uint8(internal) {
			t.Errorf("UnitState %v=%d backend=%d", pub, pub, internal)
		}
	}
}

// TestNewQueuedTransaction_FailsClosed: with no SAP system
// reachable, NewQueuedTransaction on a nil/closed Conn fails
// fast with a broken-connection error rather than panicking or
// silently returning a valid Transaction. We can't go further
// without an open connection (the queueName validation lives
// AFTER the open-check, so reaching it requires Categoria B).
func TestNewQueuedTransaction_FailsClosed(t *testing.T) {
	var c *nwrfc.Conn
	_, err := c.NewQueuedTransaction(context.Background(), "")
	if err == nil {
		t.Fatal("NewQueuedTransaction(nil-Conn) returned nil error")
	}
	// Either the broken-connection check (current path with
	// closed Conn) or the queueName *ConfigError (only
	// reachable with an open Conn) is acceptable. The point of
	// this test is "no panic, error surfaced".
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "connection") && !strings.Contains(msg, "queuename") {
		t.Errorf("err=%v did not mention connection or queueName", err)
	}
}

func isUpperHex(r rune) bool {
	if unicode.IsDigit(r) {
		return true
	}
	if r >= 'A' && r <= 'F' {
		return true
	}
	return false
}
