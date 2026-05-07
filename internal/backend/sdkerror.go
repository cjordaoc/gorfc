// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

import "fmt"

// SDKErrorInfo carries the structured payload extracted from
// `RFC_ERROR_INFO`. Lives at this package layer (not in nwrfc)
// so the cgo-bound `internal/sdkbackend` can return it without
// importing the public `nwrfc` package — which would create a
// cycle, since `nwrfc` blank-imports the backends to register
// them.
//
// `nwrfc/errors.go` translates a SDKError into one of the 9
// SDK-mapped typed structs (LogonError, CommunicationError,
// ABAPApplicationError, ...). End users never see SDKError
// directly; they see the typed shape.
type SDKErrorInfo struct {
	Code          int    // RFC_RC numeric code
	Group         uint32 // RFC_ERROR_GROUP enum value
	Key           string
	Message       string
	AbapMsgClass  string
	AbapMsgType   string
	AbapMsgNumber string
	AbapMsgV1     string
	AbapMsgV2     string
	AbapMsgV3     string
	AbapMsgV4     string
}

// SDKError is the wire-shaped error every cgo binding emits.
// Its [Group] tells nwrfc.mapBackendError which typed struct
// to construct.
type SDKError struct {
	Op   string // "RfcOpenConnection", etc.
	Info SDKErrorInfo
}

// Error implements error.
func (e *SDKError) Error() string {
	return fmt.Sprintf("nwrfc/sdkbackend: %s: code=%d group=%d key=%q msg=%q",
		e.Op, e.Info.Code, e.Info.Group, e.Info.Key, e.Info.Message)
}

// SDK error groups, matching the SAP NetWeaver RFC SDK
// `RFC_ERROR_GROUP` enum values. Repeated here so the public
// nwrfc package can switch on them without depending on cgo
// or the SDK headers.
//
// 🟡 Verify against SAP NetWeaver RFC SDK Programming Guide
// `<sapnwrfc.h>` enum order. node-rfc/PyRFC bindings reproduce
// the values; we follow them.
const (
	GroupOK                           uint32 = 0
	GroupAbapApplicationFailure       uint32 = 1
	GroupAbapRuntimeFailure           uint32 = 2
	GroupLogonFailure                 uint32 = 3
	GroupCommunicationFailure         uint32 = 4
	GroupExternalRuntimeFailure       uint32 = 5
	GroupExternalApplicationFailure   uint32 = 6
	GroupExternalAuthorizationFailure uint32 = 7
)
