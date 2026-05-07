// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// SDK-but-no-SAP behavior tests. These exercise SAP NetWeaver
// RFC SDK functions whose semantics are fully observable from
// inside the loaded library — no AS ABAP / no network needed.
// They flip the `🟡 needs verify` markers in
// docs/SDK_FUNCTIONS_MAP.md from "symbol exists at link time"
// to "behavior verified end-to-end on PL18".
//
// Build tag: same as the cgo backend — these tests REQUIRE
// libsapnwrfc to be loadable, which is the gate for everything
// they test.

package nwrfc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestSDKVersion_NotZero confirms the loaded SDK is reporting a
// real version. With the fix landed in
// internal/sdkbackend/version.go this is the canonical way for
// downstream apps to fail-fast on SDK-less deployments via
// nwrfc.EnsureSDK().
func TestSDKVersion_NotZero(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	v := nwrfc.SDKVersion()
	if v.IsZero() {
		t.Fatal("SDKVersion is zero")
	}
	if v.Major != 7 {
		t.Errorf("Major=%d, want 7", v.Major)
	}
	if v.Minor != 50 {
		t.Errorf("Minor=%d, want 50 (we link against the 7.50 SDK family)", v.Minor)
	}
	// PatchLevel changes per SDK release; just assert it's a
	// reasonable non-zero number rather than pinning to the
	// specific PL we happen to build against today.
	if v.PatchLevel < 3 {
		t.Errorf("PatchLevel=%d looks too low; INSTALL.md requires PL12+", v.PatchLevel)
	}
	t.Logf("SDK version %d.%d.%d (PL%d)", v.Major, v.Minor, v.PatchLevel, v.PatchLevel)
}

// TestCapabilities_DerivedFromVersion confirms the version-gated
// capabilities reflect the actual SDK we loaded. We don't assert
// concrete bool values because they're driven by the version
// thresholds; we assert internal consistency: UTCLong is always
// available on 7.50+ (the version threshold is 7.50.0), so any
// 7.50-or-higher SDK should report it true.
func TestCapabilities_DerivedFromVersion(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	v := nwrfc.SDKVersion()
	caps := nwrfc.Capabilities()
	if v.AtLeast(7, 50, 0) && !caps.UTCLong {
		t.Errorf("SDK %v claims to be 7.50+, but caps.UTCLong=false", v)
	}
	t.Logf("caps=%+v on SDK %v", caps, v)
}

// TestLanguageISOToSAP_KnownLanguages exercises RfcLanguageIsoToSap
// for the canonical set of languages every SAP install ships.
// The mapping is fixed in SAP's t002 table; the SDK is just
// looking it up.
func TestLanguageISOToSAP_KnownLanguages(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	cases := map[string]string{
		"EN": "E",
		"DE": "D",
		"PT": "P",
		"FR": "F",
		"ES": "S",
		"JA": "J",
		"ZH": "1", // SAP uses "1" for Mandarin
	}
	for iso, want := range cases {
		got, err := nwrfc.LanguageISOToSAP(iso)
		if err != nil {
			t.Errorf("LanguageISOToSAP(%q): %v", iso, err)
			continue
		}
		if got != want {
			t.Errorf("LanguageISOToSAP(%q)=%q, want %q", iso, got, want)
		}
	}
}

// TestLanguageSAPToISO_RoundTrip walks the inverse and confirms
// the SDK's table is symmetric for our cases.
func TestLanguageSAPToISO_RoundTrip(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	for _, iso := range []string{"EN", "DE", "PT", "FR", "ES", "JA"} {
		sap, err := nwrfc.LanguageISOToSAP(iso)
		if err != nil {
			t.Fatalf("LanguageISOToSAP(%q): %v", iso, err)
		}
		if len(sap) != 1 {
			t.Fatalf("LanguageISOToSAP(%q)=%q (length %d), expected 1 code unit; this would mean the buffer-zero fix in lang.go regressed", iso, sap, len(sap))
		}
		got, err := nwrfc.LanguageSAPToISO(sap)
		if err != nil {
			t.Fatalf("LanguageSAPToISO(%q): %v", sap, err)
		}
		// SAP returns lowercase ISO codes in some PLs and
		// uppercase in others. Compare case-insensitively.
		if !strings.EqualFold(got, iso) {
			t.Errorf("LanguageSAPToISO(%q)=%q, want %q", sap, got, iso)
		}
	}
}

// TestLanguageISOToSAP_RejectsBadInput confirms the wrapper
// surfaces malformed input as a Go error before going to the
// SDK. We check Go-side validation here; the SDK's own error
// path for unknown but well-formed codes is exercised by the
// next test.
func TestLanguageISOToSAP_RejectsBadInput(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	for _, bad := range []string{"", "E", "ENG", "english"} {
		_, err := nwrfc.LanguageISOToSAP(bad)
		if err == nil {
			t.Errorf("LanguageISOToSAP(%q) returned nil error", bad)
		}
	}
}

// TestSetTraceLevel_RangeCheck confirms the wrapper rejects
// out-of-range trace levels at the Go side, before hitting the
// SDK (which silently ignores some out-of-range values, masking
// configuration bugs).
func TestSetTraceLevel_RangeCheck(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	for _, bad := range []int{-1, 4, 99} {
		err := nwrfc.SetTraceLevel(bad)
		if err == nil {
			t.Errorf("SetTraceLevel(%d) returned nil; want error", bad)
		}
	}
	// Reset to off; valid range.
	if err := nwrfc.SetTraceLevel(0); err != nil {
		t.Errorf("SetTraceLevel(0) returned %v; want nil", err)
	}
}

// TestSetTraceDir_RealFile sets the trace dir to a temp dir,
// raises the trace level (which causes the SDK to write a
// startup line), then resets level to 0 and confirms a trace
// file landed in our temp dir. This is the strongest behavioral
// proof we can get for the trace plumbing without a connection.
func TestSetTraceDir_RealFile(t *testing.T) {
	if err := nwrfc.EnsureSDK(); err != nil {
		t.Fatalf("EnsureSDK: %v", err)
	}
	tmp := t.TempDir()
	if err := nwrfc.SetTraceDir(tmp); err != nil {
		t.Fatalf("SetTraceDir(%q): %v", tmp, err)
	}
	t.Cleanup(func() {
		// Reset trace dir + level so subsequent tests run
		// in the same process don't accumulate trace files
		// in a torn-down temp dir.
		_ = nwrfc.SetTraceLevel(0)
	})

	if err := nwrfc.SetTraceLevel(1); err != nil {
		t.Fatalf("SetTraceLevel(1): %v", err)
	}
	// The SDK only flushes trace on subsequent calls; nudge it
	// with a no-op SDK call. SDKVersion goes through
	// RfcGetVersion which IS traced at level >= 1.
	_ = nwrfc.SDKVersion()
	if err := nwrfc.SetTraceLevel(0); err != nil {
		t.Fatalf("SetTraceLevel(0): %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmp, "*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	// A trace level > 0 should produce at least one file in
	// the dir. Empty means the SDK silently dropped our
	// SetTraceDir which would be a real regression.
	if len(matches) == 0 {
		t.Errorf("SetTraceDir(%q) + SetTraceLevel(1) produced no files", tmp)
	} else {
		// Log the file names so a reader can see what the
		// SDK actually wrote on this PL.
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, filepath.Base(m))
		}
		t.Logf("trace files written: %v", names)
	}
	// Sanity: stat them so we know they're real files, not
	// directories the SDK created for whatever reason.
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			t.Errorf("stat %q: %v", m, err)
			continue
		}
		if fi.IsDir() {
			t.Errorf("%q is a dir, expected a regular file", m)
		}
	}
}
