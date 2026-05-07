// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package sdkbackend

/*
#include <sapnwrfc.h>
*/
import "C"

import (
	"github.com/cjordaoc/gorfc/internal/backend"
)

// Version returns the SAP NWRFC SDK version reported by
// `RfcGetVersion`.
//
// SDK function: RfcGetVersion (✅ confirmed; 7.50 PL3+).
func (b *sdkBackend) Version() backend.Version {
	var major, minor, patchlevel C.uint
	C.RfcGetVersion(&major, &minor, &patchlevel)
	return backend.Version{
		Major:      uint(major),
		Minor:      uint(minor),
		PatchLevel: uint(patchlevel),
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
