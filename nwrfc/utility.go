// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"errors"
	"sync"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// =============================================================
// Trace-level cap (process-global; matches the SDK's process-
// global trace-level state).
// =============================================================
//
// MaxTraceLevel on [Params] declares an upper bound the
// caller's deployment is willing to allow. The SDK accepts
// 0..3 natively; trace level > 0 captures payloads to disk.
// In regulated environments operators set MaxTraceLevel=0 or
// =1 to prevent any downstream call site from raising the
// trace level beyond what was risk-assessed.
//
// We store the cap as a process-global because the SDK trace
// state is itself process-global (RfcSetTraceLevel(NULL,
// NULL, level, ...) in trace.go). Storing it per-Conn would
// invite a race where a relaxed Conn unsets a stricter
// neighbor's cap. The "tightest wins" semantic below makes
// the cap monotonically restrictive across the process
// lifetime; operators relax the cap by restarting the
// process, not by issuing a relaxing call.

var (
	maxTraceLevelMu sync.Mutex
	// maxTraceLevel is 0 = no cap. Any non-zero value caps
	// SetTraceLevel(n) to n <= maxTraceLevel.
	maxTraceLevel int
)

// setMaxTraceLevelFloor applies the "tightest wins" rule:
// the floor only ever moves down. Once set to e.g. 1, no
// later [Params]{MaxTraceLevel: 3} can raise it back to 3.
func setMaxTraceLevelFloor(level int) {
	if level <= 0 || level > 3 {
		return
	}
	maxTraceLevelMu.Lock()
	defer maxTraceLevelMu.Unlock()
	if maxTraceLevel == 0 || level < maxTraceLevel {
		maxTraceLevel = level
	}
}

// currentMaxTraceLevel returns the process-wide cap, or 0
// when none is set. Exposed for testability.
func currentMaxTraceLevel() int {
	maxTraceLevelMu.Lock()
	defer maxTraceLevelMu.Unlock()
	return maxTraceLevel
}

// resetMaxTraceLevelForTest clears the cap. NOT exposed in
// godoc; tests use it via the test-only build tag where
// process-global state would otherwise leak between tests.
// Production code MUST NOT call this — operators relax the
// cap by restarting the process.
func resetMaxTraceLevelForTest() {
	maxTraceLevelMu.Lock()
	defer maxTraceLevelMu.Unlock()
	maxTraceLevel = 0
}

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
//
// Memoized via [sync.OnceValue]: the probe runs at most once
// per process. Subsequent calls return the cached error (or
// nil). This is safe because the SDK link state cannot change
// at runtime; if the binary loaded, the SDK is loaded.
//
// On Windows the probe is diagnostic, not remedial. cgo
// direct linkage resolves DLL imports during process start,
// before any Go code runs — if `sapnwrfc.dll` is not next to
// the .exe, the OS aborts the process before `main`. EnsureSDK
// running successfully therefore implies the loader already
// found the DLLs; we do NOT call `SetDllDirectoryW` from
// init() or here. See docs/DEPLOY.md for VDI packaging.
var EnsureSDK = sync.OnceValue(ensureSDKOnce)

func ensureSDKOnce() error {
	b := backend.Default()
	if b == nil {
		return &SDKUnavailableError{Reason: "no backend registered"}
	}
	if b.Name() == "nosdk" {
		return &SDKUnavailableError{
			Reason:     "library built with -tags nwrfc_nosdk or CGO_ENABLED=0",
			LookupPath: "(nosdk build mode)",
		}
	}
	if b.Name() == "unregistered" {
		return &SDKUnavailableError{Reason: "no backend registered (link both gorfc/internal/sdkbackend and the SAP NW RFC SDK)"}
	}
	v := b.Version()
	if v.IsZero() {
		return &SDKUnavailableError{Reason: "RfcGetVersion returned zero (SDK not loaded)"}
	}
	return nil
}

// SDKVersion returns the loaded SDK version, or the zero
// version when no SDK is loaded. Memoized per process.
var SDKVersion = sync.OnceValue(func() backend.Version {
	return backend.Default().Version()
})

// Capabilities returns the active SDK's runtime feature flags.
// Memoized per process — capabilities are SDK-version-derived
// and cannot change at runtime.
var Capabilities = sync.OnceValue(func() backend.Capabilities {
	return backend.Default().Capabilities()
})

// SetTraceLevel adjusts SDK trace verbosity (0 = off, 1..3
// increasingly verbose). Backend support is optional; returns
// [ErrUnsupported] when the active backend does not implement
// the [backend.Trace] capability.
//
// Honors the process-global cap installed via
// [Params.MaxTraceLevel]. A SetTraceLevel(n) call with n
// greater than the cap is rejected with a *ConfigError
// referencing docs/SECURITY.md §5.
//
// AGENTS.md non-negotiable: trace level > 0 captures payloads
// to disk, including business data and (in some external-auth
// flows) credential material. The cap is therefore a hard
// security gate, not a hint.
//
// SDK function: RfcSetTraceLevel (✅ confirmed when SDK
// linked).
func SetTraceLevel(level int) error {
	if cap := currentMaxTraceLevel(); cap > 0 && level > cap {
		return &ConfigError{
			Field: "trace",
			Hint:  "SetTraceLevel rejected: requested level exceeds Params.MaxTraceLevel cap; see docs/SECURITY.md §5",
		}
	}
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
