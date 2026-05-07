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
	"unsafe"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/internal/timeext"
)

// wrapFunctionParameters reads every EXPORT/CHANGING/TABLES/
// RETURN parameter on the function handle into a fresh
// CallParams map.
func wrapFunctionParameters(fh C.RFC_FUNCTION_HANDLE, desc C.RFC_FUNCTION_DESC_HANDLE, opts backend.InvokeOptions) (backend.CallParams, error) {
	var paramCount C.uint
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetParameterCount(desc, &paramCount, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcGetParameterCount")
	}

	wantDir := opts.DirectionFilter
	if wantDir == 0 {
		wantDir = backend.DirAllResults
	}

	out := make(backend.CallParams, paramCount)
	for i := C.uint(0); i < paramCount; i++ {
		var pd C.RFC_PARAMETER_DESC
		if rc := C.RfcGetParameterDescByIndex(desc, i, &pd, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetParameterDescByIndex")
		}
		dir := convertDirection(pd.direction)
		if wantDir&dir == 0 {
			continue
		}
		name := sapUCSliceToString(pd.name[:])
		v, err := wrapVariable(pd._type, unsafe.Pointer(fh), &pd.name[0], pd.ucLength, pd.typeDescHandle, opts, name)
		if err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

// wrapVariable reads a single ABAP scalar / structure / table
// from the SDK container.
func wrapVariable(rfcType C.RFCTYPE, container unsafe.Pointer, cName *C.SAP_UC, ucLength C.uint, typeDesc C.RFC_TYPE_DESC_HANDLE, opts backend.InvokeOptions, fieldName string) (any, error) {
	var info C.RFC_ERROR_INFO
	switch rfcType {
	case C.RFCTYPE_CHAR:
		buf := C.goMallocU(ucLength)
		defer C.goFreeU(buf)
		if rc := C.RfcGetChars((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_CHAR)(unsafe.Pointer(buf)), ucLength, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetChars("+fieldName+")")
		}
		s, err := sapUCNToString(buf, ucLength)
		if err != nil {
			return nil, err
		}
		if rstrip := opts.RStrip; rstrip == nil || *rstrip {
			s = trimTrailing(s)
		}
		return s, nil

	case C.RFCTYPE_NUM:
		buf := C.goMallocU(ucLength)
		defer C.goFreeU(buf)
		if rc := C.RfcGetNum((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_NUM)(unsafe.Pointer(buf)), ucLength, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetNum("+fieldName+")")
		}
		s, err := sapUCNToString(buf, ucLength)
		if err != nil {
			return nil, err
		}
		return s, nil

	case C.RFCTYPE_STRING, C.RFCTYPE_BCD, C.RFCTYPE_DECF16,
		C.RFCTYPE_DECF34, C.RFCTYPE_FLOAT, C.RFCTYPE_UTCLONG:
		var strLen C.uint
		if rc := C.RfcGetStringLength((C.RFC_FUNCTION_HANDLE)(container), cName, &strLen, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetStringLength("+fieldName+")")
		}
		buf := C.goMallocU(strLen + 1)
		defer C.goFreeU(buf)
		var resultLen C.uint
		if rc := C.RfcGetString((C.RFC_FUNCTION_HANDLE)(container), cName, buf, strLen+1, &resultLen, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetString("+fieldName+")")
		}
		return sapUCNToString(buf, resultLen)

	case C.RFCTYPE_INT1, C.RFCTYPE_INT2, C.RFCTYPE_INT, C.RFCTYPE_INT8:
		var n C.RFC_INT
		if rc := C.RfcGetInt((C.RFC_FUNCTION_HANDLE)(container), cName, &n, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetInt("+fieldName+")")
		}
		return int64(n), nil

	case C.RFCTYPE_BYTE, C.RFCTYPE_XSTRING:
		// XSTRING needs length first; BYTE has fixed length.
		var byteLen C.uint = ucLength
		if rfcType == C.RFCTYPE_XSTRING {
			if rc := C.RfcGetStringLength((C.RFC_FUNCTION_HANDLE)(container), cName, &byteLen, &info); rc != C.RFC_OK {
				return nil, errFromInfo(&info, "RfcGetStringLength("+fieldName+")")
			}
		}
		if byteLen == 0 {
			return []byte{}, nil
		}
		buf := (*C.SAP_RAW)(C.malloc(C.size_t(byteLen)))
		defer C.free(unsafe.Pointer(buf))
		var rc C.RFC_RC
		if rfcType == C.RFCTYPE_BYTE {
			rc = C.RfcGetBytes((C.RFC_FUNCTION_HANDLE)(container), cName, buf, byteLen, &info)
		} else {
			var resultLen C.uint
			rc = C.RfcGetXString((C.RFC_FUNCTION_HANDLE)(container), cName, buf, byteLen, &resultLen, &info)
		}
		if rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetBytes/XString("+fieldName+")")
		}
		return C.GoBytes(unsafe.Pointer(buf), C.int(byteLen)), nil

	case C.RFCTYPE_DATE:
		var buf [9]C.SAP_UC // 8 chars + null
		if rc := C.RfcGetDate((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_CHAR)(unsafe.Pointer(&buf[0])), &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetDate("+fieldName+")")
		}
		s, err := sapUCNToString(&buf[0], 8)
		if err != nil {
			return nil, err
		}
		d, err := timeext.ParseDate(s, timeext.ParseOptions{AllowZero: opts.AllowZeroDate, Strict: opts.CheckDate})
		if err != nil {
			return nil, errMarshal(fieldName, "string", "RFCTYPE_DATE", err)
		}
		return d, nil

	case C.RFCTYPE_TIME:
		var buf [7]C.SAP_UC
		if rc := C.RfcGetTime((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_CHAR)(unsafe.Pointer(&buf[0])), &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetTime("+fieldName+")")
		}
		s, err := sapUCNToString(&buf[0], 6)
		if err != nil {
			return nil, err
		}
		t, err := timeext.ParseTime(s, timeext.ParseOptions{AllowZero: opts.AllowZeroTime, Strict: opts.CheckTime})
		if err != nil {
			return nil, errMarshal(fieldName, "string", "RFCTYPE_TIME", err)
		}
		return t, nil

	case C.RFCTYPE_STRUCTURE:
		var sub C.RFC_STRUCTURE_HANDLE
		if rc := C.RfcGetStructure((C.RFC_FUNCTION_HANDLE)(container), cName, &sub, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetStructure("+fieldName+")")
		}
		return wrapStructure(typeDesc, sub, opts, fieldName)

	case C.RFCTYPE_TABLE:
		var tbl C.RFC_TABLE_HANDLE
		if rc := C.RfcGetTable((C.RFC_FUNCTION_HANDLE)(container), cName, &tbl, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetTable("+fieldName+")")
		}
		return wrapTable(typeDesc, tbl, opts, fieldName)

	default:
		return nil, errMarshal(fieldName, "?", backend.RFCType(rfcType).String(), nil)
	}
}

// wrapStructure decodes every field of an SDK structure into a
// map[string]any.
func wrapStructure(typeDesc C.RFC_TYPE_DESC_HANDLE, sub C.RFC_STRUCTURE_HANDLE, opts backend.InvokeOptions, parent string) (map[string]any, error) {
	var fieldCount C.uint
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetFieldCount(typeDesc, &fieldCount, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcGetFieldCount")
	}
	out := make(map[string]any, fieldCount)
	for i := C.uint(0); i < fieldCount; i++ {
		var fd C.RFC_FIELD_DESC
		if rc := C.RfcGetFieldDescByIndex(typeDesc, i, &fd, &info); rc != C.RFC_OK {
			return nil, errFromInfo(&info, "RfcGetFieldDescByIndex")
		}
		name := sapUCSliceToString(fd.name[:])
		v, err := wrapVariable(fd._type, unsafe.Pointer(sub), &fd.name[0], fd.ucLength, fd.typeDescHandle, opts, name)
		if err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

// wrapTable decodes every row of an SDK table into a slice.
func wrapTable(typeDesc C.RFC_TYPE_DESC_HANDLE, tbl C.RFC_TABLE_HANDLE, opts backend.InvokeOptions, parent string) ([]map[string]any, error) {
	var rowCount C.uint
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetRowCount(tbl, &rowCount, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcGetRowCount")
	}
	if rowCount == 0 {
		return []map[string]any{}, nil
	}

	if rc := C.RfcMoveToFirstRow(tbl, &info); rc != C.RFC_OK {
		return nil, errFromInfo(&info, "RfcMoveToFirstRow")
	}
	out := make([]map[string]any, 0, rowCount)
	for i := C.uint(0); i < rowCount; i++ {
		row := C.RfcGetCurrentRow(tbl, &info)
		if row == nil {
			return nil, errFromInfo(&info, "RfcGetCurrentRow")
		}
		m, err := wrapStructure(typeDesc, row, opts, parent)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		if i+1 < rowCount {
			if rc := C.RfcMoveToNextRow(tbl, &info); rc != C.RFC_OK {
				return nil, errFromInfo(&info, "RfcMoveToNextRow")
			}
		}
	}
	return out, nil
}

// trimTrailing strips trailing space and U+0000 from a CHAR value.
func trimTrailing(s string) string {
	for len(s) > 0 {
		c := s[len(s)-1]
		if c == ' ' || c == 0 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
