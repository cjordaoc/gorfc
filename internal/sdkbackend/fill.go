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
	"reflect"
	"time"
	"unsafe"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/internal/timeext"
)

// fillFunctionParameters writes every key in the [backend.CallParams]
// map into the SDK function container. Unknown keys (not part
// of the function descriptor) trigger a *MarshalError.
//
// Bug fix vs upstream: the previous fillVariable leaked
// SAP_UC allocations because of defer-capture-before-assignment
// (see T0.3 commit message). This implementation uses paired
// nil-guarded closures and the goFreeU paired allocator.
func fillFunctionParameters(fh C.RFC_FUNCTION_HANDLE, desc C.RFC_FUNCTION_DESC_HANDLE, in backend.CallParams) error {
	for name, value := range in {
		if err := fillFunctionParameter(fh, desc, name, value); err != nil {
			return err
		}
	}
	return nil
}

func fillFunctionParameter(fh C.RFC_FUNCTION_HANDLE, desc C.RFC_FUNCTION_DESC_HANDLE, name string, value any) error {
	nameUC, err := stringToSAPUC(name)
	if err != nil {
		return err
	}
	defer C.goFreeU(nameUC)

	var pd C.RFC_PARAMETER_DESC
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetParameterDescByName(desc, nameUC, &pd, &info); rc != C.RFC_OK {
		return errFromInfo(&info, "RfcGetParameterDescByName("+name+")")
	}
	return fillVariable(pd._type, fh, &pd.name[0], value, pd.typeDescHandle, name)
}

// fillVariable writes a single ABAP scalar / structure / table.
// container is the function (top-level), structure, or table
// row handle.
func fillVariable(rfcType C.RFCTYPE, container unsafe.Pointer, cName *C.SAP_UC, value any, typeDesc C.RFC_TYPE_DESC_HANDLE, fieldName string) (err error) {
	var info C.RFC_ERROR_INFO
	switch rfcType {
	case C.RFCTYPE_CHAR, C.RFCTYPE_STRING, C.RFCTYPE_NUM,
		C.RFCTYPE_BCD, C.RFCTYPE_FLOAT, C.RFCTYPE_DECF16,
		C.RFCTYPE_DECF34, C.RFCTYPE_UTCLONG:
		s, err := coerceToString(value, rfcType)
		if err != nil {
			return errMarshal(fieldName, fmt.Sprintf("%T", value), backend.RFCType(rfcType).String(), err)
		}
		return fillStringLike(rfcType, container, cName, s, fieldName)

	case C.RFCTYPE_BYTE, C.RFCTYPE_XSTRING:
		b, ok := value.([]byte)
		if !ok {
			return errMarshal(fieldName, fmt.Sprintf("%T", value), backend.RFCType(rfcType).String(), fmt.Errorf("expected []byte"))
		}
		var rc C.RFC_RC
		if len(b) > 0 {
			ptr := (*C.SAP_RAW)(C.CBytes(b))
			defer C.free(unsafe.Pointer(ptr))
			if rfcType == C.RFCTYPE_BYTE {
				rc = C.RfcSetBytes((C.RFC_FUNCTION_HANDLE)(container), cName, ptr, C.uint(len(b)), &info)
			} else {
				rc = C.RfcSetXString((C.RFC_FUNCTION_HANDLE)(container), cName, ptr, C.uint(len(b)), &info)
			}
		}
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetBytes/XString("+fieldName+")")
		}
		return nil

	case C.RFCTYPE_INT1, C.RFCTYPE_INT2, C.RFCTYPE_INT, C.RFCTYPE_INT8:
		n, err := coerceToInt64(value)
		if err != nil {
			return errMarshal(fieldName, fmt.Sprintf("%T", value), backend.RFCType(rfcType).String(), err)
		}
		rc := C.RfcSetInt((C.RFC_FUNCTION_HANDLE)(container), cName, C.RFC_INT(n), &info)
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetInt("+fieldName+")")
		}
		return nil

	case C.RFCTYPE_DATE:
		d, err := coerceToDate(value)
		if err != nil {
			return errMarshal(fieldName, fmt.Sprintf("%T", value), "RFCTYPE_DATE", err)
		}
		s := timeext.FormatDate(d)
		uc, err := stringToSAPUC(s)
		if err != nil {
			return err
		}
		defer C.goFreeU(uc)
		rc := C.RfcSetDate((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_CHAR)(unsafe.Pointer(uc)), &info)
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetDate("+fieldName+")")
		}
		return nil

	case C.RFCTYPE_TIME:
		t, err := coerceToTime(value)
		if err != nil {
			return errMarshal(fieldName, fmt.Sprintf("%T", value), "RFCTYPE_TIME", err)
		}
		s := timeext.FormatTime(t)
		uc, err := stringToSAPUC(s)
		if err != nil {
			return err
		}
		defer C.goFreeU(uc)
		rc := C.RfcSetTime((C.RFC_FUNCTION_HANDLE)(container), cName, (*C.RFC_CHAR)(unsafe.Pointer(uc)), &info)
		if rc != C.RFC_OK {
			return errFromInfo(&info, "RfcSetTime("+fieldName+")")
		}
		return nil

	case C.RFCTYPE_STRUCTURE:
		var sub C.RFC_STRUCTURE_HANDLE
		if rc := C.RfcGetStructure((C.RFC_FUNCTION_HANDLE)(container), cName, &sub, &info); rc != C.RFC_OK {
			return errFromInfo(&info, "RfcGetStructure("+fieldName+")")
		}
		return fillStructure(typeDesc, sub, value, fieldName)

	case C.RFCTYPE_TABLE:
		var tbl C.RFC_TABLE_HANDLE
		if rc := C.RfcGetTable((C.RFC_FUNCTION_HANDLE)(container), cName, &tbl, &info); rc != C.RFC_OK {
			return errFromInfo(&info, "RfcGetTable("+fieldName+")")
		}
		return fillTable(typeDesc, tbl, value, fieldName)

	default:
		return errMarshal(fieldName, fmt.Sprintf("%T", value), backend.RFCType(rfcType).String(), backend.ErrUnknownType)
	}
}

// fillStringLike writes a string-shaped scalar. Centralizes
// the SAP_UC alloc + free + RfcSet* dispatch.
func fillStringLike(rfcType C.RFCTYPE, container unsafe.Pointer, cName *C.SAP_UC, s, fieldName string) error {
	uc, err := stringToSAPUC(s)
	if err != nil {
		return err
	}
	defer C.goFreeU(uc)
	cLen := C.goStrlenU(uc)

	var info C.RFC_ERROR_INFO
	var rc C.RFC_RC
	fh := (C.RFC_FUNCTION_HANDLE)(container)
	switch rfcType {
	case C.RFCTYPE_CHAR:
		rc = C.RfcSetChars(fh, cName, (*C.RFC_CHAR)(unsafe.Pointer(uc)), cLen, &info)
	case C.RFCTYPE_NUM:
		rc = C.RfcSetNum(fh, cName, (*C.RFC_NUM)(unsafe.Pointer(uc)), cLen, &info)
	default: // STRING, BCD, FLOAT, DECF16, DECF34, UTCLONG → RfcSetString.
		rc = C.RfcSetString(fh, cName, uc, cLen, &info)
	}
	if rc != C.RFC_OK {
		return errFromInfo(&info, "RfcSet*("+fieldName+")")
	}
	return nil
}

// fillStructure walks a Go struct or map[string]any and sets
// each field in the SDK structure handle.
func fillStructure(typeDesc C.RFC_TYPE_DESC_HANDLE, sub C.RFC_STRUCTURE_HANDLE, value any, parentField string) error {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return errMarshal(parentField, v.Type().String(), "RFCTYPE_STRUCTURE", fmt.Errorf("non-string key %v", key))
			}
			if err := fillStructureField(typeDesc, sub, key.String(), iter.Value().Interface()); err != nil {
				return err
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			fname := t.Field(i).Name
			if tag := t.Field(i).Tag.Get("rfc"); tag != "" {
				fname = tag
			}
			if err := fillStructureField(typeDesc, sub, fname, v.Field(i).Interface()); err != nil {
				return err
			}
		}
	default:
		return errMarshal(parentField, v.Type().String(), "RFCTYPE_STRUCTURE", fmt.Errorf("expected map or struct"))
	}
	return nil
}

func fillStructureField(typeDesc C.RFC_TYPE_DESC_HANDLE, sub C.RFC_STRUCTURE_HANDLE, fname string, fvalue any) error {
	uc, err := stringToSAPUC(fname)
	if err != nil {
		return err
	}
	defer C.goFreeU(uc)

	var fd C.RFC_FIELD_DESC
	var info C.RFC_ERROR_INFO
	if rc := C.RfcGetFieldDescByName(typeDesc, uc, &fd, &info); rc != C.RFC_OK {
		return errFromInfo(&info, "RfcGetFieldDescByName("+fname+")")
	}
	return fillVariable(fd._type, unsafe.Pointer(sub), &fd.name[0], fvalue, fd.typeDescHandle, fname)
}

// fillTable appends a row for each element of the slice and
// sets the row's fields.
func fillTable(typeDesc C.RFC_TYPE_DESC_HANDLE, tbl C.RFC_TABLE_HANDLE, value any, parentField string) error {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return errMarshal(parentField, v.Type().String(), "RFCTYPE_TABLE", fmt.Errorf("expected slice/array"))
	}
	for i := 0; i < v.Len(); i++ {
		var info C.RFC_ERROR_INFO
		row := C.RfcAppendNewRow(tbl, &info)
		if row == nil {
			return errFromInfo(&info, "RfcAppendNewRow")
		}
		if err := fillStructure(typeDesc, row, v.Index(i).Interface(), parentField); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================
// Coercion helpers — convert Go values to the canonical wire form
// =============================================================

func coerceToString(v any, _ C.RFCTYPE) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	case fmt.Stringer:
		return x.String(), nil
	default:
		// Last-resort numeric → string for FLOAT/BCD/DECF.
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return fmt.Sprintf("%d", rv.Int()), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return fmt.Sprintf("%d", rv.Uint()), nil
		case reflect.Float32, reflect.Float64:
			return fmt.Sprintf("%g", rv.Float()), nil
		}
		return "", fmt.Errorf("cannot convert %T to string", v)
	}
}

func coerceToInt64(v any) (int64, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint()), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func coerceToDate(v any) (backend.Date, error) {
	switch x := v.(type) {
	case backend.Date:
		return x, nil
	case time.Time:
		return backend.Date{Year: uint16(x.Year()), Month: uint16(x.Month()), Day: uint16(x.Day())}, nil
	case string:
		return timeext.ParseDate(x, timeext.ParseOptions{AllowZero: true})
	}
	return backend.Date{}, fmt.Errorf("cannot convert %T to Date", v)
}

func coerceToTime(v any) (backend.Time, error) {
	switch x := v.(type) {
	case backend.Time:
		return x, nil
	case time.Time:
		return backend.Time{Hour: uint8(x.Hour()), Minute: uint8(x.Minute()), Second: uint8(x.Second())}, nil
	case string:
		return timeext.ParseTime(x, timeext.ParseOptions{AllowZero: true})
	}
	return backend.Time{}, fmt.Errorf("cannot convert %T to Time", v)
}
