// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include "helpers.h"
*/
import "C"

import (
	"github.com/cjordaoc/gorfc/internal/backend"
)

// Version returns the SAP NWRFC SDK version reported by
// `RfcGetVersion`.
//
// The SDK packs the release line into the `major` out-param as
// a 4-digit decimal: e.g. 7500 for the 7.50 family, 7530 for
// 7.53. The `minor` out-param has been observed as 0 on PL12+.
// We decompose the packed value so that
// [backend.Version.AtLeast]`(7, 50, 0)` does what callers
// expect (the previous code passed 7500 straight into Major,
// which made every `AtLeast(7, x, y)` return true regardless
// of x/y).
//
// SDK function: RfcGetVersion (✅ behavior verified PL18 —
// raw returns 7500/0/18 on the SDK we link against).
func (b *sdkBackend) Version() backend.Version {
	var rawMajor, rawMinor, rawPatch C.uint
	_ = rawMinor // SAP-reserved out-param; observed to be 0.
	C.RfcGetVersion(&rawMajor, &rawMinor, &rawPatch)
	r := uint(rawMajor)
	if r == 0 {
		return backend.Version{}
	}
	// Decompose 7500 -> Major=7, Minor=50; 7530 -> 7, 53.
	return backend.Version{
		Major:      r / 1000,
		Minor:      (r / 10) % 100,
		PatchLevel: uint(rawPatch),
	}
}

// Capabilities reports which optional features the loaded SDK
// supports. The thresholds below are documented in
// docs/SDK_FUNCTIONS_MAP.md; entries marked 🟡 are pending
// verification against the SAP NWRFC SDK Programming Guide.
func (b *sdkBackend) Capabilities() backend.Capabilities {
	v := b.Version()
	return backend.Capabilities{
		// 🟡 verify: WebSocket RFC was introduced around
		// 7.50 PL10 per node-rfc release notes; the
		// programming guide should confirm.
		WebSocketRFC: v.AtLeast(7, 50, 10),
		// 🟡 verify: Throughput is documented to require
		// 7.53; need a primary-source citation.
		Throughput: v.AtLeast(7, 53, 0),
		// 🟡 verify: bgRFC C bindings appear from 7.50 PL5 in
		// SAP's release notes (pyrfc historical reference).
		BgRFC: v.AtLeast(7, 50, 5),
		// UTCLong is in 7.50 from the start.
		UTCLong: v.AtLeast(7, 50, 0),
		// 🟡 verify: Fast Serialization (cbRfc) PL11+.
		FastSerialization: v.AtLeast(7, 50, 11),
	}
}
