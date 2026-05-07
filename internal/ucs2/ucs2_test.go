// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package ucs2

import (
	"bytes"
	"errors"
	"testing"
	"unicode/utf16"
)

func TestRoundTrip_ASCII(t *testing.T) {
	for _, s := range []string{"", "A", "hello", "STFC_CONNECTION", "REQUTEXT"} {
		b, err := Encode(s)
		if err != nil {
			t.Fatalf("Encode(%q): %v", s, err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got != s {
			t.Errorf("round-trip: got %q want %q", got, s)
		}
	}
}

func TestRoundTrip_Unicode(t *testing.T) {
	cases := []string{
		"HELLÖ SÄP", // Latin-1 supplement
		"こんにちは",     // Hiragana (BMP)
		"αβγδε",     // Greek (BMP)
		"𝕏",         // Mathematical X (supplementary plane, surrogate pair)
		"🎉 emoji 🎊", // emoji (supplementary plane)
		"ABCßD",     // Latin sharp s
		"ä = ä",    // combining diaeresis
	}
	for _, s := range cases {
		b, err := Encode(s)
		if err != nil {
			t.Fatalf("Encode(%q): %v", s, err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("Decode(%q -> bytes): %v", s, err)
		}
		if got != s {
			t.Errorf("round-trip %q: got %q (len=%d) want %q (len=%d)", s, got, len(got), s, len(s))
		}
	}
}

func TestEncode_RejectsInvalidUTF8(t *testing.T) {
	bad := string([]byte{0xff, 0xfe, 0xfd}) // invalid UTF-8 bytes
	if _, err := Encode(bad); !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("Encode(invalid): err=%v, want errors.Is ErrInvalidUTF8", err)
	}
}

func TestDecode_OddByteLength(t *testing.T) {
	odd := []byte{0x41, 0x00, 0x42}
	if _, err := Decode(odd); err == nil {
		t.Error("Decode(odd-length) returned nil error")
	}
}

func TestDecode_StripsTrailingNullPadding(t *testing.T) {
	// "AB" followed by 6 null code units (12 zero bytes).
	codeUnits := []uint16{'A', 'B', 0, 0, 0, 0, 0, 0}
	b := unitsToBytes(codeUnits)
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "AB" {
		t.Errorf("got %q want %q", got, "AB")
	}
}

func TestDecode_AllZerosIsEmpty(t *testing.T) {
	b := make([]byte, 16)
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode(zero): %v", err)
	}
	if got != "" {
		t.Errorf("Decode(all-zero) got %q want empty", got)
	}
}

func TestDecode_RejectsUnpairedSurrogate(t *testing.T) {
	cases := [][]uint16{
		{0xD800},           // unpaired high
		{0xDC00},           // unpaired low
		{'A', 0xD800},      // unpaired high at end
		{'A', 0xD800, 'B'}, // unpaired high mid-string
		{'A', 0xDFFF, 'B'}, // unpaired low mid-string
		{0xD800, 0x0041},   // high followed by non-low
	}
	for _, units := range cases {
		_, err := DecodeUnits(units)
		if !errors.Is(err, ErrInvalidUTF16) {
			t.Errorf("DecodeUnits(%v): err=%v, want errors.Is ErrInvalidUTF16", units, err)
		}
	}
}

func TestDecode_AcceptsValidSurrogatePair(t *testing.T) {
	// U+1F389 (🎉) = 0xD83C 0xDF89
	got, err := DecodeUnits([]uint16{0xD83C, 0xDF89})
	if err != nil {
		t.Fatalf("DecodeUnits: %v", err)
	}
	if got != "🎉" {
		t.Errorf("got %q want 🎉", got)
	}
}

func TestEncodeNullTerminated_HasTrailingZeros(t *testing.T) {
	b, err := EncodeNullTerminated("X")
	if err != nil {
		t.Fatalf("EncodeNullTerminated: %v", err)
	}
	// "X" = 1 code unit + 1 null code unit = 4 bytes.
	if len(b) != 4 {
		t.Fatalf("len=%d want 4", len(b))
	}
	if !bytes.Equal(b[2:], []byte{0, 0}) {
		t.Errorf("trailing bytes = %v want [0 0]", b[2:])
	}
}

func TestRStrip(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"A", "A"},
		{"A   ", "A"},
		{"A\x00\x00", "A"},
		{"A \x00 ", "A"},
		{"   ", ""},
		{"\x00", ""},
	}
	for _, tc := range cases {
		if got := RStrip(tc.in); got != tc.want {
			t.Errorf("RStrip(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCountCodeUnits(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"A", 1},
		{"hello", 5},
		{"é", 1},   // BMP
		{"𝕏", 2},   // supplementary plane → surrogate pair
		{"🎉🎊", 4},  // two emoji = 4 code units
		{"a🎉b", 4}, // mixed
	}
	for _, tc := range cases {
		got := CountCodeUnits(tc.in)
		if got != tc.want {
			t.Errorf("CountCodeUnits(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

// FuzzRoundTrip ensures any valid UTF-8 input survives an
// Encode → Decode cycle byte-for-byte.
func FuzzRoundTrip(f *testing.F) {
	for _, s := range []string{
		"",
		"A",
		"hello",
		"HELLÖ SÄP",
		"🎉",
		"日本語",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Skip invalid UTF-8 — Encode rejects it explicitly.
		b, err := Encode(s)
		if errors.Is(err, ErrInvalidUTF8) {
			return
		}
		if err != nil {
			t.Fatalf("Encode(%q): %v", s, err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		// Decode strips trailing nulls; only assert equality
		// when the original had no trailing nulls.
		want := s
		// rfind first non-null
		// Actually just compare; trailing-null-strip only
		// matters when the input itself ends in U+0000, which
		// fuzz inputs rarely do. If so, accept the strip.
		if got != want {
			// Allow trailing-null strip.
			rstripped := want
			for len(rstripped) > 0 && rstripped[len(rstripped)-1] == 0x00 {
				rstripped = rstripped[:len(rstripped)-1]
			}
			if got != rstripped {
				t.Errorf("round-trip mismatch: got %q want %q (or %q after null-strip)",
					got, want, rstripped)
			}
		}
	})
}

// helper: pack []uint16 into LE bytes.
func unitsToBytes(units []uint16) []byte {
	out := make([]byte, len(units)*2)
	for i, u := range units {
		out[i*2] = byte(u)
		out[i*2+1] = byte(u >> 8)
	}
	return out
}

// Belt-and-suspenders: confirm we agree with stdlib utf16
// for a known supplementary-plane round-trip.
func TestStdlibAgreement(t *testing.T) {
	const s = "🎉ABC日本"
	stdUnits := utf16.Encode([]rune(s))
	stdRunes := utf16.Decode(stdUnits)
	if string(stdRunes) != s {
		t.Fatalf("stdlib round-trip broken: got %q want %q", string(stdRunes), s)
	}

	got, err := DecodeUnits(stdUnits)
	if err != nil {
		t.Fatalf("DecodeUnits(stdlib output): %v", err)
	}
	if got != s {
		t.Errorf("our DecodeUnits != stdlib: got %q want %q", got, s)
	}
}
