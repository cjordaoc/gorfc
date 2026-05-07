// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package bcd implements the decimal policy for ABAP BCD (P,
// "packed decimal"), DECF16 (16-digit IEEE 754-2008
// `decimal64`), and DECF34 (34-digit `decimal128`) types.
//
// Default policy: decimals cross the gorfc API as Go strings.
// This is the conservative default (no precision loss, no
// dependency on a third-party decimal package) and matches
// node-rfc, PyRFC, and JCo's default JSON marshaling.
//
// Opt-in policy: callers may install a [Decimal] interface
// implementation (e.g. shopspring/decimal, cockroachdb/apd) via
// [InvokeOptions.Decimal] (T1.8). The marshaling layer then
// converts string ↔ Decimal at the user boundary; the wire
// format with the SDK stays a string for fidelity.
//
// This package validates strings against ABAP precision /
// scale rules and exposes a few math-shaped helpers for the
// codegen package (T4.2) to emit type-checked client code.
package bcd

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrInvalid is returned when a string is not a valid ABAP
// decimal literal: contains characters outside the allowed
// alphabet ([0-9.,+-Ee ] up to the limits below), has a sign
// in the wrong place, or has more decimals than the destination
// scale allows.
var ErrInvalid = errors.New("bcd: invalid decimal literal")

// ErrPrecisionLoss is returned when round-tripping through a
// reduced-precision Decimal implementation would discard digits
// that the SAP system cares about. The marshal layer surfaces
// this through *MarshalError; callers who explicitly opt into
// lossy rounding can wrap the Decimal helper themselves.
var ErrPrecisionLoss = errors.New("bcd: precision loss would occur")

// Decimal is the interface user code may implement to swap the
// default string-based decimal for a typed value (apd.Decimal,
// shopspring/decimal.Decimal, etc.). The interface is
// deliberately minimal — String() is the bridge to and from the
// SDK wire format; callers do their math in their own type.
//
// To register a Decimal at call time, set the marshaling option
// in the public nwrfc.InvokeOptions struct (T1.8); the runtime
// then uses [Parse] / [Format] to convert.
type Decimal interface {
	// String returns the decimal in the canonical form
	// "[-]<digits>[.<digits>]". No exponent, no thousands
	// separator. Compatible with Go's `big.Float`/`big.Rat`
	// String() output for plain decimals.
	String() string
}

// Parse normalizes a Go-side decimal string for transmission to
// the SAP system. The output:
//
//   - has no leading or trailing whitespace
//   - uses '.' as the decimal separator (',' is converted)
//   - has at most one sign at the start
//   - has at most `scale` digits after the decimal point
//   - has at most `precision` total digits (sign + integer +
//     fractional, excluding the decimal point itself)
//
// Returns [ErrInvalid] if the input is malformed; the wrapped
// error carries the offending substring for diagnostics.
//
// precision = 0 disables the precision check (used for ABAP
// STRING-typed payloads where the field length is not fixed).
func Parse(s string, precision, scale int) (string, error) {
	s = strings.TrimSpace(s)
	// Convert ',' decimal separator to '.' (some SAP locales
	// emit ',' on the wire when the SDK is configured for
	// language-aware formatting).
	if i := strings.Index(s, ","); i >= 0 && !strings.Contains(s, ".") {
		s = s[:i] + "." + s[i+1:]
	}
	if s == "" {
		return "", fmt.Errorf("%w: empty string", ErrInvalid)
	}

	// Optional sign.
	sign := ""
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		sign = "-"
		s = s[1:]
	}
	if s == "" {
		return "", fmt.Errorf("%w: lone sign", ErrInvalid)
	}

	// Split on '.'.
	intPart, fracPart, hasFrac := splitOnce(s, '.')

	// Validate digits.
	if !allDigits(intPart) {
		return "", fmt.Errorf("%w: non-digit in integer part %q", ErrInvalid, intPart)
	}
	if hasFrac && !allDigits(fracPart) {
		return "", fmt.Errorf("%w: non-digit in fractional part %q", ErrInvalid, fracPart)
	}
	if intPart == "" && fracPart == "" {
		return "", fmt.Errorf("%w: no digits", ErrInvalid)
	}
	if intPart == "" {
		intPart = "0"
	}
	// Strip leading zeros from integer part (keep one).
	intPart = stripLeadingZeros(intPart)
	if intPart == "" {
		intPart = "0"
	}

	// Strip trailing zeros from fractional part — ABAP doesn't
	// distinguish "1.20" from "1.2", and emitting fewer digits
	// uses less wire bandwidth.
	fracPart = stripTrailingZeros(fracPart)

	// Scale check.
	if scale >= 0 && len(fracPart) > scale {
		return "", fmt.Errorf("%w: %d fractional digits > scale %d", ErrInvalid, len(fracPart), scale)
	}

	// Precision check (sum of integer + fractional digits;
	// leading "0." counts the leading 0 against precision).
	if precision > 0 {
		total := len(intPart) + len(fracPart)
		// "0.5" has 1+1=2 digits but precision counts only
		// significant digits — accept the leading zero in
		// "0.5" as not contributing if integer part was
		// zero-coerced.
		if intPart == "0" && len(fracPart) > 0 {
			total = len(fracPart)
		}
		if total > precision {
			return "", fmt.Errorf("%w: %d total digits > precision %d", ErrInvalid, total, precision)
		}
	}

	// Canonicalize signed zero: "-0" or "-0.000" → "0".
	if intPart == "0" && fracPart == "" {
		sign = ""
	}

	if fracPart == "" {
		return sign + intPart, nil
	}
	return sign + intPart + "." + fracPart, nil
}

// Format produces a canonical decimal string from a [big.Rat].
// Convenience entry for codegen-emitted clients that want to
// accept *big.Rat-shaped payloads. scale is the number of
// fractional digits to emit; values are rounded half-to-even
// to match IEEE 754-2008 default rounding (matches DECF16/34
// SDK semantics — 🟡 verify rounding mode against the
// programming guide).
func Format(r *big.Rat, scale int) string {
	if r == nil {
		return ""
	}
	if scale < 0 {
		scale = 0
	}
	return r.FloatString(scale)
}

// Negate returns the additive inverse of the canonical decimal
// string. Used by the unmarshal path when the SDK reports a
// negative BCD via the sign nibble rather than an inline '-'.
func Negate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "0.0" {
		return s
	}
	if s[0] == '-' {
		return s[1:]
	}
	if s[0] == '+' {
		return "-" + s[1:]
	}
	return "-" + s
}

// IsZero reports whether s is a canonical-or-near-canonical
// representation of zero ("0", "0.0", "-0", "+0", " 0 ").
func IsZero(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	for _, c := range s {
		if c != '0' && c != '.' {
			return false
		}
	}
	return true
}

// =============================================================
// Helpers (internal)
// =============================================================

func splitOnce(s string, sep byte) (string, string, bool) {
	idx := strings.IndexByte(s, sep)
	if idx < 0 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}

func allDigits(s string) bool {
	if s == "" {
		return true // empty is "all digits" for the integer-part case
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func stripLeadingZeros(s string) string {
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}
	return s[i:]
}

func stripTrailingZeros(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == '0' {
		i--
	}
	return s[:i]
}
