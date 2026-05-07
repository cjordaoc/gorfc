// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"reflect"
	"strings"
	"sync"
)

// tagName is the struct tag this package recognizes:
//
//	type In struct {
//	    Name string `rfc:"REQUTEXT"`
//	    Lang string `rfc:"LANG,omitempty"`
//	    Body []byte `rfc:"-"`         // skipped
//	}
//
// Empty tag falls back to the upper-cased Go field name; a tag
// of "-" skips the field entirely. The first comma-separated
// option is the ABAP parameter or field name; subsequent
// options are flags ("omitempty", "rstrip", ...).
const tagName = "rfc"

// fieldInfo is the parsed view of a single struct field.
type fieldInfo struct {
	// Index is the path through nested anonymous fields, as
	// understood by reflect.Value.FieldByIndex.
	Index []int
	// ABAPName is the parameter / field name the SDK expects.
	// Always uppercase by convention; the parser does not
	// upper-case automatically — it only upper-cases the
	// derived default when no tag was supplied.
	ABAPName string
	// OmitEmpty is true when the struct tag carried
	// ",omitempty"; the marshaler skips the field when its Go
	// value is the zero value.
	OmitEmpty bool
}

// typeInfo is the cached marshaling plan for a struct type.
type typeInfo struct {
	Fields []fieldInfo
	// ByABAPName lets the unmarshaler look up the destination
	// field by ABAP name in O(1).
	ByABAPName map[string]int
}

// typeInfoCache memoizes parsed struct metadata per
// reflect.Type. Reset by tests if needed via cacheReset.
var (
	typeInfoCacheMu sync.RWMutex
	typeInfoCache   = make(map[reflect.Type]*typeInfo)
)

// inspectStruct returns the parsed metadata for t. t must
// have Kind == reflect.Struct; the public callers (marshal/
// unmarshal in call.go) ensure this. The result is cached
// indefinitely; reflect.Type values are stable for the life
// of the process so cache entries never go stale.
func inspectStruct(t reflect.Type) *typeInfo {
	typeInfoCacheMu.RLock()
	if ti, ok := typeInfoCache[t]; ok {
		typeInfoCacheMu.RUnlock()
		return ti
	}
	typeInfoCacheMu.RUnlock()

	ti := buildTypeInfo(t, nil)
	typeInfoCacheMu.Lock()
	typeInfoCache[t] = ti
	typeInfoCacheMu.Unlock()
	return ti
}

func buildTypeInfo(t reflect.Type, parentIndex []int) *typeInfo {
	out := &typeInfo{
		ByABAPName: make(map[string]int),
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		idx := append(append([]int{}, parentIndex...), i)
		tag := sf.Tag.Get(tagName)
		if tag == "-" {
			continue
		}
		// Anonymous (embedded) struct: recurse and inline its
		// fields, matching the encoding/json idiom.
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct && tag == "" {
			child := buildTypeInfo(sf.Type, idx)
			for _, cf := range child.Fields {
				out.ByABAPName[cf.ABAPName] = len(out.Fields)
				out.Fields = append(out.Fields, cf)
			}
			continue
		}
		fi := parseTag(tag, sf.Name)
		fi.Index = idx
		out.ByABAPName[fi.ABAPName] = len(out.Fields)
		out.Fields = append(out.Fields, fi)
	}
	return out
}

// parseTag splits an `rfc:"..."` tag into a [fieldInfo].
//
//	`rfc:"REQUTEXT"`            → ABAPName="REQUTEXT"
//	`rfc:",omitempty"`          → ABAPName=upper(field), OmitEmpty=true
//	`rfc:"LANG,omitempty"`      → ABAPName="LANG", OmitEmpty=true
//	`` (no tag)                 → ABAPName=upper(field)
//	`rfc:"-"`                   → caller skips entirely
func parseTag(tag, fieldName string) fieldInfo {
	fi := fieldInfo{}
	if tag == "" {
		fi.ABAPName = strings.ToUpper(fieldName)
		return fi
	}
	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		fi.ABAPName = parts[0]
	} else {
		fi.ABAPName = strings.ToUpper(fieldName)
	}
	for _, opt := range parts[1:] {
		switch strings.TrimSpace(opt) {
		case "omitempty":
			fi.OmitEmpty = true
		}
	}
	return fi
}

// isEmpty reports whether v is the zero value for its type;
// used by OmitEmpty.
func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	}
	return false
}
