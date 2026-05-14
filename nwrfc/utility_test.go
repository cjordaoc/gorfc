// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// TestMaxTraceLevel_RejectsAboveCap installs a process-global
// trace cap of 1 and asserts that SetTraceLevel(2) is rejected
// with *ConfigError. The cap is the security gate documented
// in docs/SECURITY.md §5; tests here keep that guarantee
// honest.
//
// We exercise the public API via a backend that does NOT
// implement [backend.Trace] so we can prove the cap fires
// BEFORE the unsupported-feature error — i.e. we never reach
// the SDK with an out-of-cap level.
func TestMaxTraceLevel_RejectsAboveCap(t *testing.T) {
	t.Cleanup(resetMaxTraceLevelForTest)

	setMaxTraceLevelFloor(1)

	err := SetTraceLevel(2)
	if err == nil {
		t.Fatal("SetTraceLevel(2) under cap=1 returned nil")
	}
	if !errors.Is(err, ErrConfig) {
		t.Errorf("err=%v; want errors.Is ErrConfig", err)
	}
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As did not extract *ConfigError")
	}
	if ce.Field != "trace" {
		t.Errorf("ConfigError.Field=%q; want trace", ce.Field)
	}
}

// TestMaxTraceLevel_AcceptsAtCap: the boundary value is allowed.
// (The backend without Trace capability surfaces the
// unsupported-feature error — that is acceptable; what we
// assert here is that the cap did NOT trigger.)
func TestMaxTraceLevel_AcceptsAtCap(t *testing.T) {
	t.Cleanup(resetMaxTraceLevelForTest)

	setMaxTraceLevelFloor(2)

	err := SetTraceLevel(2)
	// Without a Trace-capable backend the call surfaces
	// *UnsupportedFeatureError. That is fine for this test —
	// we only need to prove the cap did not block 2 <= 2.
	if errors.Is(err, ErrConfig) {
		t.Errorf("cap rejected level==cap (level=2 cap=2): %v", err)
	}
}

// TestMaxTraceLevel_TightestWins: subsequent calls with a
// looser bound do NOT relax the cap. The cap can only be
// tightened; relaxing requires a process restart.
func TestMaxTraceLevel_TightestWins(t *testing.T) {
	t.Cleanup(resetMaxTraceLevelForTest)

	setMaxTraceLevelFloor(2)
	setMaxTraceLevelFloor(3) // attempt to relax — must be ignored
	if got := currentMaxTraceLevel(); got != 2 {
		t.Errorf("currentMaxTraceLevel after relax-attempt=%d; want 2", got)
	}
	setMaxTraceLevelFloor(1) // tighten further
	if got := currentMaxTraceLevel(); got != 1 {
		t.Errorf("currentMaxTraceLevel after tighten=%d; want 1", got)
	}
}

// TestMaxTraceLevel_ZeroIsNoCap: the default zero means "no
// cap", any level passes the gate (the unsupported-backend
// error is unrelated).
func TestMaxTraceLevel_ZeroIsNoCap(t *testing.T) {
	t.Cleanup(resetMaxTraceLevelForTest)

	if got := currentMaxTraceLevel(); got != 0 {
		t.Fatalf("baseline cap=%d; want 0", got)
	}
	err := SetTraceLevel(3)
	if errors.Is(err, ErrConfig) {
		t.Errorf("zero cap rejected level=3 unexpectedly: %v", err)
	}
}

// TestEnforceCapabilities_WSHostHardlock asserts that an Open
// with WSHost set against a backend without WebSocketRFC
// capability fails fast with *UnsupportedFeatureError. No
// silent downgrade.
func TestEnforceCapabilities_WSHostHardlock(t *testing.T) {
	p := Params{
		WSHost: "ws.example.invalid",
		WSPort: "443",
		User:   "u",
		Passwd: "p",
		Client: "100",
	}
	err := enforceCapabilities(p, fakeNoWSBackend)
	if err == nil {
		t.Fatal("enforceCapabilities returned nil for WSHost without capability")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err=%v; want errors.Is ErrUnsupported", err)
	}
	var uf *UnsupportedFeatureError
	if !errors.As(err, &uf) {
		t.Fatalf("errors.As did not extract *UnsupportedFeatureError")
	}
	if uf.Feature != "WebSocketRFC" {
		t.Errorf("Feature=%q; want WebSocketRFC", uf.Feature)
	}
	// Required version pinned at 7.50 PL10 per
	// docs/SDK_FUNCTIONS_MAP.md; refuse unrelated drift.
	if !uf.RequiredVersion.AtLeast(7, 50, 10) {
		t.Errorf("RequiredVersion=%s; want >= 7.50 PL10", uf.RequiredVersion)
	}
}

// TestEnforceCapabilities_WSHostAllowedWhenCapable: a backend
// that advertises WebSocketRFC must let the call through.
func TestEnforceCapabilities_WSHostAllowedWhenCapable(t *testing.T) {
	p := Params{WSHost: "ws.example.invalid", WSPort: "443"}
	if err := enforceCapabilities(p, fakeWithWSBackend); err != nil {
		t.Errorf("enforceCapabilities rejected capable backend: %v", err)
	}
}

// TestEnforceCapabilities_NoWSHostNoOp: with no WSHost, no
// capability check fires regardless of backend support.
func TestEnforceCapabilities_NoWSHostNoOp(t *testing.T) {
	p := Params{AsHost: "h", SysNr: "00"}
	if err := enforceCapabilities(p, fakeNoWSBackend); err != nil {
		t.Errorf("enforceCapabilities rejected non-WS conn: %v", err)
	}
}

// fakeCapBackend is a minimal full-interface backend used to
// exercise [enforceCapabilities] without involving the SDK. The
// only fields that actually matter are `caps` and `version`;
// everything else is a no-op stub matching the Backend
// contract.
type fakeCapBackend struct {
	caps    backend.Capabilities
	version backend.Version
}

func (b *fakeCapBackend) Name() string                       { return "fakeCap" }
func (b *fakeCapBackend) Version() backend.Version           { return b.version }
func (b *fakeCapBackend) Capabilities() backend.Capabilities { return b.caps }
func (*fakeCapBackend) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 1, nil
}
func (*fakeCapBackend) Close(backend.ConnHandle) error                     { return nil }
func (*fakeCapBackend) Ping(_ context.Context, _ backend.ConnHandle) error { return nil }
func (*fakeCapBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, nil
}
func (*fakeCapBackend) Reset(_ context.Context, _ backend.ConnHandle) error { return nil }
func (*fakeCapBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (*fakeCapBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (*fakeCapBackend) InvalidateMetadata(string) error { return nil }

// fakeNoWS / fakeWithWS are pre-configured singletons used by
// the capability-hardlock tests so each test reads as one
// thought ("with WebSocket support" / "without").
var (
	fakeNoWSBackend   = &fakeCapBackend{version: backend.Version{Major: 7, Minor: 50, PatchLevel: 3}}
	fakeWithWSBackend = &fakeCapBackend{
		version: backend.Version{Major: 7, Minor: 50, PatchLevel: 18},
		caps:    backend.Capabilities{WebSocketRFC: true},
	}
)
