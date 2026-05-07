// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package timeext implements ABAP date / time / UTCLONG
// parsing and formatting in pure Go.
//
// ABAP types covered:
//
//   - DATS (8 chars, "YYYYMMDD"). The ABAP initial value is
//     "00000000" — historically silently translated to a zero
//     time.Time by upstream gorfc, in violation of AGENTS.md
//     "no silent fallback". This package returns the explicit
//     [ErrZeroDate] sentinel instead; the public API exposes
//     [InvokeOptions.AllowZeroDate] for callers who want the
//     legacy behavior.
//   - TIMS (6 chars, "HHMMSS"). Mirrors DATS for the
//     "000000" initial-value case; sentinel is [ErrZeroTime].
//   - UTCLONG (21 chars, "YYYY-MM-DDTHH:MM:SS,FFFFFFF" — 7
//     fractional digits = 100ns precision). Available on
//     ABAP ≥ 7.50 + SDK ≥ 7.50; the SDK exposes either
//     `RfcSetUTCLong`/`RfcGetUTCLong` (🟡 verify exact symbol)
//     or the same-as-string path. We parse and format both
//     shapes so the marshaling layer can pick at call time.
//
// The package has no SDK dependency and no cgo. Tests cover the
// happy path, the strict toggle, the zero-value sentinel, and
// the leap-year / month-length edge cases that cause silent
// data corruption in lenient implementations.
package timeext

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// ErrZeroDate is returned by [ParseDate] when the input is
// "00000000". Callers can branch on it via [errors.Is]; the
// public nwrfc package re-exports the same sentinel value
// from `nwrfc.ErrZeroDate`.
var ErrZeroDate = errors.New("timeext: ABAP initial date 00000000")

// ErrZeroTime mirrors [ErrZeroDate] for TIMS.
var ErrZeroTime = errors.New("timeext: ABAP initial time 000000")

// ParseOptions controls strictness of the parsers.
type ParseOptions struct {
	// AllowZero suppresses [ErrZeroDate] / [ErrZeroTime] and
	// returns the zero [backend.Date] / [backend.Time] instead.
	AllowZero bool

	// Strict makes the parser reject any input that is not
	// exactly the ABAP-canonical shape: 8 digits for DATS,
	// 6 digits for TIMS. Without Strict, the parser accepts
	// shorter inputs by left-padding with '0' (matches the
	// upstream gorfc behavior of accepting "20060102" or
	// "20060102 " indistinguishably; useful when the SDK
	// returns a CHAR-padded value).
	Strict bool
}

// ParseDate decodes an ABAP DATS string. The input must be at
// most 8 characters; with [ParseOptions.Strict] set, must be
// exactly 8 digits.
//
// Returns ([backend.Date{}], [ErrZeroDate]) when the input is
// "00000000" and AllowZero is false. With AllowZero set, the
// zero Date is returned with nil error.
func ParseDate(s string, opts ParseOptions) (backend.Date, error) {
	if !opts.Strict && len(s) < 8 {
		// Left-pad with '0' to canonical length.
		s = leftPadZero(s, 8)
	}
	if len(s) != 8 {
		return backend.Date{}, fmt.Errorf("timeext: DATS length=%d want 8 (%q)", len(s), s)
	}
	if s == "00000000" {
		if opts.AllowZero {
			return backend.Date{}, nil
		}
		return backend.Date{}, ErrZeroDate
	}
	y, err := atoiN(s[0:4], "YYYY")
	if err != nil {
		return backend.Date{}, err
	}
	m, err := atoiN(s[4:6], "MM")
	if err != nil {
		return backend.Date{}, err
	}
	d, err := atoiN(s[6:8], "DD")
	if err != nil {
		return backend.Date{}, err
	}
	if m < 1 || m > 12 {
		return backend.Date{}, fmt.Errorf("timeext: DATS %q month out of range: %d", s, m)
	}
	if d < 1 || d > daysInMonth(y, m) {
		return backend.Date{}, fmt.Errorf("timeext: DATS %q day out of range: %d (month %d, year %d has %d days)",
			s, d, m, y, daysInMonth(y, m))
	}
	return backend.Date{Year: uint16(y), Month: uint16(m), Day: uint16(d)}, nil
}

// FormatDate emits the canonical 8-character DATS string for d.
// Returns "00000000" for the zero Date.
func FormatDate(d backend.Date) string {
	return fmt.Sprintf("%04d%02d%02d", d.Year, d.Month, d.Day)
}

// ParseTime decodes an ABAP TIMS string. Mirrors [ParseDate].
func ParseTime(s string, opts ParseOptions) (backend.Time, error) {
	if !opts.Strict && len(s) < 6 {
		s = leftPadZero(s, 6)
	}
	if len(s) != 6 {
		return backend.Time{}, fmt.Errorf("timeext: TIMS length=%d want 6 (%q)", len(s), s)
	}
	if s == "000000" {
		if opts.AllowZero {
			return backend.Time{}, nil
		}
		return backend.Time{}, ErrZeroTime
	}
	h, err := atoiN(s[0:2], "HH")
	if err != nil {
		return backend.Time{}, err
	}
	m, err := atoiN(s[2:4], "MM")
	if err != nil {
		return backend.Time{}, err
	}
	sec, err := atoiN(s[4:6], "SS")
	if err != nil {
		return backend.Time{}, err
	}
	if h > 23 {
		return backend.Time{}, fmt.Errorf("timeext: TIMS %q hour out of range: %d", s, h)
	}
	if m > 59 {
		return backend.Time{}, fmt.Errorf("timeext: TIMS %q minute out of range: %d", s, m)
	}
	if sec > 59 {
		return backend.Time{}, fmt.Errorf("timeext: TIMS %q second out of range: %d", s, sec)
	}
	return backend.Time{Hour: uint8(h), Minute: uint8(m), Second: uint8(sec)}, nil
}

// FormatTime emits the canonical 6-character TIMS string for t.
// Returns "000000" for the zero Time.
func FormatTime(t backend.Time) string {
	return fmt.Sprintf("%02d%02d%02d", t.Hour, t.Minute, t.Second)
}

// UTCLong is the Go representation of ABAP UTCLONG. The SDK
// stores it as a 64-bit signed integer counting 100ns ticks
// from 0001-01-01 00:00:00 UTC; we keep the same epoch and
// resolution to round-trip exactly.
//
// 🟡 verify against SAP NetWeaver RFC SDK Programming Guide:
// the integer epoch is 0001-01-01 in some SDK versions and
// 0000-12-30 in others. The canonical form ("YYYY-MM-DDTHH:
// MM:SS,FFFFFFF") is the SDK string output, unambiguous.
type UTCLong struct {
	// Time is the underlying time.Time, truncated to 100ns.
	Time time.Time
}

// String returns the canonical 27-character UTCLONG string
// "YYYY-MM-DDTHH:MM:SS,FFFFFFF". Comma decimal separator
// matches the SAP literal layout.
func (u UTCLong) String() string {
	if u.Time.IsZero() {
		return "0000-00-00T00:00:00,0000000"
	}
	t := u.Time.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d,%07d",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(),
		t.Nanosecond()/100,
	)
}

// IsZero reports whether u is the SDK initial-value UTCLONG.
func (u UTCLong) IsZero() bool { return u.Time.IsZero() }

// ParseUTCLong decodes the canonical SDK string layout
// "YYYY-MM-DDTHH:MM:SS,FFFFFFF" (or with '.' as decimal
// separator). Trailing space is tolerated unless Strict.
//
// Returns the zero UTCLong with no error when the input is the
// SDK initial-value (zero year and date).
func ParseUTCLong(s string, opts ParseOptions) (UTCLong, error) {
	// Tolerant trim of trailing whitespace / nulls (CHAR-style
	// padding from the SDK side).
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == 0x00) {
		s = s[:len(s)-1]
	}
	if opts.Strict && len(s) != 27 {
		return UTCLong{}, fmt.Errorf("timeext: UTCLONG length=%d want 27 (%q)", len(s), s)
	}
	if len(s) < 19 {
		return UTCLong{}, fmt.Errorf("timeext: UTCLONG too short: %q", s)
	}
	// Replace ',' with '.' to use Go's normal fractional parse.
	for i, c := range s {
		if c == ',' {
			s = s[:i] + "." + s[i+1:]
			break
		}
	}

	// SDK initial-value layout (all zeros except separators).
	if s == "0000-00-00T00:00:00.0000000" {
		if opts.AllowZero {
			return UTCLong{}, nil
		}
		return UTCLong{}, ErrZeroTime
	}

	// time.Parse with a fractional second layout. The Go
	// reference layout uses 7 nines for 100ns precision.
	const layout = "2006-01-02T15:04:05.9999999"
	t, err := time.Parse(layout, s)
	if err != nil {
		return UTCLong{}, fmt.Errorf("timeext: UTCLONG parse %q: %w", s, err)
	}
	return UTCLong{Time: t}, nil
}

// daysInMonth returns the day count for the given month/year
// pair, honoring leap years. month must be 1..12.
func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if isLeapYear(year) {
			return 29
		}
		return 28
	}
	return 0
}

func isLeapYear(y int) bool {
	if y%400 == 0 {
		return true
	}
	if y%100 == 0 {
		return false
	}
	return y%4 == 0
}

func atoiN(s, name string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("timeext: invalid %s in %q: %w", name, s, err)
	}
	return n, nil
}

func leftPadZero(s string, n int) string {
	if len(s) >= n {
		return s
	}
	out := make([]byte, n)
	pad := n - len(s)
	for i := 0; i < pad; i++ {
		out[i] = '0'
	}
	copy(out[pad:], s)
	return string(out)
}
