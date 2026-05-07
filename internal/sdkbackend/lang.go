// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// SAP language code conversion.
//
// SAP carries a 1-letter language code in its CHAR1 / SY-LANGU
// fields (e.g. "E" for English, "D" for German, "P" for
// Portuguese). The 2-letter ISO 639-1 form ("EN", "DE", "PT")
// is what Go callers usually have. The SDK exposes the lookup
// in both directions.
//
// Per sapnwrfc.h §1237/§1249 (PL18):
//
//	RfcLanguageIsoToSap(const SAP_UC *laiso, SAP_UC *lang, RFC_ERROR_INFO*)
//	RfcLanguageSapToIso(const SAP_UC *lang,  SAP_UC *laiso, RFC_ERROR_INFO*)
//
// The output buffers must be sized for one (SAP) or two (ISO)
// SAP_UC code units plus the null terminator.

package sdkbackend

/*
#include "helpers.h"
#include <string.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// LanguageISOToSAP converts an ISO 639-1 two-letter code to the
// SAP one-letter code. Returns the empty string and an error if
// the SDK rejects the input (unknown language, malformed length).
//
// The SDK writes ONLY the result code unit into the output
// buffer and does NOT null-terminate it. We therefore zero the
// whole buffer before the call (so a single trailing null is
// guaranteed) and read the result with the null-terminated
// helper. The previous binding allocated via mallocU which does
// not zero memory, leading to a stale code unit appearing as a
// garbage character after the result (e.g. "Eᷬ" for "EN").
//
// SDK function: RfcLanguageIsoToSap (✅ behavior verified PL18).
func (*sdkBackend) LanguageISOToSAP(iso string) (string, error) {
	if len(iso) != 2 {
		return "", fmt.Errorf("nwrfc/sdkbackend: ISO language code must be 2 characters, got %q", iso)
	}
	isoUC, err := stringToSAPUC(iso)
	if err != nil {
		return "", err
	}
	defer C.goFreeU(isoUC)

	// Output buffer: 1 SAP_UC code unit for the SAP letter
	// + 1 trailing null = 2 code units total. mallocU returns
	// `n * sizeof(SAP_UC)` bytes; memset zero across the whole
	// allocation guarantees null termination after the SDK
	// writes the single result code unit.
	const outUnits = 2
	out := C.goMallocU(outUnits)
	if out == nil {
		return "", fmt.Errorf("nwrfc/sdkbackend: mallocU(%d) returned NULL", outUnits)
	}
	defer C.goFreeU(out)
	C.memset(unsafe.Pointer(out), 0, C.size_t(outUnits)*C.size_t(unsafe.Sizeof(*out)))

	var info C.RFC_ERROR_INFO
	rc := C.RfcLanguageIsoToSap(isoUC, out, &info)
	if rc != C.RFC_OK {
		return "", errFromInfo(&info, "RfcLanguageIsoToSap("+iso+")")
	}
	return sapUCToString(out)
}

// LanguageSAPToISO converts a SAP one-letter language code to
// ISO 639-1.
//
// Same buffer-zeroing rationale as [LanguageISOToSAP]: the SDK
// does not null-terminate the result.
//
// SDK function: RfcLanguageSapToIso (✅ behavior verified PL18).
func (*sdkBackend) LanguageSAPToISO(sap string) (string, error) {
	if len(sap) != 1 {
		return "", fmt.Errorf("nwrfc/sdkbackend: SAP language code must be 1 character, got %q", sap)
	}
	sapUC, err := stringToSAPUC(sap)
	if err != nil {
		return "", err
	}
	defer C.goFreeU(sapUC)

	// Output buffer: 2 SAP_UC code units for the ISO digraph
	// + 1 trailing null = 3 code units total.
	const outUnits = 3
	out := C.goMallocU(outUnits)
	if out == nil {
		return "", fmt.Errorf("nwrfc/sdkbackend: mallocU(%d) returned NULL", outUnits)
	}
	defer C.goFreeU(out)
	C.memset(unsafe.Pointer(out), 0, C.size_t(outUnits)*C.size_t(unsafe.Sizeof(*out)))

	var info C.RFC_ERROR_INFO
	rc := C.RfcLanguageSapToIso(sapUC, out, &info)
	if rc != C.RFC_OK {
		return "", errFromInfo(&info, "RfcLanguageSapToIso("+sap+")")
	}
	return sapUCToString(out)
}
