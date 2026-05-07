// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include <sapnwrfc.h>
*/
import "C"

import (
	"fmt"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// errFromInfo decodes a populated RFC_ERROR_INFO into one of
// the typed nwrfc error structs. The mapping follows
// docs/PLAN.md §7.
//
// Field decoding: RFC_ERROR_INFO members are SAP_UC arrays; we
// decode them via sapUCSliceToString (zero/space stripped).
func errFromInfo(info *C.RFC_ERROR_INFO, op string) error {
	sdk := nwrfc.SDKErrorInfo{
		Code:          int(info.code),
		Group:         uint32(info.group),
		Key:           sapUCSliceToString(info.key[:]),
		Message:       sapUCSliceToString(info.message[:]),
		AbapMsgClass:  sapUCSliceToString(info.abapMsgClass[:]),
		AbapMsgType:   sapUCSliceToString(info.abapMsgType[:]),
		AbapMsgNumber: sapUCSliceToString(info.abapMsgNumber[:]),
		AbapMsgV1:     sapUCSliceToString(info.abapMsgV1[:]),
		AbapMsgV2:     sapUCSliceToString(info.abapMsgV2[:]),
		AbapMsgV3:     sapUCSliceToString(info.abapMsgV3[:]),
		AbapMsgV4:     sapUCSliceToString(info.abapMsgV4[:]),
	}
	switch info.group {
	case C.LOGON_FAILURE:
		return &nwrfc.LogonError{SDKErrorInfo: sdk}
	case C.COMMUNICATION_FAILURE:
		return &nwrfc.CommunicationError{SDKErrorInfo: sdk}
	case C.ABAP_APPLICATION_FAILURE:
		return &nwrfc.ABAPApplicationError{SDKErrorInfo: sdk, Function: op}
	case C.ABAP_RUNTIME_FAILURE:
		return &nwrfc.ABAPRuntimeError{SDKErrorInfo: sdk, Function: op}
	case C.EXTERNAL_AUTHORIZATION_FAILURE:
		return &nwrfc.ExternalAuthorizationError{SDKErrorInfo: sdk}
	case C.EXTERNAL_APPLICATION_FAILURE:
		return &nwrfc.ExternalApplicationError{SDKErrorInfo: sdk, Function: op}
	case C.EXTERNAL_RUNTIME_FAILURE:
		return &nwrfc.ExternalRuntimeError{SDKErrorInfo: sdk}
	default:
		// Unknown group: surface as ExternalRuntime so the
		// caller still gets a typed error; the Code/Key are
		// preserved for diagnosis.
		return &nwrfc.ExternalRuntimeError{
			SDKErrorInfo: sdk,
		}
	}
}

// errMarshal wraps a marshal-shaped failure with the field
// name and Go/ABAP type info. Used by fill / wrap.
func errMarshal(field, goType, abapType string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("conversion failed")
	}
	return &nwrfc.MarshalError{
		FieldName: field,
		GoType:    goType,
		ABAPType:  abapType,
		Reason:    cause,
	}
}

// _ keeps the [backend] import live; the file uses only
// internal types but the package needs the import.
var _ = backend.CategoryUnknown
