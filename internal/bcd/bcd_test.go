// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package bcd

import (
	"errors"
	"math/big"
	"testing"
)

func TestParse_HappyPath(t *testing.T) {
	cases := []struct {
		in               string
		precision, scale int
		want             string
	}{
		{"123.456", 6, 3, "123.456"},
		{"+123.456", 6, 3, "123.456"},
		{"-123.456", 6, 3, "-123.456"},
		{" 1.5 ", 2, 1, "1.5"},
		{"1,5", 2, 1, "1.5"},
		{"0", 1, 0, "0"},
		{"0.0", 1, 1, "0"},
		{"0.5", 1, 1, "0.5"},
		{"1.2300", 5, 4, "1.23"}, // strips trailing zeros
		{"007", 3, 0, "7"},       // strips leading zeros
		{".5", 1, 1, "0.5"},
		{"5.", 1, 0, "5"},
		{"-0", 1, 0, "0"},     // leading minus on zero canonicalizes
		{"-0.000", 1, 3, "0"}, // signed zero in any form → "0"
	}
	for _, tc := range cases {
		got, err := Parse(tc.in, tc.precision, tc.scale)
		if err != nil {
			t.Errorf("Parse(%q, %d, %d): %v", tc.in, tc.precision, tc.scale, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q, %d, %d)=%q want %q", tc.in, tc.precision, tc.scale, got, tc.want)
		}
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct{ in string }{
		{""},
		{"+"},
		{"-"},
		{"abc"},
		{"1.2.3"},
		{"1e5"}, // exponent not yet supported (ABAP wire never has it)
		{"++1"},
		{"-+1"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.in, 5, 2)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q): err=%v want ErrInvalid", tc.in, err)
		}
	}
}

func TestParse_PrecisionScaleEnforcement(t *testing.T) {
	// scale 2 rejects 3 fractional digits.
	if _, err := Parse("1.234", 5, 2); !errors.Is(err, ErrInvalid) {
		t.Errorf("scale-overflow: got %v want ErrInvalid", err)
	}
	// precision 3 rejects "1234".
	if _, err := Parse("1234", 3, 0); !errors.Is(err, ErrInvalid) {
		t.Errorf("precision-overflow: got %v want ErrInvalid", err)
	}
	// precision 0 disables the check.
	if _, err := Parse("12345678901234567890", 0, 0); err != nil {
		t.Errorf("precision-disabled: %v", err)
	}
}

func TestFormat_BigRat(t *testing.T) {
	r := new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(8)) // 0.125
	if got := Format(r, 3); got != "0.125" {
		t.Errorf("Format(1/8, 3)=%q want %q", got, "0.125")
	}
	// Rounded to 2 fractional digits → bankers rounding 0.13.
	if got := Format(r, 2); got != "0.13" {
		t.Errorf("Format(1/8, 2)=%q want %q", got, "0.13")
	}
	if got := Format(nil, 2); got != "" {
		t.Errorf("Format(nil)=%q want empty", got)
	}
}

func TestNegate(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1", "-1"},
		{"-1", "1"},
		{"+1", "-1"},
		{"0", "0"},
		{"0.0", "0.0"},
		{"", ""},
		{"123.456", "-123.456"},
		{"-123.456", "123.456"},
	}
	for _, tc := range cases {
		if got := Negate(tc.in); got != tc.want {
			t.Errorf("Negate(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsZero(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0", true},
		{"-0", true},
		{"+0", true},
		{"0.0", true},
		{"00.00", true},
		{"  0  ", true},
		{"1", false},
		{"-1", false},
		{"0.1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsZero(tc.in); got != tc.want {
			t.Errorf("IsZero(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// FuzzParse_Roundtrip ensures parse(parse(x)) is stable.
func FuzzParse_Roundtrip(f *testing.F) {
	for _, s := range []string{
		"0", "1", "-1", "1.5", "123.456", "0.001", "999.999",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		first, err := Parse(in, 0, 10)
		if err != nil {
			return
		}
		second, err := Parse(first, 0, 10)
		if err != nil {
			t.Fatalf("re-parse %q: %v", first, err)
		}
		if first != second {
			t.Errorf("not idempotent: %q -> %q -> %q", in, first, second)
		}
	})
}
