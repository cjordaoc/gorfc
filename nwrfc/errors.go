// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// The error taxonomy defined here implements docs/PLAN.md §7. It
// has three goals:
//
//  1. **Coverage** — every failure mode the SAP NetWeaver RFC SDK
//     can report becomes a typed Go error. Callers branch on
//     category (sentinels) or extract structured fields
//     (errors.As).
//  2. **Idiomatic Go** — wrapping via Unwrap, sentinel matching
//     via Is, and structured payloads via [errors.As]. Joined
//     errors via [errors.Join] for cleanup paths that emit
//     multiple errors.
//  3. **Redaction by default** — every type implements
//     [slog.LogValuer] with sensitive fields replaced by
//     "«redacted»". Logging an error never leaks credentials,
//     SAP system identity (when configured to redact), or
//     ABAP business payload at default severity.
//
// The taxonomy is composed of:
//
//   - One root interface: [RFCError].
//   - Eight SDK-mapped struct types corresponding to the
//     RFC_ERROR_INFO.group enum (Logon, Communication,
//     ABAPApplication, ABAPRuntime, ABAPClassic, ABAPClass,
//     ExternalAuthorization, ExternalApplication, ExternalRuntime).
//   - Five Go-side struct types for failures that the Go layer
//     decides about (Broken, Timeout, Cancelled, Marshal, Config).
//   - Two operational types (SDKUnavailable, UnsupportedFeature).
//   - Sentinel error values for each category, used with
//     [errors.Is].

// RFCError is implemented by every error this package returns.
// Match against a category via [errors.Is] sentinels, or extract
// the concrete type via [errors.As].
type RFCError interface {
	error
	// Category returns the broad classification. Stable across
	// versions; suitable for log/span attributes.
	Category() backend.Category
	// LogValue implements [slog.LogValuer] with redaction; never
	// returns sensitive material at default severity.
	LogValue() slog.Value
}

// Sentinel errors for use with [errors.Is]. Each concrete error
// type returns true from Is when the target is its corresponding
// sentinel. The sentinels themselves carry no state and are
// safe to compare with == as well.
var (
	ErrLogon           = sentinel{cat: backend.CategoryLogon, msg: "RFC logon failed"}
	ErrCommunication   = sentinel{cat: backend.CategoryCommunication, msg: "RFC communication failure"}
	ErrABAPApplication = sentinel{cat: backend.CategoryABAPApp, msg: "ABAP application error"}
	ErrABAPRuntime     = sentinel{cat: backend.CategoryABAPRuntime, msg: "ABAP runtime error"}
	ErrABAPClassic     = sentinel{cat: backend.CategoryABAPClassic, msg: "ABAP classic exception"}
	ErrABAPClass       = sentinel{cat: backend.CategoryABAPClass, msg: "ABAP class-based exception"}
	ErrExtAuthz        = sentinel{cat: backend.CategoryExtAuthz, msg: "external authorization failure"}
	ErrExtApp          = sentinel{cat: backend.CategoryExtApp, msg: "external application failure"}
	ErrExtRuntime      = sentinel{cat: backend.CategoryExtRuntime, msg: "external runtime failure"}
	ErrBrokenConn      = sentinel{cat: backend.CategoryBrokenConn, msg: "RFC connection is broken"}
	ErrTimeout         = sentinel{cat: backend.CategoryTimeout, msg: "RFC call timed out"}
	ErrCancelled       = sentinel{cat: backend.CategoryCancelled, msg: "RFC call cancelled"}
	ErrMarshal         = sentinel{cat: backend.CategoryMarshal, msg: "RFC marshal/unmarshal failed"}
	ErrConfig          = sentinel{cat: backend.CategoryConfig, msg: "RFC config invalid"}
	ErrSDKUnavailable  = sentinel{cat: backend.CategorySDKUnavailable, msg: "SAP NetWeaver RFC SDK is not available"}
	ErrUnsupported     = sentinel{cat: backend.CategoryUnsupported, msg: "feature not supported by this SDK version"}
)

// Domain-specific sentinels (not full categories — they refine
// existing categories with stable identity for [errors.Is]).
var (
	// ErrZeroDate is wrapped by *MarshalError when the SDK
	// returns the ABAP "00000000" initial date and the caller
	// did not opt into [InvokeOptions.AllowZeroDate]. The
	// previous upstream behavior (silently returning zero
	// time.Time) violates AGENTS.md "no silent fallback"; this
	// sentinel is the explicit replacement.
	ErrZeroDate = errors.New("nwrfc: ABAP initial date 00000000 (use AllowZeroDate to accept)")

	// ErrZeroTime mirrors [ErrZeroDate] for ABAP initial time
	// "000000".
	ErrZeroTime = errors.New("nwrfc: ABAP initial time 000000 (use AllowZeroTime to accept)")

	// ErrUnknownType is wrapped by *MarshalError when the SDK
	// reports an RFCTYPE the marshaling layer does not handle.
	// Covers types added in future SDK releases that this
	// version of the library has not been updated for.
	// Aliases backend.ErrUnknownType so cgo bindings (which
	// must avoid an import cycle with this package) can wrap
	// the same identity.
	ErrUnknownType = backend.ErrUnknownType

	// ErrConnClosed is wrapped by *BrokenConnectionError when a
	// caller uses a Conn after Close. The Conn keeps a state
	// flag rather than relying on the SDK to detect the misuse.
	ErrConnClosed = errors.New("nwrfc: connection is closed")
)

// sentinel is the type backing every Err* package value. It is
// not exposed; callers compare via [errors.Is] or use the
// package-level sentinels directly.
type sentinel struct {
	cat backend.Category
	msg string
}

func (s sentinel) Error() string              { return s.msg }
func (s sentinel) Category() backend.Category { return s.cat }
func (s sentinel) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", s.cat.String()),
		slog.String("message", s.msg),
	)
}

// SDKErrorInfo carries the structured payload extracted from
// `RFC_ERROR_INFO`. It is embedded in every SDK-mapped error
// type below, so callers can reach the raw fields:
//
//	var commErr *nwrfc.CommunicationError
//	if errors.As(err, &commErr) {
//	    fmt.Println(commErr.SDKErrorInfo.Code, commErr.SDKErrorInfo.Key)
//	}
//
// Field names match the SAP RFC_ERROR_INFO struct members
// (camelCased Go-side). The decoded SAP_UC strings are UTF-8.
type SDKErrorInfo struct {
	Code          int    // RFC_RC numeric code
	Group         uint32 // RFC_ERROR_GROUP enum value
	Key           string // SDK error key (e.g. "RFC_LOGON_FAILURE")
	Message       string // Human-readable message; may be in user's language
	AbapMsgClass  string
	AbapMsgType   string // 'A','E','I','S','W','X'
	AbapMsgNumber string
	AbapMsgV1     string
	AbapMsgV2     string
	AbapMsgV3     string
	AbapMsgV4     string
}

// LogValue redacts ABAP message variables that frequently carry
// business data. Code/Group/Key/Message/MsgClass/MsgType/MsgNumber
// are emitted; V1..V4 are summarized as "«len(N)»" so log
// readers see *something* without the raw payload leaking.
func (s SDKErrorInfo) LogValue() slog.Value {
	const v = "«redacted»"
	attrs := []slog.Attr{
		slog.Int("code", s.Code),
		slog.Uint64("group", uint64(s.Group)),
		slog.String("key", s.Key),
		slog.String("message", s.Message),
	}
	if s.AbapMsgClass != "" {
		attrs = append(attrs, slog.String("abap_msg_class", s.AbapMsgClass))
	}
	if s.AbapMsgType != "" {
		attrs = append(attrs, slog.String("abap_msg_type", s.AbapMsgType))
	}
	if s.AbapMsgNumber != "" {
		attrs = append(attrs, slog.String("abap_msg_number", s.AbapMsgNumber))
	}
	for i, val := range []string{s.AbapMsgV1, s.AbapMsgV2, s.AbapMsgV3, s.AbapMsgV4} {
		if val != "" {
			attrs = append(attrs, slog.String(
				fmt.Sprintf("abap_msg_v%d", i+1), v))
		}
	}
	return slog.GroupValue(attrs...)
}

// =============================================================
// SDK-mapped error types (RFC_ERROR_INFO.group dispatch)
// =============================================================

// LogonError corresponds to RFC_ERROR_INFO.group ==
// RFC_LOGON_FAILURE. Returned when authentication fails: bad
// password, locked user, expired ticket, refused certificate,
// SNC partner mismatch.
type LogonError struct {
	SDKErrorInfo
	// User is the SAP user that attempted logon. Emitted at
	// default severity because identifying *who* failed is
	// useful for ops; redact if your environment classifies it
	// as PII.
	User string
	// Client is the SAP mandant. Always emitted.
	Client string
	// SysID is the target system identifier. Always emitted.
	SysID string
}

func (e *LogonError) Error() string {
	return fmt.Sprintf("nwrfc: logon failed: %s (key=%s, user=%s, client=%s, sysid=%s)",
		e.SDKErrorInfo.Message, e.SDKErrorInfo.Key, e.User, e.Client, e.SysID)
}
func (e *LogonError) Unwrap() error              { return ErrLogon }
func (e *LogonError) Category() backend.Category { return backend.CategoryLogon }
func (e *LogonError) Is(target error) bool       { return target == ErrLogon }
func (e *LogonError) As(target any) bool {
	if t, ok := target.(**LogonError); ok {
		*t = e
		return true
	}
	return false
}
func (e *LogonError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryLogon.String()),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
		slog.String("user", e.User),
		slog.String("client", e.Client),
		slog.String("sysid", e.SysID),
	)
}

// CommunicationError corresponds to
// RFC_COMMUNICATION_FAILURE. Returned when the transport
// layer broke: socket closed, gateway refused, DNS failed,
// TLS handshake failed, peer-side reset.
type CommunicationError struct {
	SDKErrorInfo
	Host    string
	Service string
}

func (e *CommunicationError) Error() string {
	return fmt.Sprintf("nwrfc: communication failure: %s (host=%s, service=%s, key=%s)",
		e.SDKErrorInfo.Message, e.Host, e.Service, e.SDKErrorInfo.Key)
}
func (e *CommunicationError) Unwrap() error              { return ErrCommunication }
func (e *CommunicationError) Category() backend.Category { return backend.CategoryCommunication }
func (e *CommunicationError) Is(target error) bool       { return target == ErrCommunication }
func (e *CommunicationError) As(target any) bool {
	if t, ok := target.(**CommunicationError); ok {
		*t = e
		return true
	}
	return false
}
func (e *CommunicationError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryCommunication.String()),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
		slog.String("host", e.Host),
		slog.String("service", e.Service),
	)
}

// ABAPApplicationError corresponds to
// RFC_ABAP_APPLICATION_FAILURE. Returned when the called RFM
// raised a non-class exception (RAISE statement). Carries the
// ABAP message class/type/number/V1..V4.
type ABAPApplicationError struct {
	SDKErrorInfo
	Function string
}

func (e *ABAPApplicationError) Error() string {
	return fmt.Sprintf("nwrfc: ABAP application error in %s: %s/%s (msg=%s%s number=%s)",
		e.Function, e.SDKErrorInfo.Key, e.SDKErrorInfo.Message,
		e.SDKErrorInfo.AbapMsgClass, e.SDKErrorInfo.AbapMsgType, e.SDKErrorInfo.AbapMsgNumber)
}
func (e *ABAPApplicationError) Unwrap() error              { return ErrABAPApplication }
func (e *ABAPApplicationError) Category() backend.Category { return backend.CategoryABAPApp }
func (e *ABAPApplicationError) Is(target error) bool       { return target == ErrABAPApplication }
func (e *ABAPApplicationError) As(target any) bool {
	if t, ok := target.(**ABAPApplicationError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ABAPApplicationError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryABAPApp.String()),
		slog.String("function", e.Function),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// ABAPRuntimeError corresponds to
// RFC_ABAP_RUNTIME_FAILURE. Returned when the ABAP server
// produced a SHORT DUMP (ST22 entry). The Function field is
// set when the failed call site is known; a future field may
// carry a URL into ST22 if the SDK exposes it (🟡 verify).
type ABAPRuntimeError struct {
	SDKErrorInfo
	Function string
}

func (e *ABAPRuntimeError) Error() string {
	return fmt.Sprintf("nwrfc: ABAP runtime error in %s: %s/%s",
		e.Function, e.SDKErrorInfo.Key, e.SDKErrorInfo.Message)
}
func (e *ABAPRuntimeError) Unwrap() error              { return ErrABAPRuntime }
func (e *ABAPRuntimeError) Category() backend.Category { return backend.CategoryABAPRuntime }
func (e *ABAPRuntimeError) Is(target error) bool       { return target == ErrABAPRuntime }
func (e *ABAPRuntimeError) As(target any) bool {
	if t, ok := target.(**ABAPRuntimeError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ABAPRuntimeError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryABAPRuntime.String()),
		slog.String("function", e.Function),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// ABAPClassicException is set when an RFM raised a system
// (non-class) exception — the X section of MESSAGE. Pre-7.20
// shape; pyrfc historically separated it from
// ABAPApplicationError.
type ABAPClassicException struct {
	SDKErrorInfo
	Function string
}

func (e *ABAPClassicException) Error() string {
	return fmt.Sprintf("nwrfc: ABAP classic exception in %s: %s",
		e.Function, e.SDKErrorInfo.Key)
}
func (e *ABAPClassicException) Unwrap() error              { return ErrABAPClassic }
func (e *ABAPClassicException) Category() backend.Category { return backend.CategoryABAPClassic }
func (e *ABAPClassicException) Is(target error) bool       { return target == ErrABAPClassic }
func (e *ABAPClassicException) As(target any) bool {
	if t, ok := target.(**ABAPClassicException); ok {
		*t = e
		return true
	}
	return false
}
func (e *ABAPClassicException) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryABAPClassic.String()),
		slog.String("function", e.Function),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// ABAPClassException covers RAISE EXCEPTION TYPE on AS ABAP
// 7.20+. The ClassName field carries the ABAP exception class
// name when the SDK populates it (🟡 verify field availability).
type ABAPClassException struct {
	SDKErrorInfo
	Function  string
	ClassName string
}

func (e *ABAPClassException) Error() string {
	return fmt.Sprintf("nwrfc: ABAP class exception %s in %s: %s",
		e.ClassName, e.Function, e.SDKErrorInfo.Message)
}
func (e *ABAPClassException) Unwrap() error              { return ErrABAPClass }
func (e *ABAPClassException) Category() backend.Category { return backend.CategoryABAPClass }
func (e *ABAPClassException) Is(target error) bool       { return target == ErrABAPClass }
func (e *ABAPClassException) As(target any) bool {
	if t, ok := target.(**ABAPClassException); ok {
		*t = e
		return true
	}
	return false
}
func (e *ABAPClassException) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryABAPClass.String()),
		slog.String("function", e.Function),
		slog.String("class", e.ClassName),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// ExternalAuthorizationError corresponds to
// RFC_EXTERNAL_AUTHORIZATION_FAILURE. External (non-SAP)
// authorization callbacks refused the call. Treated as
// auth-shaped: never log the full SDKErrorInfo.Message
// because some callbacks include the rejected ticket payload
// in the message.
type ExternalAuthorizationError struct {
	SDKErrorInfo
}

func (e *ExternalAuthorizationError) Error() string {
	return fmt.Sprintf("nwrfc: external authorization failure: %s",
		e.SDKErrorInfo.Key)
}
func (e *ExternalAuthorizationError) Unwrap() error              { return ErrExtAuthz }
func (e *ExternalAuthorizationError) Category() backend.Category { return backend.CategoryExtAuthz }
func (e *ExternalAuthorizationError) Is(target error) bool       { return target == ErrExtAuthz }
func (e *ExternalAuthorizationError) As(target any) bool {
	if t, ok := target.(**ExternalAuthorizationError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ExternalAuthorizationError) LogValue() slog.Value {
	// Replace the SDK message wholesale because external authz
	// callbacks are known to embed ticket bytes in the message.
	info := e.SDKErrorInfo
	info.Message = "«redacted external authz message»"
	return slog.GroupValue(
		slog.String("category", backend.CategoryExtAuthz.String()),
		slog.Any("info", info.LogValue()),
	)
}

// ExternalApplicationError corresponds to
// RFC_EXTERNAL_APPLICATION_FAILURE. Raised by an external RFC
// server (Go, Java, .NET, ...) handler that returned an
// application error.
type ExternalApplicationError struct {
	SDKErrorInfo
	Function string
}

func (e *ExternalApplicationError) Error() string {
	return fmt.Sprintf("nwrfc: external application error in %s: %s",
		e.Function, e.SDKErrorInfo.Key)
}
func (e *ExternalApplicationError) Unwrap() error              { return ErrExtApp }
func (e *ExternalApplicationError) Category() backend.Category { return backend.CategoryExtApp }
func (e *ExternalApplicationError) Is(target error) bool       { return target == ErrExtApp }
func (e *ExternalApplicationError) As(target any) bool {
	if t, ok := target.(**ExternalApplicationError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ExternalApplicationError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryExtApp.String()),
		slog.String("function", e.Function),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// ExternalRuntimeError corresponds to
// RFC_EXTERNAL_RUNTIME_FAILURE. The SDK itself failed —
// allocation, marshaling, version mismatch, library
// dependency.
type ExternalRuntimeError struct {
	SDKErrorInfo
}

func (e *ExternalRuntimeError) Error() string {
	return fmt.Sprintf("nwrfc: external runtime error: %s/%s",
		e.SDKErrorInfo.Key, e.SDKErrorInfo.Message)
}
func (e *ExternalRuntimeError) Unwrap() error              { return ErrExtRuntime }
func (e *ExternalRuntimeError) Category() backend.Category { return backend.CategoryExtRuntime }
func (e *ExternalRuntimeError) Is(target error) bool       { return target == ErrExtRuntime }
func (e *ExternalRuntimeError) As(target any) bool {
	if t, ok := target.(**ExternalRuntimeError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ExternalRuntimeError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryExtRuntime.String()),
		slog.Any("info", e.SDKErrorInfo.LogValue()),
	)
}

// =============================================================
// Go-side errors (decided by the wrapper, not the SDK)
// =============================================================

// BrokenConnectionError is returned when a Conn is detected to
// be in a non-recoverable state — most commonly after a
// previous CommunicationError or after Close. Wraps the
// originating error if any.
type BrokenConnectionError struct {
	// Reason describes why the connection is considered broken.
	Reason string
	// Cause is the originating error, if any. Nil for "used
	// after Close".
	Cause error
}

func (e *BrokenConnectionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("nwrfc: broken connection: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("nwrfc: broken connection: %s", e.Reason)
}
func (e *BrokenConnectionError) Unwrap() error { return e.Cause }
func (e *BrokenConnectionError) Category() backend.Category {
	return backend.CategoryBrokenConn
}
func (e *BrokenConnectionError) Is(target error) bool {
	return target == ErrBrokenConn || (e.Cause != nil && errors.Is(e.Cause, target))
}
func (e *BrokenConnectionError) As(target any) bool {
	if t, ok := target.(**BrokenConnectionError); ok {
		*t = e
		return true
	}
	return false
}
func (e *BrokenConnectionError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryBrokenConn.String()),
		slog.String("reason", e.Reason),
		slog.Any("cause", e.Cause),
	)
}

// TimeoutError is returned when a context deadline elapsed
// before the SDK call finished. The wrapper calls RfcCancel
// from a watcher goroutine; the SDK reports back, then the
// wrapper synthesizes this error.
type TimeoutError struct {
	Function string
	Deadline time.Time
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("nwrfc: %s timed out (deadline=%s)",
		e.Function, e.Deadline.Format(time.RFC3339Nano))
}
func (e *TimeoutError) Unwrap() error              { return ErrTimeout }
func (e *TimeoutError) Category() backend.Category { return backend.CategoryTimeout }
func (e *TimeoutError) Is(target error) bool {
	return target == ErrTimeout || errors.Is(target, ErrCancelled) // timeouts are also cancellations
}
func (e *TimeoutError) As(target any) bool {
	if t, ok := target.(**TimeoutError); ok {
		*t = e
		return true
	}
	return false
}
func (e *TimeoutError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryTimeout.String()),
		slog.String("function", e.Function),
		slog.Time("deadline", e.Deadline),
	)
}

// CancelledError is returned when ctx was cancelled (without a
// deadline) before the SDK call finished.
type CancelledError struct {
	Function string
	Cause    error
}

func (e *CancelledError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("nwrfc: %s cancelled: %v", e.Function, e.Cause)
	}
	return fmt.Sprintf("nwrfc: %s cancelled", e.Function)
}
func (e *CancelledError) Unwrap() error              { return e.Cause }
func (e *CancelledError) Category() backend.Category { return backend.CategoryCancelled }
func (e *CancelledError) Is(target error) bool {
	if target == ErrCancelled {
		return true
	}
	if e.Cause != nil {
		return errors.Is(e.Cause, target)
	}
	return false
}
func (e *CancelledError) As(target any) bool {
	if t, ok := target.(**CancelledError); ok {
		*t = e
		return true
	}
	return false
}
func (e *CancelledError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryCancelled.String()),
		slog.String("function", e.Function),
		slog.Any("cause", e.Cause),
	)
}

// MarshalError is returned when the marshaling layer cannot
// convert a Go value to the requested ABAP type or vice versa.
// FieldName is the parameter or struct-field name; GoType is
// the Go type name; ABAPType is the destination ABAP type.
type MarshalError struct {
	FieldName string
	GoType    string
	ABAPType  string
	// Reason carries a free-form explanation (e.g. "value out
	// of range for INT2"), or wraps a more specific error like
	// [ErrZeroDate].
	Reason error
}

func (e *MarshalError) Error() string {
	return fmt.Sprintf("nwrfc: marshal error on %s: %s ↔ %s: %v",
		e.FieldName, e.GoType, e.ABAPType, e.Reason)
}
func (e *MarshalError) Unwrap() error              { return e.Reason }
func (e *MarshalError) Category() backend.Category { return backend.CategoryMarshal }
func (e *MarshalError) Is(target error) bool {
	if target == ErrMarshal {
		return true
	}
	if e.Reason != nil {
		return errors.Is(e.Reason, target)
	}
	return false
}
func (e *MarshalError) As(target any) bool {
	if t, ok := target.(**MarshalError); ok {
		*t = e
		return true
	}
	return false
}
func (e *MarshalError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryMarshal.String()),
		slog.String("field", e.FieldName),
		slog.String("go_type", e.GoType),
		slog.String("abap_type", e.ABAPType),
		slog.Any("reason", e.Reason),
	)
}

// ConfigError is returned when call-site configuration is
// invalid before any SDK call has been made (missing required
// parameter, contradictory options, malformed dest name).
// Field is the offending field path; Hint suggests how to fix.
type ConfigError struct {
	Field string
	Hint  string
}

func (e *ConfigError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("nwrfc: invalid configuration in %s: %s", e.Field, e.Hint)
	}
	return fmt.Sprintf("nwrfc: invalid configuration in %s", e.Field)
}
func (e *ConfigError) Unwrap() error              { return ErrConfig }
func (e *ConfigError) Category() backend.Category { return backend.CategoryConfig }
func (e *ConfigError) Is(target error) bool       { return target == ErrConfig }
func (e *ConfigError) As(target any) bool {
	if t, ok := target.(**ConfigError); ok {
		*t = e
		return true
	}
	return false
}
func (e *ConfigError) LogValue() slog.Value {
	// If the field name itself is sensitive (e.g. "passwd",
	// "saml2", "snc_lib"), redact the hint to avoid echoing
	// rejected input. The match table is shared with Params and
	// the otel redactor — single source of truth.
	hint := e.Hint
	if backend.IsSensitiveKey(e.Field) {
		hint = backend.RedactedPlaceholder
	}
	return slog.GroupValue(
		slog.String("category", backend.CategoryConfig.String()),
		slog.String("field", e.Field),
		slog.String("hint", hint),
	)
}

// SDKUnavailableError is returned by every operation in the
// no-SDK build (`-tags nwrfc_nosdk` or CGO_ENABLED=0). The
// Reason field describes the build mode; LookupPath is the
// path the SDK was searched in (when known), useful for
// operators diagnosing missing-SDK errors.
type SDKUnavailableError struct {
	Reason     string
	LookupPath string
}

func (e *SDKUnavailableError) Error() string {
	if e.LookupPath != "" {
		return fmt.Sprintf("nwrfc: SDK unavailable: %s (looked in %s)", e.Reason, e.LookupPath)
	}
	return fmt.Sprintf("nwrfc: SDK unavailable: %s", e.Reason)
}
func (e *SDKUnavailableError) Unwrap() error              { return ErrSDKUnavailable }
func (e *SDKUnavailableError) Category() backend.Category { return backend.CategorySDKUnavailable }
func (e *SDKUnavailableError) Is(target error) bool {
	return target == ErrSDKUnavailable || target == backend.ErrUnavailable
}
func (e *SDKUnavailableError) As(target any) bool {
	if t, ok := target.(**SDKUnavailableError); ok {
		*t = e
		return true
	}
	return false
}
func (e *SDKUnavailableError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategorySDKUnavailable.String()),
		slog.String("reason", e.Reason),
		slog.String("lookup_path", e.LookupPath),
	)
}

// UnsupportedFeatureError is returned when the active SDK
// patch level does not support the requested feature
// (WebSocket RFC, throughput, bgRFC, ...). RequiredVersion is
// the minimum, CurrentVersion is what the SDK reports.
type UnsupportedFeatureError struct {
	Feature         string
	RequiredVersion backend.Version
	CurrentVersion  backend.Version
}

func (e *UnsupportedFeatureError) Error() string {
	return fmt.Sprintf("nwrfc: feature %s requires SDK %s but %s is loaded",
		e.Feature, e.RequiredVersion, e.CurrentVersion)
}
func (e *UnsupportedFeatureError) Unwrap() error { return ErrUnsupported }
func (e *UnsupportedFeatureError) Category() backend.Category {
	return backend.CategoryUnsupported
}
func (e *UnsupportedFeatureError) Is(target error) bool {
	return target == ErrUnsupported || target == backend.ErrUnsupported
}
func (e *UnsupportedFeatureError) As(target any) bool {
	if t, ok := target.(**UnsupportedFeatureError); ok {
		*t = e
		return true
	}
	return false
}
func (e *UnsupportedFeatureError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("category", backend.CategoryUnsupported.String()),
		slog.String("feature", e.Feature),
		slog.String("required_version", e.RequiredVersion.String()),
		slog.String("current_version", e.CurrentVersion.String()),
	)
}

// =============================================================
// Helpers
// =============================================================

// CategoryOf reports the [backend.Category] of err if it (or
// any error it wraps) implements [backend.Categorized] /
// [RFCError]. Returns [backend.CategoryUnknown] otherwise.
//
// Walks both the linear `Unwrap() error` chain and the
// tree-shaped `Unwrap() []error` chain produced by
// [errors.Join]; the first categorized error encountered (in
// pre-order) wins.
func CategoryOf(err error) backend.Category {
	if err == nil {
		return backend.CategoryUnknown
	}
	if c, ok := err.(backend.Categorized); ok {
		return c.Category()
	}
	switch x := err.(type) {
	case interface{ Unwrap() error }:
		if cat := CategoryOf(x.Unwrap()); cat != backend.CategoryUnknown {
			return cat
		}
	case interface{ Unwrap() []error }:
		for _, child := range x.Unwrap() {
			if cat := CategoryOf(child); cat != backend.CategoryUnknown {
				return cat
			}
		}
	}
	return backend.CategoryUnknown
}

// IsRetryable reports whether err is the kind of failure where a
// retry against a fresh connection has a chance of succeeding.
// Communication failures and broken-connection errors retry;
// logon, application, runtime, marshal, config, unsupported,
// SDK-unavailable do not.
func IsRetryable(err error) bool {
	switch CategoryOf(err) {
	case backend.CategoryCommunication,
		backend.CategoryBrokenConn,
		backend.CategoryTimeout:
		return true
	default:
		return false
	}
}

// Compile-time assertions: every concrete error type satisfies
// the [RFCError] interface.
var (
	_ RFCError = (*LogonError)(nil)
	_ RFCError = (*CommunicationError)(nil)
	_ RFCError = (*ABAPApplicationError)(nil)
	_ RFCError = (*ABAPRuntimeError)(nil)
	_ RFCError = (*ABAPClassicException)(nil)
	_ RFCError = (*ABAPClassException)(nil)
	_ RFCError = (*ExternalAuthorizationError)(nil)
	_ RFCError = (*ExternalApplicationError)(nil)
	_ RFCError = (*ExternalRuntimeError)(nil)
	_ RFCError = (*BrokenConnectionError)(nil)
	_ RFCError = (*TimeoutError)(nil)
	_ RFCError = (*CancelledError)(nil)
	_ RFCError = (*MarshalError)(nil)
	_ RFCError = (*ConfigError)(nil)
	_ RFCError = (*SDKUnavailableError)(nil)
	_ RFCError = (*UnsupportedFeatureError)(nil)
	_ RFCError = sentinel{}
)
