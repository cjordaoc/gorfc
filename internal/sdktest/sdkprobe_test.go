// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build gorfc_sdktest && cgo && !nwrfc_nosdk

// Pure-Go test file (no `import "C"`) that drives the cgo
// probes from sdkprobe.go. See the package doc comment for
// why production cgo + test cgo cannot coexist in one
// directory.

package sdktest_test

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/sdktest"
	"github.com/cjordaoc/gorfc/internal/ucs2"
)

var hardStrings = []struct {
	name string
	in   string
}{
	{"ascii", "Hello, SAP NetWeaver."},
	{"latin1-diacritics", "Olá, München, Ñoño, naïve façade ü."},
	{"portuguese", "Inserção de ações: ônibus, propôs, criação."},
	{"cyrillic", "Здравствуй, мир!"},
	{"greek", "Γειά σου Κόσμε"},
	{"hebrew-rtl", "שלום עולם"},
	{"arabic-rtl", "مرحبا بالعالم"},
	{"devanagari", "नमस्ते दुनिया"},
	{"kanji-bmp", "新世界の連邦政府"},
	{"kana", "コンニチハ、サップ。"},
	{"hangul", "안녕하세요 세상"},
	{"emoji-supplementary", "Hello 🌍 🚀 ✨ 🦀"},
	{"emoji-mixed", "Status: ✅ ❌ 🟡 🔴 🟢 — done."},
	{"supplementary-kanji", "𠮷野家"},    // U+20BB7 surrogate pair
	{"math-supplementary", "𝟘 𝟙 𝟚 𝟛"}, // mathematical bold digits
	{"cjk-mixed", "中文 한국어 日本語 — multilingual."},
	{"single-char-bmp", "Ω"},
	{"single-char-supplementary", "🦀"},
}

// TestSDKEncoder_RoundTripVsUCS2 asserts the SDK's
// RfcUTF8ToSAPUC + RfcSAPUCToUTF8 pair preserves every input
// byte-for-byte AND that the SDK's intermediate SAP_UC bytes
// match our pure-Go ucs2 reference.
func TestSDKEncoder_RoundTripVsUCS2(t *testing.T) {
	for _, tc := range hardStrings {
		t.Run(tc.name, func(t *testing.T) {
			res, err := sdktest.EncodeUTF8ToSAPUC(tc.in)
			if err != nil {
				t.Fatalf("EncodeUTF8ToSAPUC: %v", err)
			}
			refBytes, err := ucs2.EncodeNullTerminated(tc.in)
			if err != nil {
				t.Fatalf("ucs2.EncodeNullTerminated: %v", err)
			}
			if !bytes.Equal(res.Bytes, refBytes) {
				t.Errorf("SDK SAP_UC bytes diverge from ucs2 reference\n SDK = % x\n ref = % x", res.Bytes, refBytes)
			}
			if res.Decoded != tc.in {
				t.Errorf("round-trip mismatch:\n input = %q\noutput = %q", tc.in, res.Decoded)
			}
		})
	}
}

// TestSDKEncoder_LongString stresses with 1 MiB of repeated
// hard text to catch off-by-one in buffer sizing.
func TestSDKEncoder_LongString(t *testing.T) {
	chunk := "Здравствуй мир! 🌍 — café × 10000. "
	var b strings.Builder
	for b.Len() < 1<<20 {
		b.WriteString(chunk)
	}
	in := b.String()
	res, err := sdktest.EncodeUTF8ToSAPUC(in)
	if err != nil {
		t.Fatalf("EncodeUTF8ToSAPUC: %v", err)
	}
	if res.Decoded != in {
		t.Errorf("round-trip mismatch on 1 MiB string (lengths in=%d out=%d)", len(in), len(res.Decoded))
	}
}

// TestSDKEncoder_NoLeak hammers the encode/decode/free cycle
// and checks Go-side heap occupancy stays bounded across 100k
// iterations. The cgo path inside sdkprobe.go pairs mallocU
// with the standard free(); a wrong pairing would either crash
// the SDK or grow process RSS monotonically.
func TestSDKEncoder_NoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping leak stress in -short mode")
	}
	for i := 0; i < 100; i++ {
		_, _ = sdktest.EncodeDecodeOnce("warmup")
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const iter = 100_000
	for i := 0; i < iter; i++ {
		_, err := sdktest.EncodeDecodeOnce("Olá 🌍 SAP")
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const slack = 1 << 20 // 1 MiB
	if delta > slack {
		t.Errorf("HeapAlloc grew by %d bytes across %d iterations (slack %d). Possible leak.", delta, iter, slack)
	}
	t.Logf("HeapAlloc delta: %+d bytes across %d iters (slack %d)", delta, iter, slack)
}

// TestRcAsString_KnownCodes confirms the SDK's static lookup
// for RFC_RC values returns non-empty distinct strings.
func TestRcAsString_KnownCodes(t *testing.T) {
	codes := []int32{
		sdktest.RfcOK,
		sdktest.RfcCommunicationFailure,
		sdktest.RfcLogonFailure,
		sdktest.RfcAbapRuntimeFailure,
		sdktest.RfcAbapMessage,
		sdktest.RfcAbapException,
	}
	seen := make(map[string]bool, len(codes))
	for _, rc := range codes {
		s := sdktest.RcAsString(rc)
		if s == "" {
			t.Errorf("RcAsString(%d) returned empty string", rc)
			continue
		}
		if seen[s] {
			t.Errorf("RcAsString(%d)=%q duplicates a previous code", rc, s)
		}
		seen[s] = true
	}
	t.Logf("seen %d distinct RFC_RC stringifications", len(seen))
}

// TestTypeAsString_KnownTypes does the same for RFCTYPE.
func TestTypeAsString_KnownTypes(t *testing.T) {
	types := []int32{
		sdktest.RfcTypeChar,
		sdktest.RfcTypeDate,
		sdktest.RfcTypeBcd,
		sdktest.RfcTypeTime,
		sdktest.RfcTypeByte,
		sdktest.RfcTypeTable,
		sdktest.RfcTypeNum,
		sdktest.RfcTypeFloat,
		sdktest.RfcTypeInt,
		sdktest.RfcTypeInt2,
		sdktest.RfcTypeInt1,
		sdktest.RfcTypeStructure,
		sdktest.RfcTypeDecF16,
		sdktest.RfcTypeDecF34,
		sdktest.RfcTypeString,
		sdktest.RfcTypeXString,
		sdktest.RfcTypeInt8,
		sdktest.RfcTypeUtcLong,
	}
	seen := make(map[string]bool, len(types))
	for _, ty := range types {
		s := sdktest.TypeAsString(ty)
		if s == "" {
			t.Errorf("TypeAsString(%d) returned empty string", ty)
			continue
		}
		if seen[s] {
			t.Errorf("TypeAsString(%d)=%q duplicates a previous type", ty, s)
		}
		seen[s] = true
	}
	t.Logf("seen %d distinct RFCTYPE stringifications", len(seen))
}

// TestDirectionAsString_KnownDirections does the same for
// RFC_DIRECTION.
func TestDirectionAsString_KnownDirections(t *testing.T) {
	dirs := []int32{
		sdktest.RfcImport,
		sdktest.RfcExport,
		sdktest.RfcChanging,
		sdktest.RfcTables,
	}
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		s := sdktest.DirectionAsString(d)
		if s == "" {
			t.Errorf("DirectionAsString(%d) returned empty string", d)
			continue
		}
		if seen[s] {
			t.Errorf("DirectionAsString(%d)=%q duplicates a previous direction", d, s)
		}
		seen[s] = true
	}
	t.Logf("seen %d distinct RFC_DIRECTION stringifications", len(seen))
}
