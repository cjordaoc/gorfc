// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo || nwrfc_nosdk

// Package nosdkbackend is the SDK-free fallback backend for gorfc.
//
// Compiles in two modes:
//
//   - When `CGO_ENABLED=0`, the build constraint matches `!cgo`
//     and this is the only backend linked.
//   - When the user opts in with `-tags nwrfc_nosdk`, this
//     backend replaces the cgo backend.
//
// Every method returns an error wrapping
// [backend.ErrUnavailable]. The public `nwrfc` package's
// mapBackendError translates that into a typed
// *nwrfc.SDKUnavailableError so callers can branch via
// errors.Is(err, nwrfc.ErrSDKUnavailable).
//
// AGENTS.md compliance: this is NOT silent fallback. The
// caller sees an explicit, typed error on every operation; no
// synthetic success path.
package nosdkbackend

import (
	"context"
	"fmt"
	"os"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func init() {
	backend.Register(theBackend)
}

var theBackend = &noSDK{lookupPath: lookupSAPNWRFCHome()}

// noSDK is the stub backend.
type noSDK struct {
	lookupPath string
}

func lookupSAPNWRFCHome() string {
	if v := os.Getenv("SAPNWRFC_HOME"); v != "" {
		return v
	}
	return "(SAPNWRFC_HOME unset)"
}

func (b *noSDK) Name() string                     { return "nosdk" }
func (*noSDK) Version() backend.Version           { return backend.Version{} }
func (*noSDK) Capabilities() backend.Capabilities { return backend.Capabilities{} }

func (b *noSDK) errSDKUnavailable(op string) error {
	return fmt.Errorf("nwrfc/nosdkbackend.%s: built without SAP NetWeaver RFC SDK (lookup_path=%s): %w",
		op, b.lookupPath, backend.ErrUnavailable)
}

func (b *noSDK) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 0, b.errSDKUnavailable("Open")
}
func (b *noSDK) Close(backend.ConnHandle) error { return b.errSDKUnavailable("Close") }
func (b *noSDK) Ping(_ context.Context, _ backend.ConnHandle) error {
	return b.errSDKUnavailable("Ping")
}
func (b *noSDK) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, b.errSDKUnavailable("Attributes")
}
func (b *noSDK) Reset(_ context.Context, _ backend.ConnHandle) error {
	return b.errSDKUnavailable("Reset")
}

// Cancel implements [backend.Cancellable] for the SDK-free
// stub. There is no real connection to cancel; we surface the
// SDK-unavailable error so callers cannot silently rely on a
// no-op cancel that does nothing useful.
func (b *noSDK) Cancel(_ backend.ConnHandle) error {
	return b.errSDKUnavailable("Cancel")
}
func (b *noSDK) Describe(_ context.Context, _ backend.ConnHandle, fn string) (backend.FunctionDescriptor, error) {
	_ = fn
	return backend.FunctionDescriptor{}, b.errSDKUnavailable("Describe")
}
func (b *noSDK) Invoke(_ context.Context, _ backend.ConnHandle, fn string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	_ = fn
	return nil, b.errSDKUnavailable("Invoke")
}
func (b *noSDK) InvalidateMetadata(fn string) error {
	_ = fn
	return b.errSDKUnavailable("InvalidateMetadata")
}
