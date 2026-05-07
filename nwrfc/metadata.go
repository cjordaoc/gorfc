// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Describe returns the metadata descriptor for the named RFC
// function on the connected SAP system. The first call across
// the process+system pair fetches; subsequent calls hit the
// SDK's internal metadata cache (managed by the SDK itself).
//
// SDK function: RfcGetFunctionDesc + iteration via
// RfcGetParameter*ByIndex (✅ confirmed).
func (c *Conn) Describe(ctx context.Context, fn string) (backend.FunctionDescriptor, error) {
	if err := c.checkOpen(); err != nil {
		return backend.FunctionDescriptor{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d, err := c.backend.Describe(ctx, c.handle, fn)
	if err != nil {
		return backend.FunctionDescriptor{}, mapBackendError(err)
	}
	return d, nil
}

// InvalidateMetadata removes the cached descriptor for fn from
// the SDK's metadata cache so the next Describe / Call refetches
// against the SAP repository. Useful after a transport upgrade
// changed the function signature on the SAP side.
//
// SDK function: RfcRemoveFunctionDesc (🟡 verify; node-rfc
// reports availability in 7.50 PL3+, programming guide
// pending).
func InvalidateMetadata(fn string) error {
	return mapBackendError(backend.Default().InvalidateMetadata(fn))
}
