// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// TestErrors_IsCategorySentinels asserts that every concrete
// error type matches its category sentinel via errors.Is.
func TestErrors_IsCategorySentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		sentinel error
		category backend.Category
	}{
		{"LogonError", &LogonError{User: "u", Client: "100", SysID: "DEV"}, ErrLogon, backend.CategoryLogon},
		{"CommunicationError", &CommunicationError{Host: "h", Service: "3300"}, ErrCommunication, backend.CategoryCommunication},
		{"ABAPApplicationError", &ABAPApplicationError{Function: "RFC_PING"}, ErrABAPApplication, backend.CategoryABAPApp},
		{"ABAPRuntimeError", &ABAPRuntimeError{Function: "RFC_PING"}, ErrABAPRuntime, backend.CategoryABAPRuntime},
		{"ABAPClassicException", &ABAPClassicException{Function: "RFC_PING"}, ErrABAPClassic, backend.CategoryABAPClassic},
		{"ABAPClassException", &ABAPClassException{Function: "RFC_PING", ClassName: "CX_ROOT"}, ErrABAPClass, backend.CategoryABAPClass},
		{"ExternalAuthorizationError", &ExternalAuthorizationError{}, ErrExtAuthz, backend.CategoryExtAuthz},
		{"ExternalApplicationError", &ExternalApplicationError{Function: "X"}, ErrExtApp, backend.CategoryExtApp},
		{"ExternalRuntimeError", &ExternalRuntimeError{}, ErrExtRuntime, backend.CategoryExtRuntime},
		{"BrokenConnectionError", &BrokenConnectionError{Reason: "closed"}, ErrBrokenConn, backend.CategoryBrokenConn},
		{"TimeoutError", &TimeoutError{Function: "RFC_PING", Deadline: time.Now()}, ErrTimeout, backend.CategoryTimeout},
		{"CancelledError", &CancelledError{Function: "RFC_PING"}, ErrCancelled, backend.CategoryCancelled},
		{"MarshalError", &MarshalError{FieldName: "X", GoType: "int", ABAPType: "RFCTYPE_DATE", Reason: ErrZeroDate}, ErrMarshal, backend.CategoryMarshal},
		{"ConfigError", &ConfigError{Field: "ashost", Hint: "required"}, ErrConfig, backend.CategoryConfig},
		{"SDKUnavailableError", &SDKUnavailableError{Reason: "no cgo"}, ErrSDKUnavailable, backend.CategorySDKUnavailable},
		{"UnsupportedFeatureError", &UnsupportedFeatureError{Feature: "WebSocketRFC", RequiredVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 10}, CurrentVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 3}}, ErrUnsupported, backend.CategoryUnsupported},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.sentinel) {
				t.Errorf("errors.Is(err, %v)=false, want true", tc.sentinel)
			}
			rfc, ok := tc.err.(RFCError)
			if !ok {
				t.Fatalf("err does not implement RFCError")
			}
			if got := rfc.Category(); got != tc.category {
				t.Errorf("Category()=%v, want %v", got, tc.category)
			}
			if rfc.Error() == "" {
				t.Errorf("Error() returned empty string")
			}
			// Compile-time interface check is in errors.go;
			// runtime check that LogValue produces something.
			lv := rfc.LogValue()
			if lv.Kind() == slog.KindAny && lv.Any() == nil {
				t.Errorf("LogValue returned nil Any")
			}
		})
	}
}

// TestErrors_AsExtractsConcrete ensures errors.As pulls out the
// original concrete type even after wrapping.
func TestErrors_AsExtractsConcrete(t *testing.T) {
	original := &LogonError{
		SDKErrorInfo: SDKErrorInfo{
			Code:    99,
			Key:     "RFC_LOGON_FAILURE",
			Message: "Name or password is incorrect (repeat logon)",
		},
		User: "demo", Client: "100", SysID: "DEV",
	}
	// Wrap several times.
	wrapped := errors.Join(errors.New("outer"), original)

	var le *LogonError
	if !errors.As(wrapped, &le) {
		t.Fatalf("errors.As did not find *LogonError in joined chain")
	}
	if le.User != "demo" || le.Client != "100" || le.SysID != "DEV" {
		t.Errorf("extracted struct fields not preserved: %+v", le)
	}
	if le.SDKErrorInfo.Code != 99 || le.SDKErrorInfo.Key != "RFC_LOGON_FAILURE" {
		t.Errorf("extracted SDKErrorInfo not preserved: %+v", le.SDKErrorInfo)
	}
}

// TestErrors_LogValueRedacts asserts that none of the SDK-mapped
// errors emits secret-shaped content via slog. Particularly:
// AbapMsgV1..V4 are redacted, and ExternalAuthorizationError
// fully redacts the SDK message.
func TestErrors_LogValueRedacts(t *testing.T) {
	const secret = "thisIsTheSecretValue"
	cases := []struct {
		name       string
		err        RFCError
		mustNotSee []string
		mustSee    []string
	}{
		{
			"ABAPApplicationError preserves keys, redacts V1..V4",
			&ABAPApplicationError{
				SDKErrorInfo: SDKErrorInfo{
					Code:          1,
					Key:           "RFC_ABAP_MESSAGE",
					Message:       "msg-shaped",
					AbapMsgClass:  "SR",
					AbapMsgType:   "E",
					AbapMsgNumber: "001",
					AbapMsgV1:     secret,
					AbapMsgV2:     secret + "-2",
				},
				Function: "RFC_PING",
			},
			[]string{secret},
			[]string{"RFC_ABAP_MESSAGE", "SR", "abap_msg_v1", "abap_msg_v2"},
		},
		{
			"ExternalAuthorizationError redacts the full message",
			&ExternalAuthorizationError{
				SDKErrorInfo: SDKErrorInfo{
					Code:    1,
					Key:     "RFC_EXTERNAL_AUTHORIZATION_FAILURE",
					Message: "rejected ticket: " + secret,
				},
			},
			[]string{secret, "rejected ticket"},
			[]string{"RFC_EXTERNAL_AUTHORIZATION_FAILURE"},
		},
		{
			"ConfigError on sensitive field redacts the hint",
			&ConfigError{Field: "passwd", Hint: secret},
			[]string{secret},
			[]string{"passwd", "«redacted»"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, nil))
			log.Info("err", "e", tc.err)
			out := buf.String()
			for _, leak := range tc.mustNotSee {
				if strings.Contains(out, leak) {
					t.Errorf("log leaked %q in: %s", leak, out)
				}
			}
			for _, ok := range tc.mustSee {
				if !strings.Contains(out, ok) {
					t.Errorf("log missing %q in: %s", ok, out)
				}
			}
		})
	}
}

// TestErrors_TimeoutImpliesCancelled — a TimeoutError matches
// both ErrTimeout and ErrCancelled (a deadline-elapsed is a
// special case of ctx cancellation).
func TestErrors_TimeoutImpliesCancelled(t *testing.T) {
	te := &TimeoutError{Function: "RFC_PING", Deadline: time.Now()}
	if !errors.Is(te, ErrTimeout) {
		t.Error("TimeoutError did not match ErrTimeout")
	}
	if !errors.Is(te, ErrCancelled) {
		t.Error("TimeoutError did not also match ErrCancelled")
	}
}

// TestErrors_BrokenConnUnwrapsCause — wrapping a CommErr inside
// a BrokenConnectionError lets callers branch on either.
func TestErrors_BrokenConnUnwrapsCause(t *testing.T) {
	cause := &CommunicationError{Host: "h"}
	bc := &BrokenConnectionError{Reason: "post-error", Cause: cause}
	if !errors.Is(bc, ErrBrokenConn) {
		t.Error("did not match ErrBrokenConn")
	}
	if !errors.Is(bc, ErrCommunication) {
		t.Error("did not match wrapped ErrCommunication")
	}
	var ce *CommunicationError
	if !errors.As(bc, &ce) {
		t.Error("errors.As did not extract wrapped *CommunicationError")
	}
}

// TestErrors_IsRetryable — comm/broken/timeout retryable;
// logon/marshal/config not.
func TestErrors_IsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&CommunicationError{}, true},
		{&BrokenConnectionError{Reason: "x"}, true},
		{&TimeoutError{}, true},
		{&LogonError{}, false},
		{&MarshalError{}, false},
		{&ConfigError{}, false},
		{&ABAPApplicationError{}, false},
		{nil, false},
	}
	for _, tc := range cases {
		got := IsRetryable(tc.err)
		if got != tc.want {
			t.Errorf("IsRetryable(%T)=%v want %v", tc.err, got, tc.want)
		}
	}
}

// TestErrors_CategoryOf — walks the wrapped chain.
func TestErrors_CategoryOf(t *testing.T) {
	logon := &LogonError{}
	wrapped := errors.Join(errors.New("ctx"), logon)
	if got := CategoryOf(wrapped); got != backend.CategoryLogon {
		t.Errorf("CategoryOf(joined)=%v want %v", got, backend.CategoryLogon)
	}
	if got := CategoryOf(errors.New("plain")); got != backend.CategoryUnknown {
		t.Errorf("CategoryOf(plain)=%v want CategoryUnknown", got)
	}
	if got := CategoryOf(nil); got != backend.CategoryUnknown {
		t.Errorf("CategoryOf(nil)=%v want CategoryUnknown", got)
	}
}

// TestErrors_MarshalErrUnwrapsZeroDate — domain sentinel
// composes with the typed error.
func TestErrors_MarshalErrUnwrapsZeroDate(t *testing.T) {
	me := &MarshalError{
		FieldName: "RFCDATE",
		GoType:    "time.Time",
		ABAPType:  "RFCTYPE_DATE",
		Reason:    ErrZeroDate,
	}
	if !errors.Is(me, ErrMarshal) {
		t.Error("did not match ErrMarshal")
	}
	if !errors.Is(me, ErrZeroDate) {
		t.Error("did not unwrap to ErrZeroDate")
	}
}

// TestErrors_SDKUnavailableMatchesBackendErr — the public sentinel
// chains to the internal one so callers can branch on either.
func TestErrors_SDKUnavailableMatchesBackendErr(t *testing.T) {
	su := &SDKUnavailableError{Reason: "test"}
	if !errors.Is(su, ErrSDKUnavailable) {
		t.Error("did not match ErrSDKUnavailable")
	}
	if !errors.Is(su, backend.ErrUnavailable) {
		t.Error("did not chain to backend.ErrUnavailable")
	}
}

// TestErrors_UnsupportedReportsBothVersions — the message
// names the required vs current SDK release.
func TestErrors_UnsupportedReportsBothVersions(t *testing.T) {
	uf := &UnsupportedFeatureError{
		Feature:         "WebSocketRFC",
		RequiredVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 10},
		CurrentVersion:  backend.Version{Major: 7, Minor: 50, PatchLevel: 3},
	}
	msg := uf.Error()
	for _, want := range []string{"WebSocketRFC", "7.50 PL10", "7.50 PL3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error()=%q missing %q", msg, want)
		}
	}
}
