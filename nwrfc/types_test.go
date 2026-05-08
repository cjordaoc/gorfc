// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"testing"

	"github.com/cjordaoc/gorfc/internal/bcd"
	"github.com/cjordaoc/gorfc/internal/timeext"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestTypeAliases_DateMethodsReachable: methods declared on the
// underlying internal type are reachable through the public
// alias without forwarding boilerplate. If `nwrfc.Date` were a
// wrapper struct rather than a type alias, this would not
// compile.
func TestTypeAliases_DateMethodsReachable(t *testing.T) {
	var d nwrfc.Date
	if !d.IsZero() {
		t.Error("zero Date.IsZero() == false")
	}
	d = nwrfc.Date{Year: 2026, Month: 5, Day: 8}
	if d.IsZero() {
		t.Error("non-zero Date reported IsZero()")
	}
}

// TestTypeAliases_TimeMethodsReachable mirrors the Date test for
// the Time alias.
func TestTypeAliases_TimeMethodsReachable(t *testing.T) {
	var z nwrfc.Time
	if !z.IsZero() {
		t.Error("zero Time.IsZero() == false")
	}
	z = nwrfc.Time{Hour: 12, Minute: 0, Second: 0}
	if z.IsZero() {
		t.Error("non-zero Time reported IsZero()")
	}
}

// TestTypeAliases_AsTimeComposes: the AsTime convenience
// composes Date+Time into a single time.Time without dragging
// internal/backend into caller imports.
func TestTypeAliases_AsTimeComposes(t *testing.T) {
	d := nwrfc.Date{Year: 2026, Month: 5, Day: 8}
	z := nwrfc.Time{Hour: 9, Minute: 30, Second: 0}
	got := nwrfc.AsTime(d, z)
	if got.Year() != 2026 || got.Month() != 5 || got.Day() != 8 ||
		got.Hour() != 9 || got.Minute() != 30 {
		t.Errorf("AsTime composed wrong: %v", got)
	}
}

// TestTypeAliases_DecimalContract: Decimal is the public alias
// for the bcd.Decimal interface; user code can satisfy it via
// String() and use it as marshal-time bridge type. Smoke test:
// a tiny inline decimal implements the alias.
func TestTypeAliases_DecimalContract(t *testing.T) {
	var d nwrfc.Decimal = stringDecimal("123.45")
	if d.String() != "123.45" {
		t.Errorf("Decimal.String=%q; want 123.45", d.String())
	}
	// The alias and the internal type are the same — passing a
	// nwrfc.Decimal where bcd.Decimal is expected must not need
	// a conversion.
	var bd bcd.Decimal = d
	if bd.String() != "123.45" {
		t.Errorf("alias-to-internal type mismatch: %q", bd.String())
	}
}

type stringDecimal string

func (s stringDecimal) String() string { return string(s) }

// TestTypeAliases_UTCLongMethodsReachable: the UTCLong alias
// keeps the timeext UTCLong methods reachable.
func TestTypeAliases_UTCLongMethodsReachable(t *testing.T) {
	var u nwrfc.UTCLong
	if !u.IsZero() {
		t.Error("zero UTCLong reported non-zero")
	}
	// Compare against the internal timeext type to ensure the
	// alias is identity, not a copy.
	var v timeext.UTCLong = u
	if !v.IsZero() {
		t.Error("alias-to-internal type identity broke")
	}
}
