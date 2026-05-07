// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include <sapnwrfc.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// stringToSAPUC converts a Go UTF-8 string to a *C.SAP_UC
// allocated via the SDK's `mallocU`. Caller MUST free with
// goFreeU.
//
// Bug fix versus upstream gorfc/gorfc.go fillString: when
// RfcUTF8ToSAPUC fails, the previous code returned the
// already-allocated buffer with the error, leaking it on the
// caller side. Here we free it before returning.
//
// SDK function: RfcUTF8ToSAPUC (✅ confirmed).
func stringToSAPUC(s string) (*C.SAP_UC, error) {
	// Allocate len+1 code units (room for trailing null).
	size := C.uint(len(s) + 1)
	uc := C.goMallocU(size)
	if uc == nil {
		return nil, fmt.Errorf("nwrfc/sdkbackend: mallocU(%d) returned NULL", size)
	}
	*uc = 0
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
		C.goFreeU(uc)
		return nil, errFromInfo(&info, "RfcUTF8ToSAPUC")
	}
	return uc, nil
}

// sapUCToString converts a *C.SAP_UC (null-terminated) to a Go
// UTF-8 string via RfcSAPUCToUTF8. Returns ("", nil) when uc
// is NULL or empty.
//
// SDK function: RfcSAPUCToUTF8 (✅ confirmed).
func sapUCToString(uc *C.SAP_UC) (string, error) {
	if uc == nil {
		return "", nil
	}
	return sapUCNToString(uc, C.goStrlenU(uc))
}

// sapUCNToString converts the first n code units of uc to a Go
// UTF-8 string, including the trailing null padding. Used for
// fixed-length CHAR / NUM fields where the SDK does not
// null-terminate.
func sapUCNToString(uc *C.SAP_UC, n C.uint) (string, error) {
	if uc == nil || n == 0 {
		return "", nil
	}
	bufSize := C.uint(5*n + 1) // worst-case UTF-8 expansion
	out := (*C.RFC_BYTE)(C.malloc(C.size_t(bufSize)))
	if out == nil {
		return "", fmt.Errorf("nwrfc/sdkbackend: malloc(%d) returned NULL", bufSize)
	}
	defer C.free(unsafe.Pointer(out))

	var resultLen C.uint
	var info C.RFC_ERROR_INFO
	rc := C.RfcSAPUCToUTF8(uc, n, out, &bufSize, &resultLen, &info)
	if rc != C.RFC_OK {
		return "", errFromInfo(&info, "RfcSAPUCToUTF8")
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(out)), C.int(resultLen)), nil
}

// sapUCSliceToString converts a fixed-size SAP_UC array slice
// (the kind that appears as a struct field, e.g.
// `attr.dest[64]`) to a UTF-8 string. The slice is treated as
// a sequence of UTF-16 code units; trailing nulls and trailing
// spaces are stripped.
func sapUCSliceToString(uc []C.SAP_UC) string {
	if len(uc) == 0 {
		return ""
	}
	// Find the unpadded length (strip trailing null/space).
	end := len(uc)
	for end > 0 {
		c := uc[end-1]
		if c == 0 || c == ' ' {
			end--
			continue
		}
		break
	}
	if end == 0 {
		return ""
	}
	s, err := sapUCNToString(&uc[0], C.uint(end))
	if err != nil {
		return ""
	}
	return s
}
