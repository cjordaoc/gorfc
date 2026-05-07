// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo || nwrfc_nosdk

// Tests that only make sense against the no-SDK stub backend. The
// build tag matches the no-SDK side of internal/sdkbackend's
// constraint cheat-sheet (see docs/BUILD.md): "(!cgo) ||
// nwrfc_nosdk" picks the nosdkbackend, where every connection
// attempt is supposed to surface as *SDKUnavailableError.

package nwrfc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cjordaoc/gorfc/nwrfc"
)

// TestOpen_NoSDK_SurfacesAsSDKUnavailable: against the no-SDK
// stub backend, Open returns *SDKUnavailableError rather than a
// generic backend error. With the cgo+SDK backend wired in, Open
// would actually try the network and surface a comms failure
// instead — that is correct behavior, just not what this test
// asserts. Hence the build tag.
func TestOpen_NoSDK_SurfacesAsSDKUnavailable(t *testing.T) {
	_, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err == nil {
		t.Fatal("Open returned nil error")
	}
	if !errors.Is(err, nwrfc.ErrSDKUnavailable) {
		t.Errorf("err=%v want errors.Is ErrSDKUnavailable", err)
	}
	var su *nwrfc.SDKUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("errors.As did not extract *SDKUnavailableError")
	}
	if su.Reason == "" {
		t.Error("SDKUnavailableError.Reason is empty")
	}
}
