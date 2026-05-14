// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"github.com/cjordaoc/gorfc/internal/backend"
)

// enforceCapabilities is the hardlock for capability-gated
// transports and features. Called from [Open] after
// [Params.validate] but before any SDK call.
//
// Today it covers WebSocket RFC: if the caller asked for the
// WebSocket transport (WSHost set) and the active SDK does
// not advertise the WebSocketRFC capability, return
// *UnsupportedFeatureError carrying both the required and
// current SDK versions so operators can act on the message.
//
// New rules go here, not in [Params.validate]: validation is
// the static shape check that does not depend on the active
// backend, while capability hardlocking depends on
// [backend.Backend.Capabilities] which is runtime SDK state.
//
// AGENTS.md non-negotiable: no silent fallback. A WSHost
// connection on an SDK that does not support WebSocket RFC
// would otherwise downgrade to whichever transport the SDK
// chose, possibly punching a non-TLS hole through a corporate
// boundary that the operator assumed was TLS.
func enforceCapabilities(p Params, b backend.Backend) error {
	caps := b.Capabilities()
	if p.WSHost != "" && !caps.WebSocketRFC {
		return &UnsupportedFeatureError{
			Feature: "WebSocketRFC",
			// 7.50 PL10 is the documented floor for
			// WebSocket RFC; documented in
			// docs/SDK_FUNCTIONS_MAP.md and confirmed by
			// node-rfc release notes. If a future SDK
			// raises the floor, update both this constant
			// and the table.
			RequiredVersion: backend.Version{Major: 7, Minor: 50, PatchLevel: 10},
			CurrentVersion:  b.Version(),
		}
	}
	return nil
}
