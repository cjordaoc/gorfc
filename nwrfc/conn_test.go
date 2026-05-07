// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"bytes"
	"context"
	"errors"
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

// TestOpen_NoSDK_SurfacesAsSDKUnavailable: against the
// SDK-pending or nosdk backend, Open returns
// *SDKUnavailableError rather than a generic backend error.
func TestOpen_NoSDK_SurfacesAsSDKUnavailable(t *testing.T) {
	_, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err == nil {
		t.Fatal("Open returned nil error")
	}
	if !errors.Is(err, nwrfc.ErrSDKUnavailable) {
		t.Errorf("err=%v want errors.Is ErrSDKUnavailable", err)
	}
	var su *nwrfc.SDKUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("errors.As did not extract *SDKUnavailableError")
	}
	if su.Reason == "" {
		t.Error("SDKUnavailableError.Reason is empty")
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
func (*happyBackend) Reset(backend.ConnHandle) error { return nil }
func (*happyBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (*happyBackend) Invoke(_ context.Context, _ backend.ConnHandle, _ string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return backend.CallParams{}, nil
}
func (*happyBackend) InvalidateMetadata(string) error { return nil }
