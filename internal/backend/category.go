// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

// Category enumerates the high-level error categories every
// backend must report. The public `nwrfc/errors.go` file (T1.3)
// maps these to its 13 typed error structs; this enum keeps the
// internal contract compact.
//
// The mapping from `RFC_ERROR_INFO.group` (the SDK's enum) to
// [Category] is in `internal/sdkbackend/errors.go`. The mock and
// no-SDK stub set Category directly.
type Category uint8

const (
	// CategoryUnknown is reserved for inputs that did not match
	// any other category. Should not appear in practice; treat
	// as a programming bug if observed.
	CategoryUnknown Category = iota

	// CategoryLogon corresponds to RFC_LOGON_FAILURE: bad
	// password, locked user, expired ticket. Mapped to
	// `*nwrfc.LogonError`.
	CategoryLogon

	// CategoryCommunication corresponds to RFC_COMMUNICATION_FAILURE:
	// gateway unreachable, peer closed the socket, DNS
	// failure. Mapped to `*nwrfc.CommunicationError`.
	CategoryCommunication

	// CategoryABAPApp corresponds to RFC_ABAP_APPLICATION_FAILURE:
	// the RFM raised a classic exception (RAISE / MESSAGE). The
	// payload carries the ABAP message class/type/number/V1..V4.
	// Mapped to `*nwrfc.ABAPApplicationError`.
	CategoryABAPApp

	// CategoryABAPRuntime corresponds to RFC_ABAP_RUNTIME_FAILURE:
	// SHORT DUMP on the SAP side. Mapped to
	// `*nwrfc.ABAPRuntimeError`.
	CategoryABAPRuntime

	// CategoryABAPClassic corresponds to system exceptions
	// raised before class-based exceptions existed (the X
	// section of MESSAGE statements). Mapped to
	// `*nwrfc.ABAPClassicException`.
	CategoryABAPClassic

	// CategoryABAPClass corresponds to class-based exceptions
	// (RAISE EXCEPTION TYPE), available on AS ABAP ≥ 7.20.
	// Mapped to `*nwrfc.ABAPClassException`.
	CategoryABAPClass

	// CategoryExtAuthz corresponds to
	// RFC_EXTERNAL_AUTHORIZATION_FAILURE: external auth (e.g. a
	// pluggable authorization callback) refused. Mapped to
	// `*nwrfc.ExternalAuthorizationError`.
	CategoryExtAuthz

	// CategoryExtApp corresponds to RFC_EXTERNAL_APPLICATION_FAILURE:
	// raised by an external (non-ABAP) RFC server's handler.
	// Mapped to `*nwrfc.ExternalApplicationError`.
	CategoryExtApp

	// CategoryExtRuntime corresponds to RFC_EXTERNAL_RUNTIME_FAILURE:
	// the SDK itself failed (allocation, marshaling, version
	// mismatch). Mapped to `*nwrfc.ExternalRuntimeError`.
	CategoryExtRuntime

	// CategoryBrokenConn is set by the Go side when a connection
	// is detected to be in a non-recoverable state (after a
	// communication error, after Close). Mapped to
	// `*nwrfc.BrokenConnectionError`.
	CategoryBrokenConn

	// CategoryTimeout is set by the Go side when ctx hit its
	// deadline mid-call. Mapped to `*nwrfc.TimeoutError`.
	CategoryTimeout

	// CategoryCancelled is set by the Go side when ctx was
	// cancelled mid-call. Mapped to `*nwrfc.CancelledError`.
	CategoryCancelled

	// CategoryMarshal is set by the marshaling layer in
	// `nwrfc/` when a Go value cannot be converted to the
	// requested ABAP type or vice versa. Mapped to
	// `*nwrfc.MarshalError`.
	CategoryMarshal

	// CategoryConfig is set by the Go side when the connection
	// parameters or call options are invalid before any SDK
	// call has been made. Mapped to `*nwrfc.ConfigError`.
	CategoryConfig

	// CategorySDKUnavailable is returned by the no-SDK stub on
	// every operation. Mapped to `*nwrfc.SDKUnavailableError`.
	CategorySDKUnavailable

	// CategoryUnsupported is returned when the active SDK
	// patch level does not support the requested capability.
	// Mapped to `*nwrfc.UnsupportedFeatureError`.
	CategoryUnsupported
)

// String returns a stable lowercase identifier for the category,
// suitable for log/span attributes. Stable across versions.
func (c Category) String() string {
	switch c {
	case CategoryLogon:
		return "logon"
	case CategoryCommunication:
		return "communication"
	case CategoryABAPApp:
		return "abap_application"
	case CategoryABAPRuntime:
		return "abap_runtime"
	case CategoryABAPClassic:
		return "abap_classic"
	case CategoryABAPClass:
		return "abap_class"
	case CategoryExtAuthz:
		return "external_authz"
	case CategoryExtApp:
		return "external_application"
	case CategoryExtRuntime:
		return "external_runtime"
	case CategoryBrokenConn:
		return "broken_conn"
	case CategoryTimeout:
		return "timeout"
	case CategoryCancelled:
		return "cancelled"
	case CategoryMarshal:
		return "marshal"
	case CategoryConfig:
		return "config"
	case CategorySDKUnavailable:
		return "sdk_unavailable"
	case CategoryUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// Categorized is implemented by every error returned from a
// backend. The public `nwrfc/errors.go` (T1.3) embeds this so
// callers can branch on category via [errors.As].
type Categorized interface {
	error
	Category() Category
}
