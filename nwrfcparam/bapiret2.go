// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package nwrfcparam holds RFC parameter helpers that simplify
// common SAP-side conventions: BAPIRet2 lists, dialog message
// classification, header/return splitting.
//
// Lives in its own subpackage so users who never call BAPIs can
// avoid the import overhead.
package nwrfcparam

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cjordaoc/gorfc/nwrfc"
)

// BAPIReturn is the Go shape of a single BAPIRET2 row. Field
// names match ABAP DDIC (BAPIRET2 structure) verbatim except
// for casing.
type BAPIReturn struct {
	Type       string `rfc:"TYPE"`        // "S","I","W","E","A","X" (success / info / warning / error / abort / dump)
	ID         string `rfc:"ID"`          // Message class
	Number     string `rfc:"NUMBER"`      // Message number
	Message    string `rfc:"MESSAGE"`     // Human-readable text
	LogNo      string `rfc:"LOG_NO"`
	LogMsgNo   string `rfc:"LOG_MSG_NO"`
	MessageV1  string `rfc:"MESSAGE_V1"`
	MessageV2  string `rfc:"MESSAGE_V2"`
	MessageV3  string `rfc:"MESSAGE_V3"`
	MessageV4  string `rfc:"MESSAGE_V4"`
	Parameter  string `rfc:"PARAMETER"`
	Row        int    `rfc:"ROW"`
	Field      string `rfc:"FIELD"`
	System     string `rfc:"SYSTEM"`
}

// IsError reports whether r is an Error / Abort / Dump-typed
// return. The conventional BAPI semantics:
//
//   - "S" success
//   - "I" info  (non-blocking)
//   - "W" warning (non-blocking)
//   - "E" error (caller must rollback)
//   - "A" abort (caller must rollback; dialog terminated)
//   - "X" runtime error / short dump
func (r BAPIReturn) IsError() bool {
	switch r.Type {
	case "E", "A", "X":
		return true
	}
	return false
}

// IsWarning reports whether r is a non-blocking warning ("W").
// Callers may treat warnings as success after logging.
func (r BAPIReturn) IsWarning() bool { return r.Type == "W" }

// LogValue redacts MessageV1..V4 because they often carry
// business data (account numbers, document IDs, names). Code/
// ID/Type/Number are emitted because they identify the failure
// shape, not the payload.
func (r BAPIReturn) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("type", r.Type),
		slog.String("id", r.ID),
		slog.String("number", r.Number),
	}
	if r.Field != "" {
		attrs = append(attrs, slog.String("field", r.Field))
	}
	for i, v := range []string{r.MessageV1, r.MessageV2, r.MessageV3, r.MessageV4} {
		if v != "" {
			attrs = append(attrs, slog.String(fmt.Sprintf("v%d", i+1), "«redacted»"))
		}
	}
	return slog.GroupValue(attrs...)
}

// FromCallParams reads a single BAPIReturn or a slice of them
// from a [backend.CallParams]-shaped raw response.
//
// Two ABAP idioms are accepted:
//
//   - Single RETURN structure (one BAPIRET2 row): the param
//     value is a map[string]any.
//   - Multiple RETURN rows (TABLES parameter): the value is a
//     slice of map[string]any.
//
// Returns (nil, nil) when raw is nil or the wrong shape.
func ParseBAPIReturn(raw any) ([]BAPIReturn, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case map[string]any:
		one := unmarshalRow(v)
		return []BAPIReturn{one}, nil
	case []map[string]any:
		out := make([]BAPIReturn, len(v))
		for i, row := range v {
			out[i] = unmarshalRow(row)
		}
		return out, nil
	case []any:
		out := make([]BAPIReturn, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, unmarshalRow(m))
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("nwrfcparam: BAPIReturn unsupported shape %T", raw)
	}
}

func unmarshalRow(m map[string]any) BAPIReturn {
	get := func(k string) string {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getInt := func(k string) int {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case int:
				return x
			case int64:
				return int(x)
			case int32:
				return int(x)
			}
		}
		return 0
	}
	return BAPIReturn{
		Type:      get("TYPE"),
		ID:        get("ID"),
		Number:    get("NUMBER"),
		Message:   get("MESSAGE"),
		LogNo:     get("LOG_NO"),
		LogMsgNo:  get("LOG_MSG_NO"),
		MessageV1: get("MESSAGE_V1"),
		MessageV2: get("MESSAGE_V2"),
		MessageV3: get("MESSAGE_V3"),
		MessageV4: get("MESSAGE_V4"),
		Parameter: get("PARAMETER"),
		Row:       getInt("ROW"),
		Field:     get("FIELD"),
		System:    get("SYSTEM"),
	}
}

// AsError converts a slice of BAPIReturn to an error if any row
// satisfies [BAPIReturn.IsError]. The returned error joins
// every error-typed row, so [errors.Is] / [errors.As] still
// work on individual entries.
func AsError(rows []BAPIReturn) error {
	var errs []error
	for _, r := range rows {
		if r.IsError() {
			errs = append(errs, asABAPApplicationError(r))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

// asABAPApplicationError synthesizes a typed
// *nwrfc.ABAPApplicationError matching the BAPIReturn row.
// The Function field is populated from the calling context if
// known; this helper expects callers to override it post-hoc
// when they know the RFM name.
func asABAPApplicationError(r BAPIReturn) error {
	return &nwrfc.ABAPApplicationError{
		SDKErrorInfo: nwrfc.SDKErrorInfo{
			Code:          1, // synthetic; the row is a SAP-side return, not an SDK error
			Key:           strings.Join([]string{"BAPI_RETURN", r.Type, r.ID, r.Number}, "/"),
			Message:       r.Message,
			AbapMsgClass:  r.ID,
			AbapMsgType:   r.Type,
			AbapMsgNumber: r.Number,
			AbapMsgV1:     r.MessageV1,
			AbapMsgV2:     r.MessageV2,
			AbapMsgV3:     r.MessageV3,
			AbapMsgV4:     r.MessageV4,
		},
	}
}

// CheckRETURN is a one-liner that turns the conventional
// "RETURN" parameter on a call response into an error:
//
//	resp, err := nwrfc.CallMap(ctx, conn, "BAPI_USER_GET_DETAIL", in)
//	if err != nil { return err }
//	if err := nwrfcparam.CheckRETURN(resp); err != nil { return err }
//
// Looks up "RETURN" (and "RET" as a fallback for older BAPIs).
func CheckRETURN(resp map[string]any) error {
	for _, key := range []string{"RETURN", "RET"} {
		raw, ok := resp[key]
		if !ok {
			continue
		}
		rows, err := ParseBAPIReturn(raw)
		if err != nil {
			return err
		}
		if e := AsError(rows); e != nil {
			return e
		}
	}
	return nil
}
