// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestParams_Validate_HappyPath: each transport shape passes.
func TestParams_Validate_HappyPath(t *testing.T) {
	cases := map[string]nwrfc.Params{
		"direct": {AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100"},
		"lb":     {MsHost: "ms", R3Name: "PRD", Group: "PUBLIC", User: "u", Passwd: "p", Client: "100"},
		"ws":     {WSHost: "ws", WSPort: "443", User: "u", Passwd: "p", Client: "100"},
		"dest":   {Dest: "DEV"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			_, err := nwrfc.Open(ctx, p)
			// We don't have a real backend here, so Open
			// will fail at the backend layer; that's expected.
			// What we want to assert: validate() did NOT
			// reject the input — i.e. the error category is
			// NOT CategoryConfig.
			if err == nil {
				t.Fatal("Open returned no error against the no-SDK backend")
			}
			if cat := nwrfc.CategoryOf(err); cat == backend.CategoryConfig {
				t.Errorf("validate() rejected %s: %v", name, err)
			}
		})
	}
}

// TestParams_Validate_MissingTransport: rejects empty Params.
func TestParams_Validate_MissingTransport(t *testing.T) {
	_, err := nwrfc.Open(context.Background(), nwrfc.Params{User: "u"})
	if !errors.Is(err, nwrfc.ErrConfig) {
		t.Errorf("err=%v want ErrConfig", err)
	}
	var ce *nwrfc.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As did not extract *ConfigError")
	}
	if ce.Field != "transport" {
		t.Errorf("Field=%q want %q", ce.Field, "transport")
	}
}

// TestParams_Validate_MissingSysNr: AsHost requires SysNr.
func TestParams_Validate_MissingSysNr(t *testing.T) {
	_, err := nwrfc.Open(context.Background(), nwrfc.Params{AsHost: "h", User: "u", Passwd: "p"})
	if !errors.Is(err, nwrfc.ErrConfig) {
		t.Fatalf("err=%v want ErrConfig", err)
	}
	var ce *nwrfc.ConfigError
	errors.As(err, &ce)
	if ce.Field != "SysNr" {
		t.Errorf("Field=%q want %q", ce.Field, "SysNr")
	}
}

// TestParams_Validate_MultipleAuth: at most one auth method.
func TestParams_Validate_MultipleAuth(t *testing.T) {
	_, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00",
		Passwd:    "p",
		Mysapsso2: "ticket",
	})
	if !errors.Is(err, nwrfc.ErrConfig) {
		t.Fatalf("err=%v want ErrConfig", err)
	}
}

// TestOpenDest_RequiresName: empty dest fails fast.
func TestOpenDest_RequiresName(t *testing.T) {
	_, err := nwrfc.OpenDest(context.Background(), "")
	if !errors.Is(err, nwrfc.ErrConfig) {
		t.Errorf("err=%v want ErrConfig", err)
	}
}

// TestOpen_CancelledContextFailsBeforeBackend: Open must honor an
// already-cancelled ctx without validating Params or dispatching
// to the active backend.
func TestOpen_CancelledContextFailsBeforeBackend(t *testing.T) {
	b := &openShouldNotRunBackend{}
	prev := backend.SetTesting(b)
	t.Cleanup(prev)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := nwrfc.Open(ctx, nwrfc.Params{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
	if b.openCalled {
		t.Fatal("backend Open was called despite cancelled context")
	}
}

// TestParams_StringerRedacts asserts that fmt.Sprintf("%v") /
// "%s" / "%+v" / "%#v" all go through the redacted Stringer
// rather than the default reflection-based formatter. Without
// this, a single accidental fmt.Println(p) leaks the password.
func TestParams_StringerRedacts(t *testing.T) {
	p := nwrfc.Params{
		AsHost:    "sap.example.invalid",
		SysNr:     "00",
		User:      "demo",
		Passwd:    "supersecret",
		Mysapsso2: "ticket-xyz",
		Bearer:    "bearer-zzz",
		Extra: map[string]string{
			"custom_token":  "tok-AAA",
			"api_secret":    "sek-BBB",
			"snc_qos_token": "snc-CCC",
			"safe_field":    "ok-DDD",
		},
	}
	for _, verb := range []string{"%v", "%s", "%+v", "%#v"} {
		out := fmt.Sprintf(verb, p)
		for _, leak := range []string{
			"supersecret", "ticket-xyz", "bearer-zzz",
			"tok-AAA", "sek-BBB", "snc-CCC",
		} {
			if strings.Contains(out, leak) {
				t.Errorf("verb=%s leaked %q in: %s", verb, leak, out)
			}
		}
		if !strings.Contains(out, "ok-DDD") {
			t.Errorf("verb=%s dropped non-sensitive Extra value: %s", verb, out)
		}
		// Non-sensitive named field still emitted.
		if !strings.Contains(out, "sap.example.invalid") {
			t.Errorf("verb=%s dropped AsHost: %s", verb, out)
		}
	}
}

// TestParams_ExtraRedactedInLogValue: Params.Extra entries
// whose keys match the sensitive prefix/suffix matcher are
// redacted by slog output as well as by Stringer. Same single
// source of truth (backend.IsSensitiveKey).
func TestParams_ExtraRedactedInLogValue(t *testing.T) {
	const sec = "must-not-leak-XYZ"
	p := nwrfc.Params{
		AsHost: "h", SysNr: "00",
		Extra: map[string]string{
			"client_secret": sec,
			"my_token":      sec + "-2",
			"snc_audit":     sec + "-3", // snc* prefix
			"safe":          "okay-keep",
		},
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("connect", "params", p)
	out := buf.String()
	for _, leak := range []string{sec, sec + "-2", sec + "-3"} {
		if strings.Contains(out, leak) {
			t.Errorf("Extra leaked %q in: %s", leak, out)
		}
	}
	if !strings.Contains(out, "okay-keep") {
		t.Errorf("Extra dropped non-sensitive value: %s", out)
	}
}

// TestParams_LogValueRedacts: passwords and tickets do not
// reach slog output.
func TestParams_LogValueRedacts(t *testing.T) {
	p := nwrfc.Params{
		AsHost:         "sap.example.invalid",
		SysNr:          "00",
		User:           "demo",
		Passwd:         "supersecret",
		Mysapsso2:      "ticket-xyz",
		SncPartnerName: "p:CN=corp.example.invalid",
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("connect", "params", p)
	out := buf.String()

	for _, leak := range []string{"supersecret", "ticket-xyz", "p:CN=corp.example.invalid"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in: %s", leak, out)
		}
	}
	for _, ok := range []string{"sap.example.invalid", "demo"} {
		if !strings.Contains(out, ok) {
			t.Errorf("dropped %q from: %s", ok, out)
		}
	}
}

// TestConn_NilSafe: nil Conn methods do not panic.
func TestConn_NilSafe(t *testing.T) {
	var c *nwrfc.Conn
	if c.Alive() {
		t.Error("nil Conn reported Alive")
	}
	if err := c.Close(); err != nil {
		t.Errorf("nil Conn.Close()=%v want nil", err)
	}
}

// TestConn_LogValue: state field reflects open/closed.
func TestConn_LogValue_StateField(t *testing.T) {
	// Use a cooperative mock backend that returns a real handle
	// from Open so we can test LogValue on a non-failed Conn.
	prev := backend.SetTesting(&happyBackend{})
	t.Cleanup(prev)

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("c", "conn", c)
	if !strings.Contains(buf.String(), `"state":"open"`) {
		t.Errorf("LogValue did not emit state=open: %s", buf.String())
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.Alive() {
		t.Error("Alive after Close")
	}
	// Use after close should fail.
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping after Close: nil error")
	} else if !errors.Is(err, nwrfc.ErrBrokenConn) {
		t.Errorf("Ping after Close: err=%v want ErrBrokenConn", err)
	}
	// Idempotent close.
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// happyBackend is a minimal in-test backend that succeeds the
// lifecycle calls with deterministic values.
type happyBackend struct{}

func (*happyBackend) Name() string { return "happy" }
func (*happyBackend) Version() backend.Version {
	return backend.Version{Major: 7, Minor: 50, PatchLevel: 12}
}
func (*happyBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (*happyBackend) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 42, nil
}
func (*happyBackend) Close(backend.ConnHandle) error                     { return nil }
func (*happyBackend) Ping(_ context.Context, _ backend.ConnHandle) error { return nil }
func (*happyBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{SysID: "TST"}, nil
}
func (*happyBackend) Reset(context.Context, backend.ConnHandle) error { return nil }
func (*happyBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (*happyBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (*happyBackend) InvalidateMetadata(string) error { return nil }

type openShouldNotRunBackend struct {
	openCalled bool
}

func (*openShouldNotRunBackend) Name() string { return "open-should-not-run" }
func (*openShouldNotRunBackend) Version() backend.Version {
	return backend.Version{}
}
func (*openShouldNotRunBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}
func (b *openShouldNotRunBackend) Open(context.Context, backend.Params) (backend.ConnHandle, error) {
	b.openCalled = true
	return 0, errors.New("backend Open should not run")
}
func (*openShouldNotRunBackend) Close(backend.ConnHandle) error { return nil }
func (*openShouldNotRunBackend) Ping(context.Context, backend.ConnHandle) error {
	return nil
}
func (*openShouldNotRunBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, nil
}
func (*openShouldNotRunBackend) Reset(context.Context, backend.ConnHandle) error { return nil }
func (*openShouldNotRunBackend) Describe(context.Context, backend.ConnHandle, string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (*openShouldNotRunBackend) Invoke(context.Context, backend.ConnHandle, string, backend.CallParams, backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (*openShouldNotRunBackend) InvalidateMetadata(string) error { return nil }
