// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestPreflightReportIncludesSDKPackagingChecks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SAPNWRFC_HOME", dir)

	report := preflightReport(assertErr("sdk missing"))

	home, ok := report["sapnwrfc_home"].(map[string]any)
	if !ok || home["set"] != true || home["exists"] != true {
		t.Fatalf("sapnwrfc_home = %#v", report["sapnwrfc_home"])
	}
	files, ok := report["required_files"].([]map[string]any)
	if !ok || len(files) < 2 {
		t.Fatalf("required_files = %#v", report["required_files"])
	}
	if files[0]["name"] != "sapnwrfc.h" {
		t.Fatalf("first required file = %#v", files[0])
	}
	dyn, ok := report["dynamic_loading"].(map[string]any)
	if !ok || dyn["ok"] != false || dyn["error"] != "sdk missing" {
		t.Fatalf("dynamic_loading = %#v", report["dynamic_loading"])
	}
}

func TestRedactRuntimeSecrets(t *testing.T) {
	t.Setenv("GORFC_TEST_PASSWD", "super-sensitive-rfc-password")
	got := redactRuntimeSecrets("login failed for super-sensitive-rfc-password")
	if strings.Contains(got, "super-sensitive-rfc-password") {
		t.Fatalf("secret leaked: %q", got)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
