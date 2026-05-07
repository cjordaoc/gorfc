// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package ucs2 converts between UTF-8 (Go-side) and UTF-16
// (SAP_UC on Linux and Windows when the SDK is built with
// `-DSAPwithUNICODE`).
//
// The SAP NetWeaver RFC SDK exposes encoders for the same
// pair (`RfcUTF8ToSAPUC`, `RfcSAPUCToUTF8`), but those are
// CGO-only and therefore unusable in pure-Go test code, mock
// backends, and SDK-free CI. This package provides a Go-only
// implementation with the same semantics:
//
//   - Encode produces UTF-16LE on little-endian hosts
//     (matches the SAP_UC layout in the SDK on x86_64 / arm64
//     — SAP defines SAP_UC = unsigned short on those targets,
//     and `mallocU` returns `n * sizeof(SAP_UC)` bytes).
//   - Decode trims the trailing null terminator if present;
//     callers passing an explicit length with no terminator
//     get back exactly that many code units worth of decoded
//     text.
//
// The Go-only path is the source of truth for tests; T1.6/T1.7
// continue to call `RfcUTF8ToSAPUC` / `RfcSAPUCToUTF8` for the
// real SDK path so the SDK's codepage handling (when SAP
// configures non-Unicode trace, language conversion, etc.)
// stays authoritative.
//
// 🟡 verify against SAP NetWeaver RFC SDK Programming Guide:
// the SAP_UC type alias on big-endian SDK builds (z/OS port?)
// is documented to use UTF-16BE; the Go-only path here would
// need byte-swap on those targets. Linux/Windows/Darwin x86_64
// and arm64 are all little-endian, so this is not a problem
// for any tier-1 platform.
package ucs2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrInvalidUTF8 is returned by Encode when the input is not a
// valid UTF-8 string. UTF-8 is the lingua franca of Go strings
// but [string] in Go is a byte sequence — invalid encodings
// must fail explicitly per AGENTS.md "no silent fallback".
var ErrInvalidUTF8 = errors.New("ucs2: input is not valid UTF-8")

// ErrInvalidUTF16 is returned by Decode when the input contains
// an unpaired surrogate. The SDK SHOULD never emit invalid
// UTF-16 but operators using non-Unicode SAP systems with
// codepage tricks can produce it; we surface it explicitly.
var ErrInvalidUTF16 = errors.New("ucs2: input contains an unpaired UTF-16 surrogate")

// Encode converts a Go UTF-8 string to UTF-16LE bytes WITHOUT a
// trailing null terminator. The byte length is always even.
// Use [EncodeNullTerminated] to append a U+0000 code unit
// (two zero bytes), which most SDK call sites expect.
func Encode(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, ErrInvalidUTF8
	}
	codeUnits := utf16.Encode([]rune(s))
	out := make([]byte, len(codeUnits)*2)
	for i, cu := range codeUnits {
		binary.LittleEndian.PutUint16(out[i*2:], cu)
	}
	return out, nil
}

// EncodeNullTerminated wraps [Encode] and appends two zero
// bytes (one UTF-16 null code unit). The byte slice is suitable
// for `RfcSetString` / `RfcSetChars` style writes that expect a
// null-terminated SAP_UC buffer.
func EncodeNullTerminated(s string) ([]byte, error) {
	core, err := Encode(s)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(core)+2)
	copy(out, core)
	// trailing two zero bytes already present (zero-init).
	return out, nil
}

// Decode converts UTF-16LE bytes to a Go UTF-8 string. The
// input length must be even (one code unit per two bytes).
// Trailing null code units are stripped — both the
// canonical "single U+0000 terminator" and the SAP-specific
// "padded with U+0000 to a fixed CHAR length" cases.
//
// If the input contains an unpaired surrogate, Decode returns
// [ErrInvalidUTF16]. The Go-side replacement for that case is
// to fix the producer; we never silently substitute U+FFFD.
func Decode(b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", fmt.Errorf("ucs2: byte length %d is not even", len(b))
	}
	codeUnits := make([]uint16, len(b)/2)
	for i := range codeUnits {
		codeUnits[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return DecodeUnits(codeUnits)
}

// DecodeUnits converts a slice of UTF-16 code units (already
// platform-endian-decoded — the natural layout of `[]C.SAP_UC`
// after a Go-side reflective copy) to a UTF-8 string.
//
// Trailing U+0000 padding is stripped: SAP CHAR fields are
// space-padded *or* zero-padded depending on the codepath. We
// strip both at the right edge to match upstream behavior.
//
// If the input contains an unpaired surrogate, DecodeUnits
// returns [ErrInvalidUTF16].
func DecodeUnits(codeUnits []uint16) (string, error) {
	// Validate surrogate pairing.
	for i := 0; i < len(codeUnits); i++ {
		c := codeUnits[i]
		switch {
		case c >= 0xD800 && c <= 0xDBFF:
			// High surrogate; next must be low surrogate.
			if i+1 >= len(codeUnits) {
				return "", ErrInvalidUTF16
			}
			next := codeUnits[i+1]
			if next < 0xDC00 || next > 0xDFFF {
				return "", ErrInvalidUTF16
			}
			i++ // skip the paired low surrogate
		case c >= 0xDC00 && c <= 0xDFFF:
			// Low surrogate without preceding high surrogate.
			return "", ErrInvalidUTF16
		}
	}
	// Strip trailing U+0000 padding (SAP CHAR / NUM
	// zero-terminated buffer convention).
	end := len(codeUnits)
	for end > 0 && codeUnits[end-1] == 0 {
		end--
	}
	if end == 0 {
		return "", nil
	}
	runes := utf16.Decode(codeUnits[:end])
	return string(runes), nil
}

// RStrip removes trailing space and U+0000 from the right edge
// of s. Equivalent to the legacy `strings.TrimRight(result,
// "\x00 ")` from upstream `gorfc/gorfc.go:wrapString` but
// kept as a separate helper so the rstrip toggle in
// [InvokeOptions] can flip it on/off without touching Decode.
//
// Use RStrip on values returned from CHAR / NUM scalar wrap;
// not on STRING (variable-length) since its trailing whitespace
// is intentional payload.
func RStrip(s string) string {
	return strings.TrimRight(s, "\x00 ")
}

// CountCodeUnits returns the number of UTF-16 code units the
// UTF-8 input would encode to, without allocating the
// intermediate buffer. Useful for sizing SAP_UC allocations
// before calling Encode.
func CountCodeUnits(s string) int {
	n := 0
	for _, r := range s {
		// Surrogate pair for runes outside the BMP.
		if r > 0xFFFF {
			n += 2
			continue
		}
		n++
	}
	return n
}
