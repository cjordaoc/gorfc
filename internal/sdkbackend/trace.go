// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// SAP NetWeaver RFC SDK trace control.
//
// The SDK exports four mutators for trace state, all per
// sapnwrfc.h §812-§946 (PL18):
//
//   - RfcSetTraceLevel(connection, destination, level, info)
//     — 0 = off, 1..3 increasingly verbose. When connection and
//     destination are NULL, the level is set globally.
//   - RfcSetTraceDir(dir, info) — directory where dev_rfc*.trc
//     files get written.
//   - RfcSetTraceEncoding(encoding, info) — "UTF-8" or "DEFAULT".
//   - RfcSetTraceType(type, info) — text vs cpic trace.
//
// All four are process-global, not per-connection. The SDK stores
// the values in a singleton; concurrent SetTrace* from multiple
// goroutines requires the caller to serialize. AGENTS.md
// §Engineering Rules: documented + tested.
//
// Security note: trace level > 0 captures payloads to disk. See
// docs/SECURITY.md §5.

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"fmt"
)

// SetTraceLevel forwards to RfcSetTraceLevel. The SDK accepts
// 0..3; we range-check to surface bad input as a Go error rather
// than a (non-)warning from the SDK.
//
// SDK function: RfcSetTraceLevel (✅ behavior verified PL18).
func (*sdkBackend) SetTraceLevel(level int) error {
	if level < 0 || level > 3 {
		return fmt.Errorf("nwrfc/sdkbackend: trace level %d out of range [0,3]", level)
	}
	var info C.RFC_ERROR_INFO
	rc := C.RfcSetTraceLevel(nil, nil, C.uint(level), &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSetTraceLevel")
	}
	return nil
}

// SetTraceDir forwards to RfcSetTraceDir. The directory must
// exist and be writable; the SDK will not create it.
//
// SDK function: RfcSetTraceDir (✅ behavior verified PL18).
func (*sdkBackend) SetTraceDir(dir string) error {
	dirUC, err := stringToSAPUC(dir)
	if err != nil {
		return err
	}
	defer C.goFreeU(dirUC)

	var info C.RFC_ERROR_INFO
	rc := C.RfcSetTraceDir(dirUC, &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSetTraceDir")
	}
	return nil
}

// setTraceEncoding forwards to RfcSetTraceEncoding. Internal
// helper — exposed via the Trace interface only when needed.
// Currently unexported; kept here so the trace.go file groups
// every RfcSetTrace* call.
//
// SDK function: RfcSetTraceEncoding (✅ exists in PL18).
func (*sdkBackend) setTraceEncoding(encoding string) error {
	encUC, err := stringToSAPUC(encoding)
	if err != nil {
		return err
	}
	defer C.goFreeU(encUC)

	var info C.RFC_ERROR_INFO
	rc := C.RfcSetTraceEncoding(encUC, &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSetTraceEncoding")
	}
	return nil
}
