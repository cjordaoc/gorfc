// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Logon-failure subtypes.
//
// Both PyRFC and node-rfc converge on the same operational
// breakdown: a logon failure is most useful to the caller when
// the client knows whether to re-prompt for a password
// (`InvalidCredentials`), surface a "your password expired,
// reset it in SU01" UX (`PasswordExpired`), open a ticket with
// IT (`UserLocked`), or retry against a different node
// (`UnknownLogonFailure`). The SDK reports the same shape via
// `RFC_ERROR_INFO.key` / `.message`; we map them to typed Go
// values here so callers never string-match in business code.
//
// Every subtype:
//
//   - embeds [LogonError] so `errors.As(err, &le *LogonError)`
//     keeps working — old call sites continue to compile.
//   - matches [ErrLogon] via [errors.Is].
//   - is also extractable as its specific type via
//     [errors.As], so a controller can branch on
//     PasswordExpired only.
//
// The classification table is the single source of truth
// (`logonClassifications`) and lives next to the types; the
// dispatch helper `classifyLogon` is the only function that
// reads it.

// PasswordExpiredError narrows [LogonError] to the case where
// the SAP system signalled the password is no longer valid for
// new logons. Common keys / messages: `RFC_LOGON_FAILURE` with
// "Password is no longer valid", `RFC_PASSWORD_EXPIRED`.
//
// Operational meaning: the user must reset the password
// (transaction SU01 or self-service). Re-prompting for the same
// password will not succeed.
type PasswordExpiredError struct {
	LogonError
}

// UserLockedError narrows [LogonError] to user-lockout. Common
// shapes: `RFC_LOGON_FAILURE` with "User is locked", direct
// `RFC_USER_LOCKED`. Operational meaning: an admin must unlock
// (SU01); retries will not help and may extend the lockout.
type UserLockedError struct {
	LogonError
}

// InvalidCredentialsError narrows [LogonError] to "wrong user
// or password". Common shape: `RFC_LOGON_FAILURE` with "Name
// or password is incorrect" (the SAP system intentionally does
// not distinguish to avoid leaking which one was wrong).
//
// Operational meaning: re-prompt the human, do NOT log the
// password.
type InvalidCredentialsError struct {
	LogonError
}

// UnknownLogonFailureError is the catch-all for logon failures
// the classifier could not assign to one of the specific
// shapes. CRITICALLY this stays inside the logon category —
// fallback to `CommunicationError` would mislead retry policy.
type UnknownLogonFailureError struct {
	LogonError
}

// Compile-time interface assertions.
var (
	_ RFCError = (*PasswordExpiredError)(nil)
	_ RFCError = (*UserLockedError)(nil)
	_ RFCError = (*InvalidCredentialsError)(nil)
	_ RFCError = (*UnknownLogonFailureError)(nil)
)

// Domain sentinels for the four subtypes. They unwrap into
// [ErrLogon] so existing callers that branch on
// `errors.Is(err, ErrLogon)` keep working.
var (
	// ErrPasswordExpired matches *PasswordExpiredError via
	// [errors.Is]. Use to drive "your password expired" UX.
	ErrPasswordExpired = errors.New("nwrfc: SAP password expired")

	// ErrUserLocked matches *UserLockedError via [errors.Is].
	// Use to escalate to SAP admins (the user is in SU01 lock
	// state).
	ErrUserLocked = errors.New("nwrfc: SAP user locked")

	// ErrInvalidCredentials matches *InvalidCredentialsError
	// via [errors.Is]. Use to re-prompt the human.
	ErrInvalidCredentials = errors.New("nwrfc: invalid SAP credentials")

	// ErrUnknownLogonFailure matches *UnknownLogonFailureError
	// via [errors.Is]. The SDK reported logon failure but did
	// not match any specific shape; treat as fatal for the
	// current credential and escalate.
	ErrUnknownLogonFailure = errors.New("nwrfc: unclassified SAP logon failure")
)

// PasswordExpiredError methods. Inheriting from LogonError
// preserves Category / SDKErrorInfo; we override the
// behavioural ones so the subtype identity wins under
// errors.Is / errors.As.
func (e *PasswordExpiredError) Unwrap() error {
	// Joining the subtype sentinel with the parent sentinel
	// keeps both errors.Is(ErrPasswordExpired) and
	// errors.Is(ErrLogon) true without requiring callers to
	// consult two methods.
	return errors.Join(ErrPasswordExpired, ErrLogon)
}
func (e *PasswordExpiredError) Is(target error) bool {
	return target == ErrPasswordExpired || target == ErrLogon
}
func (e *PasswordExpiredError) As(target any) bool {
	if t, ok := target.(**PasswordExpiredError); ok {
		*t = e
		return true
	}
	if t, ok := target.(**LogonError); ok {
		*t = &e.LogonError
		return true
	}
	return false
}
func (e *PasswordExpiredError) Error() string {
	return fmt.Sprintf("nwrfc: SAP password expired (user=%s, client=%s, sysid=%s, key=%s)",
		e.User, e.Client, e.SysID, e.SDKErrorInfo.Key)
}
func (e *PasswordExpiredError) LogValue() slog.Value {
	return logonSubtypeLogValue("password_expired", &e.LogonError)
}

// UserLockedError methods.
func (e *UserLockedError) Unwrap() error { return errors.Join(ErrUserLocked, ErrLogon) }
func (e *UserLockedError) Is(target error) bool {
	return target == ErrUserLocked || target == ErrLogon
}
func (e *UserLockedError) As(target any) bool {
	if t, ok := target.(**UserLockedError); ok {
		*t = e
		return true
	}
	if t, ok := target.(**LogonError); ok {
		*t = &e.LogonError
		return true
	}
	return false
}
func (e *UserLockedError) Error() string {
	return fmt.Sprintf("nwrfc: SAP user locked (user=%s, client=%s, sysid=%s, key=%s)",
		e.User, e.Client, e.SysID, e.SDKErrorInfo.Key)
}
func (e *UserLockedError) LogValue() slog.Value {
	return logonSubtypeLogValue("user_locked", &e.LogonError)
}

// InvalidCredentialsError methods.
func (e *InvalidCredentialsError) Unwrap() error {
	return errors.Join(ErrInvalidCredentials, ErrLogon)
}
func (e *InvalidCredentialsError) Is(target error) bool {
	return target == ErrInvalidCredentials || target == ErrLogon
}
func (e *InvalidCredentialsError) As(target any) bool {
	if t, ok := target.(**InvalidCredentialsError); ok {
		*t = e
		return true
	}
	if t, ok := target.(**LogonError); ok {
		*t = &e.LogonError
		return true
	}
	return false
}
func (e *InvalidCredentialsError) Error() string {
	return fmt.Sprintf("nwrfc: invalid SAP credentials (user=%s, client=%s, sysid=%s, key=%s)",
		e.User, e.Client, e.SysID, e.SDKErrorInfo.Key)
}
func (e *InvalidCredentialsError) LogValue() slog.Value {
	return logonSubtypeLogValue("invalid_credentials", &e.LogonError)
}

// UnknownLogonFailureError methods.
func (e *UnknownLogonFailureError) Unwrap() error {
	return errors.Join(ErrUnknownLogonFailure, ErrLogon)
}
func (e *UnknownLogonFailureError) Is(target error) bool {
	return target == ErrUnknownLogonFailure || target == ErrLogon
}
func (e *UnknownLogonFailureError) As(target any) bool {
	if t, ok := target.(**UnknownLogonFailureError); ok {
		*t = e
		return true
	}
	if t, ok := target.(**LogonError); ok {
		*t = &e.LogonError
		return true
	}
	return false
}
func (e *UnknownLogonFailureError) Error() string {
	return fmt.Sprintf("nwrfc: unclassified SAP logon failure (user=%s, client=%s, sysid=%s, key=%s, msg=%s)",
		e.User, e.Client, e.SysID, e.SDKErrorInfo.Key, e.SDKErrorInfo.Message)
}
func (e *UnknownLogonFailureError) LogValue() slog.Value {
	return logonSubtypeLogValue("unknown_logon_failure", &e.LogonError)
}

// logonSubtypeLogValue is the shared LogValue formatter for the
// four subtypes. Keeping it here avoids per-subtype duplication
// (DRY) while still giving operators a stable `subtype` field
// they can group/aggregate on.
//
// We deliberately do NOT pass the full SDKErrorInfo through:
// some SDK builds echo the rejected ticket / SAML assertion
// inside `RFC_ERROR_INFO.message` when the logon failed via
// external authz callbacks. The fields below are the safe
// projection — code, key, ABAP message class/type/number — and
// match the parent LogonError's redaction story.
func logonSubtypeLogValue(subtype string, le *LogonError) slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryLogon.String()),
		slog.String("subtype", subtype),
		slog.Any("info", redactedSDKErrorInfo(le.SDKErrorInfo).LogValue()),
		slog.String("user", le.User),
		slog.String("client", le.Client),
		slog.String("sysid", le.SysID),
	)
}

// redactedSDKErrorInfo returns a copy of info with the free-form
// `Message` field replaced by [backend.RedactedPlaceholder]. The
// SDK message for logon failures is observed in the wild to
// include rejected tokens, ticket payloads, and SNC principal
// names that operators do not want broadcast.
//
// `Code`, `Group`, `Key`, ABAP message class/type/number
// continue to be emitted — they identify the failure shape
// without echoing user input.
func redactedSDKErrorInfo(info SDKErrorInfo) SDKErrorInfo {
	if info.Message != "" {
		info.Message = backend.RedactedPlaceholder
	}
	return info
}

// =============================================================
// Classification table (one place; consult by classifyLogon)
// =============================================================

// logonSubtype enumerates the kinds of logon failures we expose
// as their own typed error. The ordering reflects classifier
// priority: more specific shapes first.
type logonSubtype uint8

const (
	logonUnknown logonSubtype = iota
	logonPasswordExpired
	logonUserLocked
	logonInvalidCredentials
)

// logonRule is one row of the classification table. A row
// matches when EITHER the SDK key is in the row's `strongKeys`
// (no message inspection needed; the key alone identifies the
// subtype) OR the SDK message contains one of the row's
// `messagePhrases`. The two arms compose with OR so that
// either a clean key or a recognized phrase is enough; both
// arms list the substring we observed in the wild (lower-
// cased).
//
// Rule sources:
//
//   - SAP NetWeaver RFC SDK Programming Guide §7.4 — "Common
//     RFC error keys returned via RFC_ERROR_INFO.key on logon
//     failure".
//   - PyRFC `_cyrfc.pyx` (`RfcLogonError` derivation block).
//   - node-rfc `lib/wrapper/noderfc-bindings.h` shape hints.
//   - SAP note 1497993 (password expiry RFC behavior).
//
// Phrases come from the actual messages emitted by AS ABAP
// 7.50+ in EN locale (verified against SU01 short-text). Match
// is case-insensitive substring; we deliberately keep the
// substrings short to remain robust across SAP minor versions.
type logonRule struct {
	subtype        logonSubtype
	strongKeys     []string
	messagePhrases []string
}

// logonClassifications is the table consulted by classifyLogon.
// Add a row here to extend the taxonomy; do NOT scatter rules
// into the dispatch site.
//
// Order matters: the first match wins, so the more-specific
// shapes precede the more-general ones. (Today there is no
// ambiguity, but the contract is documented.)
var logonClassifications = []logonRule{
	{
		subtype: logonPasswordExpired,
		strongKeys: []string{
			"rfc_password_expired",
			"abap_password_expired",
		},
		messagePhrases: []string{
			"password is no longer valid",
			"password has expired",
			"password change required",
			"please change your password",
			"password is expired",
		},
	},
	{
		subtype: logonUserLocked,
		strongKeys: []string{
			"rfc_user_locked",
			"abap_user_locked",
		},
		messagePhrases: []string{
			"user is locked",
			"user has been locked",
			"is locked due to incorrect logon",
		},
	},
	{
		subtype: logonInvalidCredentials,
		strongKeys: []string{
			"rfc_invalid_credentials",
		},
		messagePhrases: []string{
			"name or password is incorrect",
			"name or password incorrect",
			"incorrect user name or password",
			"please re-enter your password",
			"unknown user",
		},
	},
}

// classifyLogon dispatches an SDKErrorInfo from
// `RFC_LOGON_FAILURE` into the matching logonSubtype. Returns
// `logonUnknown` when no row matches; the caller surfaces an
// `*UnknownLogonFailureError` in that case (NOT a comm error,
// per the AGENTS.md non-negotiable on silent fallback).
//
// Cheap function: O(rows × phrases), all lowercase string
// comparisons. Called once per logon failure, never on the hot
// path.
func classifyLogon(info SDKErrorInfo) logonSubtype {
	keyLower := strings.ToLower(strings.TrimSpace(info.Key))
	msgLower := strings.ToLower(info.Message)

	for _, rule := range logonClassifications {
		if matchLogonRule(rule, keyLower, msgLower) {
			return rule.subtype
		}
	}
	return logonUnknown
}

// matchLogonRule applies one rule to a (keyLower, msgLower)
// pair. EITHER a strong-key hit OR a message-phrase hit is
// enough — they compose with OR. Extracted for testability.
func matchLogonRule(rule logonRule, keyLower, msgLower string) bool {
	for _, k := range rule.strongKeys {
		if k != "" && keyLower == k {
			return true
		}
	}
	for _, sub := range rule.messagePhrases {
		if sub != "" && strings.Contains(msgLower, sub) {
			return true
		}
	}
	return false
}

// buildLogonError applies the classifier to construct the right
// typed error. Called from `sdkErrorToTyped`.
//
// The base [LogonError] is always populated; only the wrapping
// type changes. This keeps the error chain rich for callers who
// want generic logon handling AND callers who want to react to
// a specific subtype.
func buildLogonError(info SDKErrorInfo, attrs LogonErrorContext) error {
	base := LogonError{
		SDKErrorInfo: info,
		User:         attrs.User,
		Client:       attrs.Client,
		SysID:        attrs.SysID,
	}
	switch classifyLogon(info) {
	case logonPasswordExpired:
		return &PasswordExpiredError{LogonError: base}
	case logonUserLocked:
		return &UserLockedError{LogonError: base}
	case logonInvalidCredentials:
		return &InvalidCredentialsError{LogonError: base}
	default:
		return &UnknownLogonFailureError{LogonError: base}
	}
}

// LogonErrorContext bundles the optional Conn-attribute fields
// that the classifier copies into the typed error. Used to
// avoid a long parameter list at the call site.
type LogonErrorContext struct {
	User   string
	Client string
	SysID  string
}
