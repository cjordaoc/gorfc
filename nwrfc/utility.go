// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"errors"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// EnsureSDK reports whether the SAP NetWeaver RFC SDK is
// available in this build. Returns nil when the cgo backend
// is registered AND RfcGetVersion reports a non-zero version.
// Returns *SDKUnavailableError otherwise.
//
// Use during process start to fail-fast on SDK-less
// deployments:
//
//	if err := nwrfc.EnsureSDK(); err != nil {
//	    log.Fatalf("nwrfc: %v", err)
//	}
//
// Equivalent to SapNwRfc's `EnsureLibraryPresent` and node-rfc's
// implicit version probe.
func EnsureSDK() error {
	b := backend.Default()
	if b == nil {
		return &SDKUnavailableError{Reason: "no backend registered"}
	}
	if b.Name() == "nosdk" {
		return &SDKUnavailableError{Reason: "library built with -tags nwrfc_nosdk or CGO_ENABLED=0"}
	}
	v := b.Version()
	if v.IsZero() {
		return &SDKUnavailableError{Reason: "RfcGetVersion returned zero (SDK not loaded)"}
	}
	return nil
}

// SDKVersion returns the loaded SDK version, or the zero
// version when no SDK is loaded.
func SDKVersion() backend.Version {
	return backend.Default().Version()
}

// Capabilities returns the active SDK's runtime feature flags.
func Capabilities() backend.Capabilities {
	return backend.Default().Capabilities()
}

// SetTraceLevel adjusts SDK trace verbosity (0 = off, 1..3
// increasingly verbose). Backend support is optional; returns
// [ErrUnsupported] when the active backend does not implement
// the [backend.Trace] capability.
//
// SDK function: RfcSetTraceLevel (✅ confirmed when SDK
// linked). 🟡 verify cross-version flag semantics; trace level
// > 0 captures payloads (see docs/SECURITY.md §5).
func SetTraceLevel(level int) error {
	b := backend.Default()
	t, ok := b.(backend.Trace)
	if !ok {
		return &UnsupportedFeatureError{Feature: "SetTraceLevel", CurrentVersion: b.Version()}
	}
	return t.SetTraceLevel(level)
}

// SetTraceDir redirects SDK trace output to the given dir.
func SetTraceDir(dir string) error {
	b := backend.Default()
	t, ok := b.(backend.Trace)
	if !ok {
		return &UnsupportedFeatureError{Feature: "SetTraceDir", CurrentVersion: b.Version()}
	}
	return t.SetTraceDir(dir)
}

// LanguageISOToSAP converts an ISO 639-1 code (e.g. "en") to
// the SAP one-letter code (e.g. "E"). Wraps the SDK
// `RfcLanguageIsoToSap`.
func LanguageISOToSAP(iso string) (string, error) {
	b := backend.Default()
	conv, ok := b.(backend.LanguageConverter)
	if !ok {
		return "", &UnsupportedFeatureError{Feature: "LanguageISOToSAP", CurrentVersion: b.Version()}
	}
	return conv.LanguageISOToSAP(iso)
}

// LanguageSAPToISO converts a SAP one-letter language code
// (e.g. "E") to ISO 639-1 (e.g. "en"). Wraps
// `RfcLanguageSapToIso`.
func LanguageSAPToISO(sap string) (string, error) {
	b := backend.Default()
	conv, ok := b.(backend.LanguageConverter)
	if !ok {
		return "", &UnsupportedFeatureError{Feature: "LanguageSAPToISO", CurrentVersion: b.Version()}
	}
	return conv.LanguageSAPToISO(sap)
}

// LoadCryptoLibrary loads SAP CommonCryptoLib (libsapcrypto)
// for SNC and WebSocket TLS. Wraps `RfcLoadCryptoLibrary`.
//
// 🟡 verify minimum SDK PL: node-rfc release notes mention
// 7.50 PL10+; the programming guide should confirm.
func LoadCryptoLibrary(path string) error {
	b := backend.Default()
	cl, ok := b.(backend.CryptoLoader)
	if !ok {
		return &UnsupportedFeatureError{Feature: "LoadCryptoLibrary", CurrentVersion: b.Version()}
	}
	return cl.LoadCryptoLibrary(path)
}

// SetIniPath redirects the SDK to a different sapnwrfc.ini
// search path. Wraps `RfcSetIniPath`.
func SetIniPath(dir string) error {
	b := backend.Default()
	r, ok := b.(backend.IniReloader)
	if !ok {
		return &UnsupportedFeatureError{Feature: "SetIniPath", CurrentVersion: b.Version()}
	}
	return r.SetIniPath(dir)
}

// ReloadIniFile asks the SDK to re-read sapnwrfc.ini after a
// change at runtime. Wraps `RfcReloadIniFile`.
func ReloadIniFile() error {
	b := backend.Default()
	r, ok := b.(backend.IniReloader)
	if !ok {
		return &UnsupportedFeatureError{Feature: "ReloadIniFile", CurrentVersion: b.Version()}
	}
	return r.ReloadIniFile()
}

// _ keeps the errors import live; reserved for future
// errors.Join patterns in capability dispatch.
var _ = errors.New
