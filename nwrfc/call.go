// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// CallOptions extends [backend.InvokeOptions] with public-API
// shapes (e.g. typed RStrip pointer, future Decimal interface).
// Empty value is the safe default.
type CallOptions struct {
	// NotRequested lists ABAP parameter names to mark inactive
	// before the call (RfcSetParameterActive).
	NotRequested []string

	// ReturnImportParams echoes IMPORT parameters back into
	// the result map (PyRFC-compat flag).
	ReturnImportParams bool

	// RStrip controls trailing-space stripping on CHAR /
	// fixed-length scalar reads. Default behavior is to
	// strip; pass *bool(false) to keep the trailing padding.
	RStrip *bool

	// AllowZeroDate / AllowZeroTime suppress the explicit
	// zero-date / zero-time errors and return the Go zero
	// value instead. Default is to fail explicitly per
	// AGENTS.md "no silent fallback".
	AllowZeroDate bool
	AllowZeroTime bool

	// CheckDate / CheckTime parse strictly and reject
	// malformed payloads.
	CheckDate bool
	CheckTime bool
}

func (o CallOptions) toBackend() backend.InvokeOptions {
	return backend.InvokeOptions{
		NotRequested:       o.NotRequested,
		ReturnImportParams: o.ReturnImportParams,
		RStrip:             o.RStrip,
		AllowZeroDate:      o.AllowZeroDate,
		AllowZeroTime:      o.AllowZeroTime,
		CheckDate:          o.CheckDate,
		CheckTime:          o.CheckTime,
	}
}

// Call invokes an RFC function with a typed Go input and writes
// the EXPORT/CHANGING/TABLES/RETURN parameters into out.
//
//	type In  struct { ReqText string `rfc:"REQUTEXT"` }
//	type Out struct {
//	    EchoText string `rfc:"ECHOTEXT"`
//	    RespText string `rfc:"RESPTEXT"`
//	}
//	var out Out
//	res, err := nwrfc.Call(ctx, conn, "STFC_CONNECTION", In{ReqText: "ping"}, &out)
//
// The first return value is the raw [backend.CallParams] map
// containing every result the SDK returned, useful when the
// caller needs to peek at parameters not modeled in out.
//
// in may be a struct, *struct, [backend.CallParams], or
// map[string]any. out, when non-nil, must be a *struct.
//
// Pass [CallOptions]{} for default behavior, or the typed
// shape for per-call overrides.
func Call(ctx context.Context, c *Conn, fn string, in any, out any, optsOpt ...CallOptions) (backend.CallParams, error) {
	if c == nil {
		return nil, &BrokenConnectionError{Reason: "nil Conn", Cause: ErrConnClosed}
	}
	if !c.Alive() {
		return nil, &BrokenConnectionError{Reason: "closed Conn", Cause: ErrConnClosed}
	}

	var opts CallOptions
	if len(optsOpt) > 0 {
		opts = optsOpt[0]
	}

	inMap, err := marshalInput(in)
	if err != nil {
		return nil, err
	}

	c.Lock()
	defer c.Unlock()
	resp, err := c.backend.Invoke(ctx, c.handle, fn, inMap, opts.toBackend())
	if err != nil {
		// Translate backend.SDKError → typed RFCError. The
		// cycle-break refactor moved typed-construction here
		// from sdkbackend.
		return nil, mapBackendError(err)
	}
	// Go-side throughput fallback: count the successful call.
	// Bytes/timing stay zero here — those require the SDK-side
	// counters (see [Throughput.Attach]).
	if c.tp != nil {
		c.tp.observe(0, 0, 0, 0)
	}
	if out != nil {
		if err := unmarshalOutput(resp, out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

// CallMap is the dynamic-shape sibling of [Call]. Equivalent to
// passing a [backend.CallParams] in and out.
func CallMap(ctx context.Context, c *Conn, fn string, in backend.CallParams, optsOpt ...CallOptions) (backend.CallParams, error) {
	return Call(ctx, c, fn, in, nil, optsOpt...)
}

// marshalInput converts in (struct / *struct / map / nil) to
// the dynamic [backend.CallParams] map. Struct fields use the
// rfc:"" tag; missing tags upper-case the Go field name.
func marshalInput(in any) (backend.CallParams, error) {
	if in == nil {
		return backend.CallParams{}, nil
	}
	if cp, ok := in.(backend.CallParams); ok {
		return cp, nil
	}
	if m, ok := in.(map[string]any); ok {
		return backend.CallParams(m), nil
	}
	v := reflect.ValueOf(in)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return backend.CallParams{}, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, &MarshalError{
			FieldName: "(input)",
			GoType:    fmt.Sprintf("%T", in),
			ABAPType:  "(struct)",
			Reason:    fmt.Errorf("expected struct, map, or backend.CallParams"),
		}
	}
	ti := inspectStruct(v.Type())
	out := make(backend.CallParams, len(ti.Fields))
	for _, fi := range ti.Fields {
		fv := v.FieldByIndex(fi.Index)
		if fi.OmitEmpty && isEmpty(fv) {
			continue
		}
		val, err := marshalGoValue(fv)
		if err != nil {
			return nil, &MarshalError{
				FieldName: fi.ABAPName,
				GoType:    fv.Type().String(),
				ABAPType:  "(any)",
				Reason:    err,
			}
		}
		out[fi.ABAPName] = val
	}
	return out, nil
}

// marshalGoValue converts one struct field to the value the
// backend expects. Nested structs become map[string]any;
// nested slices become []any (homogeneous-shape preserved).
func marshalGoValue(v reflect.Value) (any, error) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		// Check for time.Time / Date / Time / similar — they
		// are not RFC structures; pass through as-is and let
		// the backend coerce.
		switch v.Interface().(type) {
		case backend.Date, backend.Time:
			return v.Interface(), nil
		}
		// Generic struct → map[string]any.
		ti := inspectStruct(v.Type())
		m := make(map[string]any, len(ti.Fields))
		for _, fi := range ti.Fields {
			fv := v.FieldByIndex(fi.Index)
			if fi.OmitEmpty && isEmpty(fv) {
				continue
			}
			val, err := marshalGoValue(fv)
			if err != nil {
				return nil, err
			}
			m[fi.ABAPName] = val
		}
		return m, nil
	case reflect.Slice, reflect.Array:
		// []byte stays as []byte (BYTE / XSTRING).
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Bytes(), nil
		}
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			val, err := marshalGoValue(v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	default:
		return v.Interface(), nil
	}
}

// unmarshalOutput writes the dynamic [backend.CallParams] into
// the typed *struct destination. out must be a non-nil pointer
// to a struct.
func unmarshalOutput(resp backend.CallParams, out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return &MarshalError{FieldName: "(output)", GoType: fmt.Sprintf("%T", out), ABAPType: "(struct)", Reason: fmt.Errorf("expected non-nil *struct")}
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return &MarshalError{FieldName: "(output)", GoType: v.Type().String(), ABAPType: "(struct)", Reason: fmt.Errorf("expected pointer to struct")}
	}
	ti := inspectStruct(v.Type())
	for _, fi := range ti.Fields {
		raw, ok := resp[fi.ABAPName]
		if !ok || raw == nil {
			continue
		}
		fv := v.FieldByIndex(fi.Index)
		if !fv.CanSet() {
			continue
		}
		if err := unmarshalValue(raw, fv); err != nil {
			return &MarshalError{FieldName: fi.ABAPName, GoType: fv.Type().String(), ABAPType: fmt.Sprintf("%T", raw), Reason: err}
		}
	}
	return nil
}

func unmarshalValue(raw any, dst reflect.Value) error {
	rv := reflect.ValueOf(raw)
	if !rv.IsValid() {
		return nil
	}
	// Direct assignable type — fast path.
	if rv.Type().AssignableTo(dst.Type()) {
		dst.Set(rv)
		return nil
	}
	// Pointer destinations.
	if dst.Kind() == reflect.Pointer {
		nv := reflect.New(dst.Type().Elem())
		if err := unmarshalValue(raw, nv.Elem()); err != nil {
			return err
		}
		dst.Set(nv)
		return nil
	}
	// Convertible (e.g. int64 → int, int → int32).
	if rv.Type().ConvertibleTo(dst.Type()) {
		dst.Set(rv.Convert(dst.Type()))
		return nil
	}
	// Struct destination from map[string]any.
	if dst.Kind() == reflect.Struct {
		if m, ok := raw.(map[string]any); ok {
			ti := inspectStruct(dst.Type())
			for _, fi := range ti.Fields {
				val, ok := m[fi.ABAPName]
				if !ok {
					continue
				}
				if err := unmarshalValue(val, dst.FieldByIndex(fi.Index)); err != nil {
					return err
				}
			}
			return nil
		}
	}
	// Slice destination.
	if dst.Kind() == reflect.Slice {
		if rv.Kind() == reflect.Slice {
			out := reflect.MakeSlice(dst.Type(), rv.Len(), rv.Len())
			for i := 0; i < rv.Len(); i++ {
				if err := unmarshalValue(rv.Index(i).Interface(), out.Index(i)); err != nil {
					return err
				}
			}
			dst.Set(out)
			return nil
		}
	}
	return fmt.Errorf("cannot assign %s to %s", rv.Type(), dst.Type())
}
