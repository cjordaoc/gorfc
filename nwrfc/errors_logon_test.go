// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// fakeSDKError is a small helper to feed sdkErrorToTyped without
// dragging the cgo backend into pure-Go tests.
func fakeSDKError(group uint32, key, msg string) *backend.SDKError {
	return &backend.SDKError{
		Op: "RfcOpenConnection",
		Info: backend.SDKErrorInfo{
			Group:   group,
			Key:     key,
			Message: msg,
		},
	}
}

// TestLogonSubtype_PasswordExpired covers the canonical message
// emitted when a SAP user must reset their password before the
// next logon. errors.Is(err, ErrPasswordExpired) AND
// errors.Is(err, ErrLogon) must both report true so generic
// retry policies and specific UX branching keep working.
func TestLogonSubtype_PasswordExpired(t *testing.T) {
	err := sdkErrorToTyped(fakeSDKError(
		backend.GroupLogonFailure,
		"RFC_PASSWORD_EXPIRED",
		"Password is no longer valid",
	))
	if !errors.Is(err, ErrPasswordExpired) {
		t.Errorf("errors.Is(err, ErrPasswordExpired)=false, want true (err=%v)", err)
	}
	if !errors.Is(err, ErrLogon) {
		t.Errorf("errors.Is(err, ErrLogon)=false, want true")
	}
	var pe *PasswordExpiredError
	if !errors.As(err, &pe) {
		t.Fatalf("errors.As did not extract *PasswordExpiredError")
	}
	// Subtype must also be reachable through the parent type.
	var le *LogonError
	if !errors.As(err, &le) {
		t.Fatalf("errors.As did not extract embedded *LogonError")
	}
}

// TestLogonSubtype_UserLocked exercises the "user locked"
// shape — the SU01 case where retries should not happen.
func TestLogonSubtype_UserLocked(t *testing.T) {
	err := sdkErrorToTyped(fakeSDKError(
		backend.GroupLogonFailure,
		"RFC_USER_LOCKED",
		"User is locked. Please notify the system administrator",
	))
	if !errors.Is(err, ErrUserLocked) || !errors.Is(err, ErrLogon) {
		t.Errorf("errors.Is mismatches: ErrUserLocked=%v ErrLogon=%v",
			errors.Is(err, ErrUserLocked), errors.Is(err, ErrLogon))
	}
	var ul *UserLockedError
	if !errors.As(err, &ul) {
		t.Fatalf("errors.As did not extract *UserLockedError")
	}
}

// TestLogonSubtype_UserLockedFromGenericKey: the SDK frequently
// uses RFC_LOGON_FAILURE for everything; we must still classify
// when the message reveals lockout shape.
func TestLogonSubtype_UserLockedFromGenericKey(t *testing.T) {
	err := sdkErrorToTyped(fakeSDKError(
		backend.GroupLogonFailure,
		"RFC_LOGON_FAILURE",
		"User is locked.",
	))
	var ul *UserLockedError
	if !errors.As(err, &ul) {
		t.Fatalf("classifier missed user-lockout when key is generic")
	}
}

// TestLogonSubtype_InvalidCredentials — the most common case,
// produced by both wrong-username and wrong-password (SAP does
// not distinguish to avoid leaking which one was wrong).
func TestLogonSubtype_InvalidCredentials(t *testing.T) {
	err := sdkErrorToTyped(fakeSDKError(
		backend.GroupLogonFailure,
		"RFC_LOGON_FAILURE",
		"Name or password is incorrect (repeat logon)",
	))
	if !errors.Is(err, ErrInvalidCredentials) || !errors.Is(err, ErrLogon) {
		t.Errorf("errors.Is mismatches: ErrInvalidCredentials=%v ErrLogon=%v",
			errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrLogon))
	}
	var ic *InvalidCredentialsError
	if !errors.As(err, &ic) {
		t.Fatalf("errors.As did not extract *InvalidCredentialsError")
	}
}

// TestLogonSubtype_UnknownDoesNotLeakIntoCommunication: a
// LOGON_FAILURE that the classifier cannot place must surface
// as *UnknownLogonFailureError — NOT *CommunicationError. The
// AGENTS.md non-negotiable on silent fallback applies here.
func TestLogonSubtype_UnknownDoesNotLeakIntoCommunication(t *testing.T) {
	err := sdkErrorToTyped(fakeSDKError(
		backend.GroupLogonFailure,
		"RFC_LOGON_FAILURE",
		"Some never-before-seen reason",
	))
	if !errors.Is(err, ErrUnknownLogonFailure) {
		t.Errorf("did not match ErrUnknownLogonFailure")
	}
	if !errors.Is(err, ErrLogon) {
		t.Errorf("logon umbrella not preserved")
	}
	if errors.Is(err, ErrCommunication) {
		t.Errorf("misclassified as ErrCommunication")
	}
	var ulf *UnknownLogonFailureError
	if !errors.As(err, &ulf) {
		t.Fatalf("errors.As did not extract *UnknownLogonFailureError")
	}
}

// TestLogonSubtype_NotARetryableCategory: every logon subtype is
// a fatal-for-this-credential failure; IsRetryable must report
// false for all four.
func TestLogonSubtype_NotARetryable(t *testing.T) {
	cases := []error{
		&PasswordExpiredError{},
		&UserLockedError{},
		&InvalidCredentialsError{},
		&UnknownLogonFailureError{},
	}
	for _, e := range cases {
		if IsRetryable(e) {
			t.Errorf("%T classified as retryable", e)
		}
	}
}

// TestLogonSubtype_NoSecretLeak: the Error() and LogValue() of
// every subtype must not echo the password / ticket / SNC
// principal. We seed the SDKErrorInfo.Message with a marker and
// check it never reaches stdout.
func TestLogonSubtype_NoSecretLeak(t *testing.T) {
	const secret = "thisIsTheSecret"
	info := SDKErrorInfo{
		Code:    99,
		Key:     "RFC_LOGON_FAILURE",
		Message: "Name or password is incorrect (repeat logon) " + secret,
		// AbapMsgV1..V4 are the noisy ones; the redactor in
		// SDKErrorInfo.LogValue masks them.
		AbapMsgV1: secret,
	}
	cases := []RFCError{
		&PasswordExpiredError{LogonError: LogonError{SDKErrorInfo: info, User: "demo"}},
		&UserLockedError{LogonError: LogonError{SDKErrorInfo: info, User: "demo"}},
		&InvalidCredentialsError{LogonError: LogonError{SDKErrorInfo: info, User: "demo"}},
		&UnknownLogonFailureError{LogonError: LogonError{SDKErrorInfo: info, User: "demo"}},
	}
	for _, e := range cases {
		var buf bytes.Buffer
		log := slog.New(slog.NewJSONHandler(&buf, nil))
		log.Info("logon", "err", e)
		out := buf.String()
		if strings.Contains(out, secret) {
			t.Errorf("%T: log leaked secret %q in: %s", e, secret, out)
		}
		// Subtype indicator must be present so operators can
		// alert on it.
		switch e.(type) {
		case *PasswordExpiredError:
			if !strings.Contains(out, "password_expired") {
				t.Errorf("PasswordExpiredError missing subtype tag in log: %s", out)
			}
		}
	}
}

// TestClassifyLogon_TableMatrix exercises the classifier directly
// with a fixture matrix for stability across SDK versions.
func TestClassifyLogon_TableMatrix(t *testing.T) {
	cases := []struct {
		name string
		info SDKErrorInfo
		want logonSubtype
	}{
		{
			"explicit password expired key",
			SDKErrorInfo{Key: "RFC_PASSWORD_EXPIRED", Message: ""},
			logonPasswordExpired,
		},
		{
			"generic key, password change required",
			SDKErrorInfo{Key: "RFC_LOGON_FAILURE", Message: "Password change required"},
			logonPasswordExpired,
		},
		{
			"user locked explicit",
			SDKErrorInfo{Key: "RFC_USER_LOCKED", Message: ""},
			logonUserLocked,
		},
		{
			"invalid creds",
			SDKErrorInfo{Key: "RFC_LOGON_FAILURE", Message: "Name or password is incorrect"},
			logonInvalidCredentials,
		},
		{
			"unknown logon shape",
			SDKErrorInfo{Key: "RFC_LOGON_FAILURE", Message: "some unrelated message"},
			logonUnknown,
		},
		{
			"empty info",
			SDKErrorInfo{},
			logonUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyLogon(tc.info); got != tc.want {
				t.Errorf("classifyLogon=%d want %d (%+v)", got, tc.want, tc.info)
			}
		})
	}
}

// TestSentinelsDistinct: the four logon sentinels must be
// distinct values; otherwise a single switch arm could match
// multiple subtypes. (Smoke test.)
func TestLogonSubtype_SentinelsDistinct(t *testing.T) {
	all := []error{ErrPasswordExpired, ErrUserLocked, ErrInvalidCredentials, ErrUnknownLogonFailure}
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel %d collides with %d", i, j)
			}
		}
	}
}
