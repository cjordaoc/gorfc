// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo || nwrfc_nosdk

package nosdkbackend

import (
	"context"
	"errors"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// TestNoSDK_AllOpsFailExplicitly asserts every Backend method on
// the stub returns an error wrapping backend.ErrUnavailable, with
// no nil-error path. This is the contract: AGENTS.md forbids
// silent fallback.
func TestNoSDK_AllOpsFailExplicitly(t *testing.T) {
	b := &noSDK{}
	ctx := context.Background()

	if got := b.Name(); got != "nosdk" {
		t.Errorf("Name=%q want %q", got, "nosdk")
	}
	if v := b.Version(); !v.IsZero() {
		t.Errorf("Version=%v want zero", v)
	}
	if c := b.Capabilities(); c != (backend.Capabilities{}) {
		t.Errorf("Capabilities=%v want zero", c)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"Open", func() error {
			_, err := b.Open(ctx, backend.Params{"user": "x"})
			return err
		}},
		{"Close", func() error { return b.Close(1) }},
		{"Ping", func() error { return b.Ping(ctx, 1) }},
		{"Attributes", func() error {
			_, err := b.Attributes(1)
			return err
		}},
		{"Reset", func() error { return b.Reset(ctx, 1) }},
		{"Cancel", func() error { return b.Cancel(1) }},
		{"Describe", func() error {
			_, err := b.Describe(ctx, 1, "RFC_PING")
			return err
		}},
		{"Invoke", func() error {
			_, err := b.Invoke(ctx, 1, "RFC_PING", backend.CallParams{}, backend.InvokeOptions{})
			return err
		}},
		{"InvalidateMetadata", func() error { return b.InvalidateMetadata("X") }},
	}

	for _, tc := range cases {
		err := tc.run()
		if err == nil {
			t.Errorf("%s: nil error", tc.name)
			continue
		}
		if !errors.Is(err, backend.ErrUnavailable) {
			t.Errorf("%s: err=%v, want errors.Is ErrUnavailable", tc.name, err)
		}
	}
}

// TestNoSDK_Registered confirms the package init() registered
// the stub as the active backend in this build.
func TestNoSDK_Registered(t *testing.T) {
	got := backend.Default().Name()
	if got != "nosdk" {
		t.Fatalf("backend.Default().Name()=%q want %q (init() did not register)", got, "nosdk")
	}
}
