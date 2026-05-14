// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package timeext

import (
	"errors"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func TestParseDate_HappyPath(t *testing.T) {
	cases := []struct {
		in        string
		want      backend.Date
		wantError bool
	}{
		{"20260506", backend.Date{Year: 2026, Month: 5, Day: 6}, false},
		{"20240229", backend.Date{Year: 2024, Month: 2, Day: 29}, false}, // leap year
		{"20000229", backend.Date{Year: 2000, Month: 2, Day: 29}, false}, // century leap year
		{"20231231", backend.Date{Year: 2023, Month: 12, Day: 31}, false},
		{"00010101", backend.Date{Year: 1, Month: 1, Day: 1}, false},
		{"99991231", backend.Date{Year: 9999, Month: 12, Day: 31}, false},
	}
	for _, tc := range cases {
		got, err := ParseDate(tc.in, ParseOptions{})
		if (err != nil) != tc.wantError {
			t.Errorf("ParseDate(%q): err=%v wantError=%v", tc.in, err, tc.wantError)
		}
		if got != tc.want {
			t.Errorf("ParseDate(%q)=%+v want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseDate_RejectsZero(t *testing.T) {
	_, err := ParseDate("00000000", ParseOptions{})
	if !errors.Is(err, ErrZeroDate) {
		t.Errorf("err=%v want ErrZeroDate", err)
	}
}

func TestParseDate_AllowZero(t *testing.T) {
	got, err := ParseDate("00000000", ParseOptions{AllowZero: true})
	if err != nil {
		t.Errorf("AllowZero: err=%v", err)
	}
	if !got.IsZero() {
		t.Errorf("got %+v want zero Date", got)
	}
}

func TestParseDate_RejectsInvalidValues(t *testing.T) {
	cases := []string{
		"20260000", // month 0
		"20261301", // month 13
		"20260132", // day 32
		"20260230", // Feb 30
		"20230229", // non-leap Feb 29
		"21000229", // century non-leap
		"20260431", // April 31
		"abcd1234", // non-digit
		"20260a01", // non-digit in month
	}
	for _, s := range cases {
		if _, err := ParseDate(s, ParseOptions{}); err == nil {
			t.Errorf("ParseDate(%q): nil error", s)
		}
	}
}

func TestParseDate_Strict(t *testing.T) {
	// "260506" without strict pads to "00260506" → valid.
	if _, err := ParseDate("260506", ParseOptions{}); err != nil {
		t.Errorf("non-strict short: err=%v", err)
	}
	// With strict, the same input must fail.
	if _, err := ParseDate("260506", ParseOptions{Strict: true}); err == nil {
		t.Error("strict short: nil error")
	}
}

func TestFormatDate(t *testing.T) {
	cases := []struct {
		in   backend.Date
		want string
	}{
		{backend.Date{Year: 2026, Month: 5, Day: 6}, "20260506"},
		{backend.Date{Year: 1, Month: 1, Day: 1}, "00010101"},
		{backend.Date{}, "00000000"},
	}
	for _, tc := range cases {
		if got := FormatDate(tc.in); got != tc.want {
			t.Errorf("FormatDate(%+v)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want backend.Time
		ok   bool
	}{
		{"123456", backend.Time{Hour: 12, Minute: 34, Second: 56}, true},
		{"235959", backend.Time{Hour: 23, Minute: 59, Second: 59}, true},
		{"000001", backend.Time{Hour: 0, Minute: 0, Second: 1}, true},
		{"240000", backend.Time{}, false}, // hour 24
		{"126059", backend.Time{}, false}, // min 60
		{"123460", backend.Time{}, false}, // sec 60
	}
	for _, tc := range cases {
		got, err := ParseTime(tc.in, ParseOptions{})
		if tc.ok && err != nil {
			t.Errorf("ParseTime(%q): err=%v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParseTime(%q): expected error", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParseTime(%q)=%+v want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseTime_Zero(t *testing.T) {
	_, err := ParseTime("000000", ParseOptions{})
	if !errors.Is(err, ErrZeroTime) {
		t.Errorf("err=%v want ErrZeroTime", err)
	}
	got, err := ParseTime("000000", ParseOptions{AllowZero: true})
	if err != nil || !got.IsZero() {
		t.Errorf("AllowZero: got=%+v err=%v", got, err)
	}
}

func TestFormatTime(t *testing.T) {
	cases := []struct {
		in   backend.Time
		want string
	}{
		{backend.Time{Hour: 12, Minute: 34, Second: 56}, "123456"},
		{backend.Time{Hour: 0, Minute: 0, Second: 0}, "000000"},
		{backend.Time{Hour: 23, Minute: 59, Second: 59}, "235959"},
	}
	for _, tc := range cases {
		if got := FormatTime(tc.in); got != tc.want {
			t.Errorf("FormatTime(%+v)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseUTCLong_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2026-05-06T12:34:56,1234567", time.Date(2026, 5, 6, 12, 34, 56, 123456700, time.UTC)},
		{"2026-05-06T12:34:56.1234567", time.Date(2026, 5, 6, 12, 34, 56, 123456700, time.UTC)},
		{"2026-05-06T00:00:00,0000000", time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got, err := ParseUTCLong(tc.in, ParseOptions{})
		if err != nil {
			t.Fatalf("ParseUTCLong(%q): %v", tc.in, err)
		}
		if !got.Time.Equal(tc.want) {
			t.Errorf("ParseUTCLong(%q)=%v want %v", tc.in, got.Time, tc.want)
		}
	}
}

func TestParseUTCLong_Zero(t *testing.T) {
	_, err := ParseUTCLong("0000-00-00T00:00:00,0000000", ParseOptions{})
	if !errors.Is(err, ErrZeroTime) {
		t.Errorf("err=%v want ErrZeroTime", err)
	}
}

func TestUTCLong_RoundTrip(t *testing.T) {
	t0 := time.Date(2026, 5, 6, 12, 34, 56, 100, time.UTC) // 100ns
	u := UTCLong{Time: t0}
	str := u.String()
	parsed, err := ParseUTCLong(str, ParseOptions{})
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !parsed.Time.Equal(t0) {
		t.Errorf("round-trip lost precision: in=%v out=%v str=%s", t0, parsed.Time, str)
	}
}

func TestIsLeapYear(t *testing.T) {
	cases := []struct {
		y    int
		leap bool
	}{
		{2000, true},  // divisible by 400
		{1900, false}, // divisible by 100 but not 400
		{2024, true},  // divisible by 4
		{2023, false}, // not divisible
		{2400, true},  // div by 400
	}
	for _, tc := range cases {
		if got := isLeapYear(tc.y); got != tc.leap {
			t.Errorf("isLeapYear(%d)=%v want %v", tc.y, got, tc.leap)
		}
	}
}

func FuzzParseFormatDate_RoundTrip(f *testing.F) {
	for _, s := range []string{"20260506", "20240229", "00010101", "99991231"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		d, err := ParseDate(in, ParseOptions{})
		if err != nil {
			return // not a valid DATS — fine.
		}
		// Re-format and re-parse; must be idempotent.
		formatted := FormatDate(d)
		d2, err := ParseDate(formatted, ParseOptions{})
		if err != nil {
			t.Fatalf("re-parse %q: %v", formatted, err)
		}
		if d != d2 {
			t.Errorf("round-trip changed value: %+v -> %q -> %+v", d, formatted, d2)
		}
	})
}
