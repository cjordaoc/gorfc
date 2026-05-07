// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

// Package sdkbackend implements [backend.Backend] over the SAP
// NetWeaver RFC SDK via cgo.
//
// Build constraints:
//
//   - `cgo && !nwrfc_nosdk` — this file (and the rest of the
//     package) compiles only when cgo is enabled AND the user
//     has not opted into the SDK-free stub. This is the default
//     for `CGO_ENABLED=1` builds without the `nwrfc_nosdk` tag.
//   - The mirror package `internal/nosdkbackend` carries the
//     inverse constraint, so exactly one of them registers in
//     any given build.
//
// Tier 1 staging:
//
//   - T1.1 (this file): registers a placeholder backend named
//     "sdk-pending" that fails every operation with
//     [backend.ErrUnavailable] wrapped in a "binding not
//     implemented" message. This unblocks the public `nwrfc/`
//     package and the registry plumbing without forcing
//     contributors to have the SAP NWRFC SDK installed in CI.
//   - T1.5 + T1.6 + T1.7: real cgo bindings replace the
//     placeholder methods; the package starts including
//     `<sapnwrfc.h>` and requires SAPNWRFC_HOME to be set at
//     build time.
//
// AGENTS.md compliance: this is NOT a silent fallback — the
// placeholder backend reports its name as "sdk-pending" and
// every method returns an explicit error containing
// [backend.ErrUnavailable]. Operators using a build with this
// stub will see a clear message and can either upgrade the
// library version or build with `-tags nwrfc_nosdk` to make the
// behavior explicit at the build layer.
package sdkbackend

import (
	"context"
	"fmt"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func init() {
	backend.Register(&sdkPending{})
}

// sdkPending is the placeholder until T1.5+ lands the real cgo
// bindings. It implements every Backend method with an explicit
// error wrapping [backend.ErrUnavailable].
type sdkPending struct{}

func (*sdkPending) Name() string                       { return "sdk-pending" }
func (*sdkPending) Version() backend.Version           { return backend.Version{} }
func (*sdkPending) Capabilities() backend.Capabilities { return backend.Capabilities{} }

func (*sdkPending) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 0, fmt.Errorf("nwrfc/sdkbackend: bindings not yet implemented (Tier 1.5): %w", backend.ErrUnavailable)
}

func (*sdkPending) Close(backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/sdkbackend: bindings not yet implemented: %w", backend.ErrUnavailable)
}

func (*sdkPending) Ping(_ context.Context, _ backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/sdkbackend: bindings not yet implemented: %w", backend.ErrUnavailable)
}

func (*sdkPending) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, fmt.Errorf("nwrfc/sdkbackend: bindings not yet implemented: %w", backend.ErrUnavailable)
}

func (*sdkPending) Reset(backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/sdkbackend: bindings not yet implemented: %w", backend.ErrUnavailable)
}

func (*sdkPending) Describe(_ context.Context, _ backend.ConnHandle, fn string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, fmt.Errorf("nwrfc/sdkbackend: Describe(%q) bindings not yet implemented: %w", fn, backend.ErrUnavailable)
}

func (*sdkPending) Invoke(_ context.Context, _ backend.ConnHandle, fn string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return nil, fmt.Errorf("nwrfc/sdkbackend: Invoke(%q) bindings not yet implemented: %w", fn, backend.ErrUnavailable)
}

func (*sdkPending) InvalidateMetadata(fn string) error {
	return fmt.Errorf("nwrfc/sdkbackend: InvalidateMetadata(%q) bindings not yet implemented: %w", fn, backend.ErrUnavailable)
}
