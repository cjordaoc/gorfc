// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"fmt"
	"log/slog"
	"time"
)

// ConnHandle is an opaque connection identifier. Backends are free
// to choose any non-zero representation (a pointer wrapper, a map
// key, a numeric ID); callers MUST treat the value as opaque and
// pass it back to the same backend that issued it.
//
// The zero value is reserved for "invalid handle". Backends MUST
// reject it with an error of category [CategoryBrokenConn].
type ConnHandle uint64

// Params is the connection parameter set passed to [Backend.Open].
// The keys match the SAP NWRFC SDK parameter names verbatim
// (lowercase, e.g. "ashost", "user", "passwd", "snc_qop"). The
// public `nwrfc.Params` struct in the consumer package serializes
// itself to this shape so the backend layer stays string-keyed.
//
// Params values that may carry credentials (see [SensitiveKeys])
// are redacted by [Params.LogValue]; never log a Params via the
// default `%v` verb.
type Params map[string]string

// SensitiveKeys lists Params keys whose values must be redacted
// in any log, span attribute, or error message. The list is
// ordered by likelihood of operator confusion (password first,
// then ticket variants, then crypto material).
//
// This list is the authoritative redaction whitelist; both the
// public `nwrfc/errors.go` (T1.3) and `nwrfcotel/redact.go`
// (T2.3) consult it.
var SensitiveKeys = []string{
	"passwd",
	"password",
	"mysapsso2",
	"x509cert",
	"snc_myname",
	"snc_partnername",
	"tls_client_pse",
	"tls_trust_all",
	"saml2",
	"bearer",
}

// LogValue implements [slog.LogValuer], redacting any
// [SensitiveKeys] before emitting. Use:
//
//	slog.Info("connecting", "params", params)
//
// to get a redacted JSON object instead of the raw map.
func (p Params) LogValue() slog.Value {
	if len(p) == 0 {
		return slog.GroupValue()
	}
	attrs := make([]slog.Attr, 0, len(p))
	for k, v := range p {
		if isSensitiveKey(k) {
			attrs = append(attrs, slog.String(k, "«redacted»"))
			continue
		}
		attrs = append(attrs, slog.String(k, v))
	}
	return slog.GroupValue(attrs...)
}

func isSensitiveKey(k string) bool {
	for _, s := range SensitiveKeys {
		if s == k {
			return true
		}
	}
	return false
}

// CallParams is the dynamic shape of an RFC parameter set. Keys
// are uppercase ABAP parameter names (`REQUTEXT`, `IMPORTSTRUCT`).
// Values are Go values that map to the ABAP type:
//
//   - string for CHAR / STRING / NUM / BCD / DECF / UTCLONG (the
//     decimal-as-string default; opt-in to a Decimal interface
//     in the public API).
//   - int / int8 / int16 / int32 / int64 (and unsigned for
//     INT1) for INT family.
//   - float64 for FLOAT.
//   - []byte for BYTE / XSTRING.
//   - [Date] / [Time] for DATS / TIMS (zero values fail unless
//     [InvokeOptions.AllowZeroDate] / AllowZeroTime is set).
//   - map[string]any for nested STRUCTURE.
//   - []any (or []map[string]any, []SomeStruct) for TABLE.
//
// The marshaling layer in `nwrfc/` converts struct-tagged Go
// types into this map shape (T1.8) and back.
type CallParams map[string]any

// InvokeOptions controls per-call behavior. Zero value is the
// safe default (strict, all params requested, no return-imports
// echo).
type InvokeOptions struct {
	// NotRequested lists ABAP parameter names that should be
	// flagged inactive via `RfcSetParameterActive`. The SAP
	// system can skip computing them, saving round-trip time.
	NotRequested []string

	// ReturnImportParams echoes IMPORT parameters in the
	// returned [CallParams] (PyRFC compatibility flag).
	ReturnImportParams bool

	// RStrip trims trailing space from CHAR / NUM scalars on
	// unmarshal. Default is true, matching upstream behavior.
	RStrip *bool

	// AllowZeroDate suppresses [ErrZeroDate] for ABAP "00000000"
	// initial dates and returns the zero [Date] instead.
	AllowZeroDate bool

	// AllowZeroTime suppresses [ErrZeroTime] for ABAP "000000"
	// initial times and returns the zero [Time] instead.
	AllowZeroTime bool

	// CheckDate / CheckTime, when true, parse DATS/TIMS values
	// strictly (reject malformed payloads instead of returning
	// the raw string).
	CheckDate bool
	CheckTime bool

	// DirectionFilter restricts the parameters that are read
	// back from the SDK to the listed directions. Default is
	// EXPORT | CHANGING | TABLES | RETURN.
	DirectionFilter Direction
}

// Direction is a bitmask matching the SAP NWRFC SDK direction
// constants (`RFC_IMPORT`, `RFC_EXPORT`, `RFC_CHANGING`,
// `RFC_TABLES`, `RFC_RETURN`). Zero means "no filter (all
// directions)".
type Direction uint8

const (
	DirImport Direction = 1 << iota
	DirExport
	DirChanging
	DirTables
	DirReturn

	// DirAllResults matches the directions that are returned by
	// the SDK after [Backend.Invoke]. IMPORT is excluded
	// because it is what the caller sent.
	DirAllResults = DirExport | DirChanging | DirTables | DirReturn
)

// Version represents the SDK release reported by `RfcGetVersion`.
// The zero value is "no SDK linked".
type Version struct {
	Major      uint
	Minor      uint
	PatchLevel uint
}

// AtLeast reports whether v is at least the given (major, minor,
// patch) triple. Used to gate capabilities.
func (v Version) AtLeast(major, minor, patch uint) bool {
	if v.Major != major {
		return v.Major > major
	}
	if v.Minor != minor {
		return v.Minor > minor
	}
	return v.PatchLevel >= patch
}

// IsZero reports whether v is the zero version (no SDK linked).
func (v Version) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.PatchLevel == 0
}

// String formats v as "Major.Minor PLPatchLevel".
func (v Version) String() string {
	if v.IsZero() {
		return "no-sdk"
	}
	return fmt.Sprintf("%d.%d PL%d", v.Major, v.Minor, v.PatchLevel)
}

// Capabilities is the set of optional features the active SDK
// supports at runtime. The cgo backend populates these at Open
// time from `RfcGetVersion`; the no-SDK stub returns the zero
// value (no capabilities).
//
// Each field's minimum SDK patch level is documented in
// docs/SDK_FUNCTIONS_MAP.md; values marked 🟡 in PLAN.md are
// pending verification against the SAP NWRFC SDK Programming
// Guide.
type Capabilities struct {
	// WebSocketRFC: WSHOST/WSPORT params and `RfcLoadCryptoLibrary`
	// are usable. Available on SDK 7.50 PL10+ (🟡 verify).
	WebSocketRFC bool

	// Throughput: `RfcCreateThroughput` and friends are usable.
	// Available on SDK 7.53+ (🟡 verify).
	Throughput bool

	// BgRFC: `RfcCreateUnit` and `RfcInstallBgRfcHandlers` are
	// usable. Available on SDK 7.50 PL5+ (🟡 verify).
	BgRFC bool

	// UTCLong: native UTCLONG marshal via `RfcSetUTCLong` /
	// `RfcGetUTCLong` is usable. Available on SDK 7.50 PL0+ but
	// requires the SAP system to be on a release that supports
	// the type.
	UTCLong bool

	// FastSerialization: `cbRfc` Fast Serialization protocol is
	// usable. Available on SDK 7.50 PL11+ (🟡 verify).
	FastSerialization bool
}

// Attributes mirrors the populated fields of
// `RFC_ATTRIBUTES` after `RfcGetConnectionAttributes`. Field
// names match the SDK members (camelCased Go-side). The values
// are SAP_UC strings decoded to UTF-8 by the backend.
type Attributes struct {
	Dest                  string
	Host                  string
	PartnerHost           string
	SysNumber             string
	SysID                 string
	Client                string
	User                  string
	Language              string
	Trace                 string
	IsoLanguage           string
	Codepage              string
	PartnerCodepage       string
	RfcRole               string
	Type                  string
	PartnerType           string
	Rel                   string
	PartnerRel            string
	KernelRel             string
	CpicConvID            string
	ProgName              string
	PartnerBytesPerChar   string
	PartnerSystemCodepage string
	PartnerIP             string
	PartnerIPv6           string
}

// FunctionDescriptor is the metadata returned by [Backend.Describe].
// Used by the marshaling layer to pre-compute fill/wrap plans.
type FunctionDescriptor struct {
	Name       string
	Parameters []ParameterDescriptor
}

// ParameterDescriptor is one parameter (IMPORT/EXPORT/CHANGING/
// TABLES/RETURN) of an RFC function.
type ParameterDescriptor struct {
	Name      string
	Type      RFCType
	Direction Direction
	Length    uint
	Decimals  uint
	Optional  bool
	// TypeDesc is non-nil for STRUCTURE and TABLE parameters
	// and carries the field metadata.
	TypeDesc *TypeDescriptor
}

// TypeDescriptor is a STRUCTURE or TABLE field-level descriptor.
type TypeDescriptor struct {
	Name   string
	Fields []FieldDescriptor
}

// FieldDescriptor is one field inside a TypeDescriptor.
type FieldDescriptor struct {
	Name     string
	Type     RFCType
	Length   uint
	Decimals uint
	Offset   uint
	// TypeDesc is non-nil for nested STRUCTURE / TABLE fields.
	TypeDesc *TypeDescriptor
}

// RFCType mirrors the SAP `RFCTYPE` enum. Constants are exported
// so the marshaling layer in `nwrfc/` can switch on them
// without depending on cgo.
//
// The numeric values match the SAP NWRFC SDK header
// `<sapnwrfc.h>`; verified against the public PyRFC bindings
// (which reproduce the same numeric layout) and against the
// upstream `gorfc` test fixtures. (🟡 final verification pending
// against the SDK header in hand.)
type RFCType uint8

const (
	TypeChar      RFCType = 0  // RFCTYPE_CHAR
	TypeDate      RFCType = 1  // RFCTYPE_DATE
	TypeBCD       RFCType = 2  // RFCTYPE_BCD
	TypeTime      RFCType = 3  // RFCTYPE_TIME
	TypeByte      RFCType = 4  // RFCTYPE_BYTE
	TypeTable     RFCType = 5  // RFCTYPE_TABLE
	TypeNum       RFCType = 6  // RFCTYPE_NUM
	TypeFloat     RFCType = 7  // RFCTYPE_FLOAT
	TypeInt       RFCType = 8  // RFCTYPE_INT
	TypeInt2      RFCType = 9  // RFCTYPE_INT2
	TypeInt1      RFCType = 10 // RFCTYPE_INT1
	TypeStructure RFCType = 17 // RFCTYPE_STRUCTURE
	TypeDecF16    RFCType = 23 // RFCTYPE_DECF16
	TypeDecF34    RFCType = 24 // RFCTYPE_DECF34
	TypeXMLData   RFCType = 28 // RFCTYPE_XMLDATA
	TypeString    RFCType = 29 // RFCTYPE_STRING
	TypeXString   RFCType = 30 // RFCTYPE_XSTRING
	TypeInt8      RFCType = 31 // RFCTYPE_INT8
	TypeUTCLong   RFCType = 32 // RFCTYPE_UTCLONG
)

// String returns the SDK-style name (`RFCTYPE_CHAR` etc.) for t.
// Used in error messages and traces.
func (t RFCType) String() string {
	switch t {
	case TypeChar:
		return "RFCTYPE_CHAR"
	case TypeDate:
		return "RFCTYPE_DATE"
	case TypeBCD:
		return "RFCTYPE_BCD"
	case TypeTime:
		return "RFCTYPE_TIME"
	case TypeByte:
		return "RFCTYPE_BYTE"
	case TypeTable:
		return "RFCTYPE_TABLE"
	case TypeNum:
		return "RFCTYPE_NUM"
	case TypeFloat:
		return "RFCTYPE_FLOAT"
	case TypeInt:
		return "RFCTYPE_INT"
	case TypeInt2:
		return "RFCTYPE_INT2"
	case TypeInt1:
		return "RFCTYPE_INT1"
	case TypeStructure:
		return "RFCTYPE_STRUCTURE"
	case TypeDecF16:
		return "RFCTYPE_DECF16"
	case TypeDecF34:
		return "RFCTYPE_DECF34"
	case TypeXMLData:
		return "RFCTYPE_XMLDATA"
	case TypeString:
		return "RFCTYPE_STRING"
	case TypeXString:
		return "RFCTYPE_XSTRING"
	case TypeInt8:
		return "RFCTYPE_INT8"
	case TypeUTCLong:
		return "RFCTYPE_UTCLONG"
	default:
		return fmt.Sprintf("RFCTYPE_UNKNOWN(%d)", uint8(t))
	}
}

// Date is the ABAP DATS type (8 chars, format YYYYMMDD).
//
// The zero value is the ABAP "00000000" initial date; backends
// reject it with [ErrZeroDate] unless [InvokeOptions.AllowZeroDate]
// is set. This is a deliberate departure from the upstream
// `gorfc` behavior of silently returning a zero `time.Time`; see
// docs/PLAN.md §1.3 and the migration guide (T4.5).
type Date struct {
	Year, Month, Day uint16
}

// IsZero reports whether d is the ABAP initial date (00000000).
func (d Date) IsZero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// Time is the ABAP TIMS type (6 chars, format HHMMSS).
//
// The zero value is the ABAP "000000" initial time; backends
// reject it with [ErrZeroTime] unless
// [InvokeOptions.AllowZeroTime] is set.
type Time struct {
	Hour, Minute, Second uint8
}

// IsZero reports whether t is the ABAP initial time (000000).
func (t Time) IsZero() bool { return t.Hour == 0 && t.Minute == 0 && t.Second == 0 }

// AsTime returns a [time.Time] in UTC composed from d and t. If
// either is zero and the corresponding Allow flag was not set,
// the result is the zero [time.Time].
func AsTime(d Date, t Time) time.Time {
	return time.Date(int(d.Year), time.Month(d.Month), int(d.Day), int(t.Hour), int(t.Minute), int(t.Second), 0, time.UTC)
}
