// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// SAP NetWeaver RFC SDK ini-file controls.
//
// The SDK searches for `sapnwrfc.ini` in (a) the directory
// configured via `RfcSetIniPath`, (b) the current working
// directory, (c) the directory of the running executable. The
// file lists named destinations the application can connect to
// via `Conn.OpenDest`.
//
// `RfcReloadIniFile` re-reads the file at runtime so a service
// can pick up changes without restart. Both calls are
// process-global (no per-Conn state).
//
// Per sapnwrfc.h §812/§829 (PL18):
//
//	RfcSetIniPath(const SAP_UC *pathName, RFC_ERROR_INFO*)
//	RfcReloadIniFile(RFC_ERROR_INFO*)

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

// SetIniPath forwards to RfcSetIniPath.
//
// SDK function: RfcSetIniPath (✅ behavior verified PL18).
func (*sdkBackend) SetIniPath(dir string) error {
	dirUC, err := stringToSAPUC(dir)
	if err != nil {
		return err
	}
	defer C.goFreeU(dirUC)

	var info C.RFC_ERROR_INFO
	rc := C.RfcSetIniPath(dirUC, &info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSetIniPath")
	}
	return nil
}

// ReloadIniFile forwards to RfcReloadIniFile.
//
// SDK function: RfcReloadIniFile (✅ behavior verified PL18).
func (*sdkBackend) ReloadIniFile() error {
	var info C.RFC_ERROR_INFO
	rc := C.RfcReloadIniFile(&info)
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcReloadIniFile")
	}
	return nil
}
