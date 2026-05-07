// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestVersion_AtLeast(t *testing.T) {
	cases := []struct {
		v          Version
		mj, mn, pl uint
		want       bool
	}{
		{Version{7, 50, 12}, 7, 50, 12, true},
		{Version{7, 50, 12}, 7, 50, 13, false},
		{Version{7, 50, 12}, 7, 50, 0, true},
		{Version{7, 50, 12}, 7, 49, 99, true},
		{Version{7, 50, 12}, 7, 51, 0, false},
		{Version{7, 50, 12}, 8, 0, 0, false},
		{Version{8, 0, 0}, 7, 50, 99, true},
		{Version{}, 7, 50, 0, false}, // zero version refuses everything
	}
	for _, tc := range cases {
		got := tc.v.AtLeast(tc.mj, tc.mn, tc.pl)
		if got != tc.want {
			t.Errorf("Version{%d,%d,%d}.AtLeast(%d,%d,%d)=%v, want %v",
				tc.v.Major, tc.v.Minor, tc.v.PatchLevel,
				tc.mj, tc.mn, tc.pl, got, tc.want)
		}
	}
}

func TestVersion_String(t *testing.T) {
	if got := (Version{}).String(); got != "no-sdk" {
		t.Errorf("zero version: got %q want %q", got, "no-sdk")
	}
	if got := (Version{7, 50, 12}).String(); got != "7.50 PL12" {
		t.Errorf("Version{7,50,12}: got %q want %q", got, "7.50 PL12")
	}
}

func TestRFCType_String(t *testing.T) {
	cases := []struct {
		t    RFCType
		want string
	}{
		{TypeChar, "RFCTYPE_CHAR"},
		{TypeDate, "RFCTYPE_DATE"},
		{TypeBCD, "RFCTYPE_BCD"},
		{TypeStructure, "RFCTYPE_STRUCTURE"},
		{TypeUTCLong, "RFCTYPE_UTCLONG"},
		{RFCType(99), "RFCTYPE_UNKNOWN(99)"},
	}
	for _, tc := range cases {
		if got := tc.t.String(); got != tc.want {
			t.Errorf("RFCType(%d).String()=%q want %q", tc.t, got, tc.want)
		}
	}
}

func TestParams_LogValue_Redacts(t *testing.T) {
	p := Params{
		"user":            "demo",
		"passwd":          "supersecret",
		"ashost":          "sap.example.invalid",
		"snc_partnername": "p:CN=corp.example.invalid",
		"mysapsso2":       "ABC...DEF",
		"x509cert":        "-----BEGIN CERTIFICATE-----...",
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("connect", "params", p)

	out := buf.String()
	t.Logf("emit: %s", out)

	// Sensitive values must NOT be in the output.
	for _, leak := range []string{
		"supersecret", "ABC...DEF", "BEGIN CERTIFICATE",
		"p:CN=corp.example.invalid",
	} {
		if strings.Contains(out, leak) {
			t.Errorf("Params.LogValue leaked %q in: %s", leak, out)
		}
	}
	// Non-sensitive values MUST appear.
	for _, ok := range []string{"demo", "sap.example.invalid"} {
		if !strings.Contains(out, ok) {
			t.Errorf("Params.LogValue dropped %q from: %s", ok, out)
		}
	}
	// Redaction marker must appear at least 4 times (4 sensitive keys).
	if c := strings.Count(out, "«redacted»"); c < 4 {
		t.Errorf("expected ≥4 «redacted» markers, got %d", c)
	}
}

func TestCategory_String_AllNamed(t *testing.T) {
	// Spot-check that every constant has a non-"unknown" name.
	for c := CategoryLogon; c <= CategoryUnsupported; c++ {
		if c.String() == "unknown" {
			t.Errorf("Category(%d) returned %q", c, "unknown")
		}
	}
	if CategoryUnknown.String() != "unknown" {
		t.Errorf("CategoryUnknown got %q want %q", CategoryUnknown.String(), "unknown")
	}
}

func TestRegister_Unregistered_FailsExplicitly(t *testing.T) {
	// Default() returns the unregistered sentinel when active is nil.
	// Save/restore active to keep tests hermetic.
	registryMu.Lock()
	prev := active
	active = nil
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		active = prev
		registryMu.Unlock()
	})

	b := Default()
	if b == nil {
		t.Fatal("Default() returned nil")
	}
	if got := b.Name(); got != "unregistered" {
		t.Errorf("Name=%q want %q", got, "unregistered")
	}

	ctx := context.Background()
	_, err := b.Open(ctx, Params{"user": "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Open: err=%v, want errors.Is ErrUnavailable", err)
	}
	if err := b.Close(0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Close: err=%v", err)
	}
	if err := b.Ping(ctx, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Ping: err=%v", err)
	}
	if err := b.InvalidateMetadata("X"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("InvalidateMetadata: err=%v", err)
	}
}

func TestSetTesting_Restores(t *testing.T) {
	registryMu.RLock()
	prev := active
	registryMu.RUnlock()

	mock := &fakeBackend{name: "fake"}
	restore := SetTesting(mock)

	if Default() != mock {
		t.Fatal("SetTesting did not install the mock")
	}
	restore()
	registryMu.RLock()
	got := active
	registryMu.RUnlock()
	if got != prev {
		t.Errorf("restore did not put the previous backend back: got %v want %v", got, prev)
	}
}

func TestDate_IsZero(t *testing.T) {
	if !(Date{}).IsZero() {
		t.Error("Date{} should be zero")
	}
	if (Date{Year: 2026, Month: 5, Day: 6}).IsZero() {
		t.Error("non-zero Date should not be zero")
	}
}

func TestTime_IsZero(t *testing.T) {
	if !(Time{}).IsZero() {
		t.Error("Time{} should be zero")
	}
	if (Time{Hour: 12}).IsZero() {
		t.Error("non-zero Time should not be zero")
	}
}

// fakeBackend is a tiny stand-in used to test [SetTesting]; it
// satisfies the [Backend] interface with errors so tests that
// accidentally route to it are caught.
type fakeBackend struct{ name string }

func (b *fakeBackend) Name() string                                         { return b.name }
func (b *fakeBackend) Version() Version                                     { return Version{} }
func (b *fakeBackend) Capabilities() Capabilities                           { return Capabilities{} }
func (b *fakeBackend) Open(_ context.Context, _ Params) (ConnHandle, error) { return 0, ErrUnavailable }
func (b *fakeBackend) Close(ConnHandle) error                               { return ErrUnavailable }
func (b *fakeBackend) Ping(_ context.Context, _ ConnHandle) error           { return ErrUnavailable }
func (b *fakeBackend) Attributes(ConnHandle) (Attributes, error)            { return Attributes{}, ErrUnavailable }
func (b *fakeBackend) Reset(ConnHandle) error                               { return ErrUnavailable }
func (b *fakeBackend) Describe(_ context.Context, _ ConnHandle, _ string) (FunctionDescriptor, error) {
	return FunctionDescriptor{}, ErrUnavailable
}
func (b *fakeBackend) Invoke(_ context.Context, _ ConnHandle, _ string, _ CallParams, _ InvokeOptions) (CallParams, error) {
	return nil, ErrUnavailable
}
func (b *fakeBackend) InvalidateMetadata(string) error { return ErrUnavailable }
