// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// echoBackend records the inputs to Invoke and returns a
// configurable response. Lets us test the marshal/unmarshal
// round trip without an SDK.
type echoBackend struct {
	lastFn  string
	lastIn  backend.CallParams
	lastOpt backend.InvokeOptions

	resp backend.CallParams
	err  error
}

func (b *echoBackend) Name() string                       { return "echo" }
func (b *echoBackend) Version() backend.Version           { return backend.Version{} }
func (b *echoBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (b *echoBackend) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 1, nil
}
func (b *echoBackend) Close(backend.ConnHandle) error                     { return nil }
func (b *echoBackend) Ping(_ context.Context, _ backend.ConnHandle) error { return nil }
func (b *echoBackend) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, nil
}
func (b *echoBackend) Reset(context.Context, backend.ConnHandle) error { return nil }
func (b *echoBackend) Describe(_ context.Context, _ backend.ConnHandle, _ string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, nil
}
func (b *echoBackend) Invoke(_ context.Context, _ backend.ConnHandle, fn string, in backend.CallParams, opts backend.InvokeOptions) (backend.CallParams, error) {
	b.lastFn = fn
	b.lastIn = in
	b.lastOpt = opts
	if b.err != nil {
		return nil, b.err
	}
	if b.resp == nil {
		return backend.CallParams{}, nil
	}
	return b.resp, nil
}
func (b *echoBackend) InvalidateMetadata(string) error { return nil }

func newEchoConn(t *testing.T, b *echoBackend) *nwrfc.Conn {
	t.Helper()
	prev := backend.SetTesting(b)
	t.Cleanup(prev)
	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCall_StructInOut(t *testing.T) {
	type In struct {
		ReqText string `rfc:"REQUTEXT"`
	}
	type Out struct {
		EchoText string `rfc:"ECHOTEXT"`
		RespText string `rfc:"RESPTEXT"`
	}

	b := &echoBackend{
		resp: backend.CallParams{
			"ECHOTEXT": "ping",
			"RESPTEXT": "pong",
		},
	}
	c := newEchoConn(t, b)

	var out Out
	resp, err := nwrfc.Call(context.Background(), c, "STFC_CONNECTION", In{ReqText: "ping"}, &out)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if b.lastFn != "STFC_CONNECTION" {
		t.Errorf("lastFn=%q want STFC_CONNECTION", b.lastFn)
	}
	if b.lastIn["REQUTEXT"] != "ping" {
		t.Errorf("REQUTEXT=%v want ping", b.lastIn["REQUTEXT"])
	}
	if out.EchoText != "ping" {
		t.Errorf("EchoText=%q want %q", out.EchoText, "ping")
	}
	if out.RespText != "pong" {
		t.Errorf("RespText=%q want %q", out.RespText, "pong")
	}
	if resp["RESPTEXT"] != "pong" {
		t.Errorf("resp[RESPTEXT]=%v want pong", resp["RESPTEXT"])
	}
}

func TestCall_OmitEmpty(t *testing.T) {
	type In struct {
		Required string `rfc:"REQ"`
		Optional string `rfc:"OPT,omitempty"`
	}
	b := &echoBackend{}
	c := newEchoConn(t, b)

	if _, err := nwrfc.Call(context.Background(), c, "F", In{Required: "x"}, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, ok := b.lastIn["OPT"]; ok {
		t.Errorf("empty OPT was sent: %v", b.lastIn)
	}

	if _, err := nwrfc.Call(context.Background(), c, "F", In{Required: "x", Optional: "y"}, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if b.lastIn["OPT"] != "y" {
		t.Errorf("OPT=%v want y", b.lastIn["OPT"])
	}
}

func TestCall_MapIn(t *testing.T) {
	b := &echoBackend{
		resp: backend.CallParams{"OUT": "value"},
	}
	c := newEchoConn(t, b)

	resp, err := nwrfc.Call(context.Background(), c, "F",
		backend.CallParams{"IN": "x"}, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if b.lastIn["IN"] != "x" {
		t.Errorf("dynamic input not preserved: %v", b.lastIn)
	}
	if resp["OUT"] != "value" {
		t.Errorf("resp[OUT]=%v", resp["OUT"])
	}
}

func TestCall_NestedStructure(t *testing.T) {
	type Inner struct {
		Field1 string `rfc:"F1"`
		Field2 int    `rfc:"F2"`
	}
	type In struct {
		Header Inner `rfc:"HEADER"`
	}
	type Out struct {
		Header Inner `rfc:"HEADER"`
	}

	b := &echoBackend{
		resp: backend.CallParams{
			"HEADER": map[string]any{"F1": "echo", "F2": int64(42)},
		},
	}
	c := newEchoConn(t, b)

	var out Out
	_, err := nwrfc.Call(context.Background(), c, "F",
		In{Header: Inner{Field1: "in", Field2: 7}}, &out)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	hdr, ok := b.lastIn["HEADER"].(map[string]any)
	if !ok {
		t.Fatalf("HEADER input not a map: %T", b.lastIn["HEADER"])
	}
	if hdr["F1"] != "in" || hdr["F2"] != 7 {
		t.Errorf("HEADER = %v", hdr)
	}
	if out.Header.Field1 != "echo" || out.Header.Field2 != 42 {
		t.Errorf("Header = %+v", out.Header)
	}
}

func TestCall_BackendError_Passthrough(t *testing.T) {
	want := &nwrfc.LogonError{User: "demo"}
	b := &echoBackend{err: want}
	c := newEchoConn(t, b)

	_, err := nwrfc.Call(context.Background(), c, "F", backend.CallParams{}, nil)
	if !errors.Is(err, nwrfc.ErrLogon) {
		t.Errorf("err=%v want ErrLogon", err)
	}
	var le *nwrfc.LogonError
	if !errors.As(err, &le) {
		t.Fatalf("errors.As did not extract")
	}
	if le.User != "demo" {
		t.Errorf("preserved field lost: %+v", le)
	}
}

func TestCall_TagDashSkipped(t *testing.T) {
	type In struct {
		Visible string `rfc:"VIS"`
		Hidden  string `rfc:"-"`
	}
	b := &echoBackend{}
	c := newEchoConn(t, b)
	_, err := nwrfc.Call(context.Background(), c, "F",
		In{Visible: "v", Hidden: "h"}, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, ok := b.lastIn["HIDDEN"]; ok {
		t.Error("HIDDEN was sent despite -")
	}
	if b.lastIn["VIS"] != "v" {
		t.Error("VIS not sent")
	}
}

func TestCall_TagFallbackToFieldName(t *testing.T) {
	type In struct {
		REQUTEXT string // no tag → field name upper-cased
	}
	b := &echoBackend{}
	c := newEchoConn(t, b)
	_, err := nwrfc.Call(context.Background(), c, "F", In{REQUTEXT: "x"}, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if b.lastIn["REQUTEXT"] != "x" {
		t.Errorf("derived name failed: %v", b.lastIn)
	}
}

func TestCall_NilConn(t *testing.T) {
	_, err := nwrfc.Call(context.Background(), nil, "F", nil, nil)
	if !errors.Is(err, nwrfc.ErrBrokenConn) {
		t.Errorf("err=%v want ErrBrokenConn", err)
	}
}

func TestCall_ClosedConn(t *testing.T) {
	b := &echoBackend{}
	c := newEchoConn(t, b)
	_ = c.Close()
	_, err := nwrfc.Call(context.Background(), c, "F", nil, nil)
	if !errors.Is(err, nwrfc.ErrBrokenConn) {
		t.Errorf("err=%v want ErrBrokenConn", err)
	}
}
