// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/internal/bcd"
	"github.com/cjordaoc/gorfc/internal/timeext"
)

// Public re-exports of the ABAP scalar types the marshaling
// layer understands.
//
// Until v0.2.0 these lived in `internal/`, and consumers wanting
// to declare a struct field of ABAP DATS / TIMS / UTCLONG /
// DECF type had to import the internal package — which Go
// disallows for callers outside the gorfc module. The aliases
// below re-export the underlying types at the public surface so
// the typical pattern just works:
//
//	type MyResponse struct {
//	    OrderDate    nwrfc.Date    `rfc:"ERDAT"`
//	    OrderTime    nwrfc.Time    `rfc:"ERZET"`
//	    Amount       nwrfc.Decimal `rfc:"NETWR"` // string-backed
//	    LastEditedAt nwrfc.UTCLong `rfc:"LAEDA"`
//	}
//
// Aliases (Go 1.9+ `type X = Y`) are used rather than wrapper
// structs so that:
//
//   - Methods on the underlying type are reachable directly on
//     the alias (no manual forwarding needed).
//   - Reflection-based marshaling (`internal/sdkbackend/fill.go`,
//     `wrap.go`) sees the same `reflect.Type` for the public
//     and internal name; no type assertions break.
//   - `interface{ AsTime() time.Time }` style ducking still
//     works without the alias adding extra layers.
//
// The downside: an alias cannot grow methods that the
// underlying type does not have. Where a public-only helper is
// useful, we add it as a free function below (e.g. [DateFromTime]).

// Date is the ABAP DATS scalar (8 chars, "YYYYMMDD"). The zero
// value is the ABAP "00000000" initial date; the marshaling
// layer rejects it with [ErrZeroDate] unless the caller opts
// into [CallOptions.AllowZeroDate].
//
// Aliases [backend.Date]; methods including [Date.IsZero] are
// inherited.
type Date = backend.Date

// Time is the ABAP TIMS scalar (6 chars, "HHMMSS"). Zero value
// is "000000"; rejected with [ErrZeroTime] unless the caller
// opts into [CallOptions.AllowZeroTime].
//
// Aliases [backend.Time]; methods including [Time.IsZero] are
// inherited.
type Time = backend.Time

// UTCLong is the ABAP UTCLONG scalar (21 → 27-char canonical
// form, 100 ns precision). Available on AS ABAP 7.50+ +
// SAP NW RFC SDK 7.50+.
//
// Aliases [timeext.UTCLong]; methods (`String`, `IsZero`,
// `MarshalText`, `UnmarshalText`) are inherited from the
// internal type.
type UTCLong = timeext.UTCLong

// Decimal is the interface implemented by user code that wants
// to swap the default string-based decimal for a typed value
// (apd.Decimal, shopspring/decimal.Decimal, …). The interface is
// intentionally minimal — `String()` is the bridge to and from
// the SDK wire format; consumers do their math in their own
// type.
//
// Aliases [bcd.Decimal]. Default policy stays string-based so
// the built-in marshaling has no third-party dependency; the
// alias is the bridge for callers who already use a decimal
// library.
type Decimal = bcd.Decimal

// AsTime composes Date + Time into a [time.Time] in UTC. Useful
// for callers who want a single timestamp value out of two ABAP
// fields (`ERDAT`, `ERZET`).
//
// Free function rather than a method because the underlying
// types live in `internal/backend`; we cannot add methods to a
// type alias.
//
// Returns the zero [time.Time] when both inputs are zero
// (matches the upstream lenient policy when
// `CallOptions.AllowZeroDate` was set).
//
// Convenience wrapper around [backend.AsTime]; preserved here
// so callers do not need to import `internal/backend`.
var AsTime = backend.AsTime
