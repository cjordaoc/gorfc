// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfcparam_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcparam"
)

func TestBAPIReturn_TypeClassification(t *testing.T) {
	cases := []struct {
		t      string
		isErr  bool
		isWarn bool
	}{
		{"S", false, false}, {"I", false, false}, {"W", false, true},
		{"E", true, false}, {"A", true, false}, {"X", true, false},
		{"", false, false},
	}
	for _, tc := range cases {
		r := nwrfcparam.BAPIReturn{Type: tc.t}
		if r.IsError() != tc.isErr {
			t.Errorf("IsError(%q)=%v want %v", tc.t, r.IsError(), tc.isErr)
		}
		if r.IsWarning() != tc.isWarn {
			t.Errorf("IsWarning(%q)=%v want %v", tc.t, r.IsWarning(), tc.isWarn)
		}
	}
}

func TestBAPIReturn_LogValueRedactsMsgVars(t *testing.T) {
	r := nwrfcparam.BAPIReturn{
		Type:      "E",
		ID:        "BAPI_USER",
		Number:    "001",
		MessageV1: "secret-account-id",
		MessageV2: "another-secret",
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("ret", "row", r)
	out := buf.String()
	for _, leak := range []string{"secret-account-id", "another-secret"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "BAPI_USER") || !strings.Contains(out, `"number":"001"`) {
		t.Errorf("dropped non-sensitive fields: %s", out)
	}
}

func TestParseBAPIReturn_SingleStructure(t *testing.T) {
	raw := map[string]any{
		"TYPE": "S", "ID": "X", "NUMBER": "001", "MESSAGE": "ok",
	}
	rows, err := nwrfcparam.ParseBAPIReturn(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Type != "S" || rows[0].ID != "X" {
		t.Errorf("rows=%+v", rows)
	}
}

func TestParseBAPIReturn_TableOfMaps(t *testing.T) {
	raw := []map[string]any{
		{"TYPE": "S", "ID": "X"},
		{"TYPE": "E", "ID": "Y", "MESSAGE": "boom"},
	}
	rows, err := nwrfcparam.ParseBAPIReturn(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].Type != "E" {
		t.Errorf("rows=%+v", rows)
	}
}

func TestAsError_SingleError(t *testing.T) {
	rows := []nwrfcparam.BAPIReturn{
		{Type: "S"},
		{Type: "E", ID: "BAPI", Number: "999", Message: "failed"},
	}
	err := nwrfcparam.AsError(rows)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, nwrfc.ErrABAPApplication) {
		t.Errorf("err=%v", err)
	}
	var abap *nwrfc.ABAPApplicationError
	if !errors.As(err, &abap) {
		t.Fatal("could not extract")
	}
	if abap.AbapMsgNumber != "999" {
		t.Errorf("MsgNumber=%q", abap.AbapMsgNumber)
	}
}

func TestAsError_MultipleErrors(t *testing.T) {
	rows := []nwrfcparam.BAPIReturn{
		{Type: "E", ID: "X", Number: "001"},
		{Type: "A", ID: "Y", Number: "002"},
	}
	err := nwrfcparam.AsError(rows)
	if err == nil {
		t.Fatal("expected error")
	}
	// errors.Join propagates per-error matching.
	if !errors.Is(err, nwrfc.ErrABAPApplication) {
		t.Error("did not match ErrABAPApplication")
	}
}

func TestAsError_AllSuccess(t *testing.T) {
	rows := []nwrfcparam.BAPIReturn{{Type: "S"}, {Type: "I"}, {Type: "W"}}
	if err := nwrfcparam.AsError(rows); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCheckRETURN_HappyPath(t *testing.T) {
	// Single RETURN map.
	resp := map[string]any{
		"RETURN": map[string]any{"TYPE": "S", "MESSAGE": "ok"},
	}
	if err := nwrfcparam.CheckRETURN(resp); err != nil {
		t.Errorf("err=%v", err)
	}
	// Failing RETURN.
	resp = map[string]any{
		"RETURN": map[string]any{"TYPE": "E", "ID": "X", "NUMBER": "1", "MESSAGE": "bad"},
	}
	if err := nwrfcparam.CheckRETURN(resp); err == nil {
		t.Error("expected error")
	}
	// No RETURN at all.
	if err := nwrfcparam.CheckRETURN(map[string]any{}); err != nil {
		t.Error("expected nil")
	}
}
