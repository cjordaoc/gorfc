// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"fmt"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// errFromInfo decodes a populated RFC_ERROR_INFO into a
// [backend.SDKError]. The public nwrfc package translates that
// to a concrete typed error (LogonError, ABAPApplicationError,
// ...) via mapBackendError. This split avoids an import cycle:
// sdkbackend → backend (one-way) → nwrfc reads SDKError.
func errFromInfo(info *C.RFC_ERROR_INFO, op string) error {
	return &backend.SDKError{
		Op: op,
		Info: backend.SDKErrorInfo{
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
		},
	}
}

// errMarshal returns a marshal-shaped failure that nwrfc's
// mapBackendError translates to *MarshalError. Lives in this
// package as a free-form error; the typed struct construction
// is at the nwrfc boundary.
func errMarshal(field, goType, abapType string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("conversion failed")
	}
	return fmt.Errorf("nwrfc/sdkbackend marshal: field=%s goType=%s abapType=%s: %w",
		field, goType, abapType, cause)
}
