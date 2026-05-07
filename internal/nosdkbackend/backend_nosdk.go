// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo || nwrfc_nosdk

// Package nosdkbackend is the SDK-free fallback backend for gorfc.
//
// It implements [backend.Backend] with every method returning
// [backend.ErrUnavailable]. It compiles in two modes:
//
//   - When `CGO_ENABLED=0`, the build constraint matches `!cgo`
//     and this is the only backend linked. The library is
//     importable in pure-Go CI environments without the SAP
//     NetWeaver RFC SDK.
//   - When the user opts in with `-tags nwrfc_nosdk`, this
//     backend replaces the cgo backend even with cgo enabled.
//     Useful for downstream packages that re-export gorfc types
//     without wanting the SDK link.
//
// The cgo backend (`internal/sdkbackend`) carries the inverse
// constraint `cgo && !nwrfc_nosdk`, so exactly one backend
// registers in any build configuration.
//
// Calling any method returns an error that wraps
// [backend.ErrUnavailable]; the public `nwrfc/errors.go` maps
// it to `*nwrfc.SDKUnavailableError`.
package nosdkbackend

import (
	"context"
	"fmt"

	"github.com/cjordaoc/gorfc/internal/backend"
)

func init() {
	backend.Register(&noSDK{})
}

// noSDK is the stub backend.
type noSDK struct{}

func (*noSDK) Name() string                       { return "nosdk" }
func (*noSDK) Version() backend.Version           { return backend.Version{} }
func (*noSDK) Capabilities() backend.Capabilities { return backend.Capabilities{} }

func (*noSDK) Open(_ context.Context, _ backend.Params) (backend.ConnHandle, error) {
	return 0, fmt.Errorf("nwrfc/nosdkbackend: Open: %w", backend.ErrUnavailable)
}

func (*noSDK) Close(backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/nosdkbackend: Close: %w", backend.ErrUnavailable)
}

func (*noSDK) Ping(_ context.Context, _ backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/nosdkbackend: Ping: %w", backend.ErrUnavailable)
}

func (*noSDK) Attributes(backend.ConnHandle) (backend.Attributes, error) {
	return backend.Attributes{}, fmt.Errorf("nwrfc/nosdkbackend: Attributes: %w", backend.ErrUnavailable)
}

func (*noSDK) Reset(backend.ConnHandle) error {
	return fmt.Errorf("nwrfc/nosdkbackend: Reset: %w", backend.ErrUnavailable)
}

func (*noSDK) Describe(_ context.Context, _ backend.ConnHandle, fn string) (backend.FunctionDescriptor, error) {
	return backend.FunctionDescriptor{}, fmt.Errorf("nwrfc/nosdkbackend: Describe(%q): %w", fn, backend.ErrUnavailable)
}

func (*noSDK) Invoke(_ context.Context, _ backend.ConnHandle, fn string, _ backend.CallParams, _ backend.InvokeOptions) (backend.CallParams, error) {
	return nil, fmt.Errorf("nwrfc/nosdkbackend: Invoke(%q): %w", fn, backend.ErrUnavailable)
}

func (*noSDK) InvalidateMetadata(fn string) error {
	return fmt.Errorf("nwrfc/nosdkbackend: InvalidateMetadata(%q): %w", fn, backend.ErrUnavailable)
}
