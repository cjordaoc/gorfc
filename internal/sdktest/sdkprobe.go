// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// Package sdktest hosts cgo-using behavior tests that exercise
// the SAP NetWeaver RFC SDK directly, independent of any
// production binding.
//
// Why a separate package: Go's tooling refuses cgo inside test
// files of a cgo-using package (it would link two copies of the
// cgo runtime into one binary; see golang/go#27091). The
// internal/sdkbackend package uses cgo for the production
// binding, so its test files cannot `import "C"`. We resolve
// this by isolating raw-SDK behavior probes here, in a package
// whose production code uses cgo and whose test files do not.
//
// Probe functions verify SDK semantics that are observable
// purely from the loaded library (no SAP system needed):
//
//   - EncodeUTF8ToSAPUC / DecodeSAPUCToUTF8 — round-trip
//     reference vs the pure-Go internal/ucs2 path.
//   - RcAsString / TypeAsString / DirectionAsString — static
//     SDK lookup tables.
//
// Tests in sdkprobe_test.go consume these and assert behavior.
package sdktest

/*
#cgo linux CFLAGS: -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
#cgo linux CFLAGS: -DSAPwithUNICODE -D__NO_MATH_INLINES -DSAPwithTHREADS
#cgo linux CFLAGS: -DSAPonUNIX -DSAPonLIN
#cgo linux CFLAGS: -fno-strict-aliasing -fno-omit-frame-pointer -fexceptions -funsigned-char
#cgo linux LDFLAGS: -lsapnwrfc -lsapucum -pthread

#cgo windows CFLAGS: -DSAPonNT -DSAPwithUNICODE -DUNICODE -D_UNICODE
#cgo windows CFLAGS: -DSAPwithTHREADS -DNDEBUG -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64
#cgo windows LDFLAGS: -lsapnwrfc -llibsapucum

#cgo darwin CFLAGS: -DSAP_UC_is_wchar -DSAPwithUNICODE -D__NO_MATH_INLINES
#cgo darwin CFLAGS: -DSAPwithTHREADS -DSAPonDARW
#cgo darwin CFLAGS: -fexceptions -funsigned-char -fno-strict-aliasing -fPIC -pthread
#cgo darwin CFLAGS: -mmacosx-version-min=10.15
#cgo darwin LDFLAGS: -lsapnwrfc -lsapucum -mmacosx-version-min=10.15

#include <sapnwrfc.h>
#include <stdlib.h>
#include <string.h>

// Private helpers for the probe functions. Mirror
// internal/sdkbackend/helpers.c on purpose: tests should not
// depend on the production helpers, so the cgo path here is
// independently exercised.
static SAP_UC* probe_mallocU(unsigned size) { return mallocU(size); }
static unsigned probe_strlenU(SAP_UC* s) { return strlenU(s); }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// EncodeResult holds the SAP_UC byte layout produced by the SDK
// for one Go string. The byte slice is a copy of the C buffer
// (safe to keep across cgo calls); the buffer itself is freed
// before EncodeUTF8ToSAPUC returns.
type EncodeResult struct {
	// Bytes is the raw SAP_UC layout produced by
	// RfcUTF8ToSAPUC, including the trailing null code unit.
	// On all supported platforms (little-endian Linux/Win/Mac
	// x86_64/arm64) this matches UTF-16LE bytes.
	Bytes []byte

	// Decoded is the result of round-tripping Bytes back
	// through RfcSAPUCToUTF8. Should equal the input string.
	Decoded string
}

// EncodeUTF8ToSAPUC drives the SDK's encode → decode pair and
// returns both the intermediate byte layout and the decoded
// output. Errors surface RfcUTF8ToSAPUC / RfcSAPUCToUTF8
// failures verbatim.
func EncodeUTF8ToSAPUC(s string) (EncodeResult, error) {
	uc, err := encodeOne(s)
	if err != nil {
		return EncodeResult{}, err
	}
	defer C.free(unsafe.Pointer(uc))

	bytes := readSAPUCBytes(uc)
	decoded, err := decodeOne(uc)
	if err != nil {
		return EncodeResult{}, err
	}
	return EncodeResult{Bytes: bytes, Decoded: decoded}, nil
}

// EncodeDecodeOnce runs one encode+decode cycle and returns
// only the decoded string. Used by the leak stress test.
func EncodeDecodeOnce(s string) (string, error) {
	uc, err := encodeOne(s)
	if err != nil {
		return "", err
	}
	defer C.free(unsafe.Pointer(uc))
	return decodeOne(uc)
}

func encodeOne(s string) (*C.SAP_UC, error) {
	size := C.uint(len(s) + 1)
	uc := C.probe_mallocU(size)
	if uc == nil {
		return nil, fmt.Errorf("probe_mallocU(%d) returned NULL", size)
	}
	C.memset(unsafe.Pointer(uc), 0, C.size_t(size)*C.size_t(unsafe.Sizeof(*uc)))
	if len(s) == 0 {
		return uc, nil
	}
	cstr := C.CString(s)
	defer C.free(unsafe.Pointer(cstr))

	var resultLen C.uint
	var info C.RFC_ERROR_INFO
	rc := C.RfcUTF8ToSAPUC(
		(*C.RFC_BYTE)(unsafe.Pointer(cstr)),
		C.uint(len(s)),
		uc, &size, &resultLen, &info,
	)
	if rc != C.RFC_OK {
		C.free(unsafe.Pointer(uc))
		return nil, fmt.Errorf("RfcUTF8ToSAPUC: rc=%d", int(rc))
	}
	return uc, nil
}

func decodeOne(uc *C.SAP_UC) (string, error) {
	if uc == nil {
		return "", nil
	}
	n := C.probe_strlenU(uc)
	if n == 0 {
		return "", nil
	}
	bufSize := C.uint(5*n + 1)
	out := (*C.RFC_BYTE)(C.malloc(C.size_t(bufSize)))
	if out == nil {
		return "", errors.New("malloc returned NULL")
	}
	defer C.free(unsafe.Pointer(out))

	var resultLen C.uint
	var info C.RFC_ERROR_INFO
	rc := C.RfcSAPUCToUTF8(uc, n, out, &bufSize, &resultLen, &info)
	if rc != C.RFC_OK {
		return "", fmt.Errorf("RfcSAPUCToUTF8: rc=%d", int(rc))
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(out)), C.int(resultLen)), nil
}

func readSAPUCBytes(uc *C.SAP_UC) []byte {
	if uc == nil {
		return nil
	}
	n := int(C.probe_strlenU(uc)) + 1
	totalBytes := n * int(unsafe.Sizeof(*uc))
	out := make([]byte, totalBytes)
	src := (*[1 << 30]byte)(unsafe.Pointer(uc))[:totalBytes:totalBytes]
	copy(out, src)
	return out
}

// RcAsString wraps RfcGetRcAsString. Returns the empty string
// if the SDK returns NULL (which it should not, per the header
// — the function has a static lookup table).
func RcAsString(rc int32) string {
	uc := C.RfcGetRcAsString(C.RFC_RC(rc))
	if uc == nil {
		return ""
	}
	s, _ := decodeOne((*C.SAP_UC)(unsafe.Pointer(uc)))
	return s
}

// TypeAsString wraps RfcGetTypeAsString.
func TypeAsString(ty int32) string {
	uc := C.RfcGetTypeAsString(C.RFCTYPE(ty))
	if uc == nil {
		return ""
	}
	s, _ := decodeOne((*C.SAP_UC)(unsafe.Pointer(uc)))
	return s
}

// DirectionAsString wraps RfcGetDirectionAsString.
func DirectionAsString(dir int32) string {
	uc := C.RfcGetDirectionAsString(C.RFC_DIRECTION(dir))
	if uc == nil {
		return ""
	}
	s, _ := decodeOne((*C.SAP_UC)(unsafe.Pointer(uc)))
	return s
}

// Constants the test file uses without needing its own cgo.
// Values mirror sapnwrfc.h §93-§113 (PL18) verbatim so the
// tests stay pure-Go.
const (
	RfcOK                   = 0
	RfcCommunicationFailure = 1
	RfcLogonFailure         = 2
	RfcAbapRuntimeFailure   = 3
	RfcAbapMessage          = 4
	RfcAbapException        = 5
	RfcInvalidParameter     = 17

	RfcTypeChar      = 0
	RfcTypeDate      = 1
	RfcTypeBcd       = 2
	RfcTypeTime      = 3
	RfcTypeByte      = 4
	RfcTypeTable     = 5
	RfcTypeNum       = 6
	RfcTypeFloat     = 7
	RfcTypeInt       = 8
	RfcTypeInt2      = 9
	RfcTypeInt1      = 10
	RfcTypeStructure = 17
	RfcTypeDecF16    = 23
	RfcTypeDecF34    = 24
	RfcTypeString    = 29
	RfcTypeXString   = 30
	RfcTypeInt8      = 31
	RfcTypeUtcLong   = 32

	RfcImport   = 0
	RfcExport   = 1
	RfcChanging = 2
	RfcTables   = 3
)
