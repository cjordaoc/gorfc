// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
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

func TestGatewayCoordinatesDirectConnection(t *testing.T) {
	host, service, port := gatewayCoordinates(nwrfc.Params{AsHost: "vhilfws1wd01.sap.iconic.com.br", SysNr: "00"})
	if host != "vhilfws1wd01.sap.iconic.com.br" {
		t.Fatalf("host = %q", host)
	}
	if service != "sapgw00" {
		t.Fatalf("service = %q", service)
	}
	if port != 3300 {
		t.Fatalf("port = %d", port)
	}
}

func TestGatewayCoordinatesWebSocket(t *testing.T) {
	host, service, port := gatewayCoordinates(nwrfc.Params{WSHost: "ws.example.local", WSPort: "443"})
	if host != "ws.example.local" || service != "443" || port != 443 {
		t.Fatalf("ws coords = %q/%q/%d", host, service, port)
	}
}

func TestGatewayCoordinatesEmpty(t *testing.T) {
	host, service, port := gatewayCoordinates(nwrfc.Params{})
	if host != "" || service != "" || port != 0 {
		t.Fatalf("empty coords = %q/%q/%d", host, service, port)
	}
}

func TestClassifyCommunicationFailureIsGatewayStage(t *testing.T) {
	commErr := &nwrfc.CommunicationError{
		SDKErrorInfo: nwrfc.SDKErrorInfo{
			Code:    750,
			Group:   backend.GroupCommunicationFailure,
			Key:     "RFC_COMMUNICATION_FAILURE",
			Message: "partner ... not reached",
		},
		Host:    "vhilfws1wd01",
		Service: "sapdp00",
	}
	stage, rec := classifyConnectionFailure(commErr)
	if stage != stageGatewayReset {
		t.Fatalf("stage = %q want %q", stage, stageGatewayReset)
	}
	if !strings.Contains(strings.ToLower(rec), "gateway") {
		t.Fatalf("recommendation missing gateway hint: %q", rec)
	}
}

func TestClassifyLogonFailureIsAuthStage(t *testing.T) {
	logonErr := &nwrfc.LogonError{
		SDKErrorInfo: nwrfc.SDKErrorInfo{Group: backend.GroupLogonFailure, Key: "RFC_LOGON_FAILURE"},
		User:         "TESTUSER",
		Client:       "100",
		SysID:        "DS4",
	}
	stage, rec := classifyConnectionFailure(logonErr)
	if stage != stageAuthFailed {
		t.Fatalf("stage = %q want %q", stage, stageAuthFailed)
	}
	if !strings.Contains(strings.ToLower(rec), "credential") {
		t.Fatalf("recommendation missing credential hint: %q", rec)
	}
}

func TestClassifyTimeoutAndCancelled(t *testing.T) {
	type stagedError struct {
		err   error
		want  string
		check string
	}
	cases := []stagedError{
		{err: &nwrfc.TimeoutError{}, want: stageTimeout, check: "timeout"},
		{err: &nwrfc.BrokenConnectionError{}, want: stageBrokenConnection, check: "trace"},
	}
	for _, tc := range cases {
		stage, rec := classifyConnectionFailure(tc.err)
		if stage != tc.want {
			t.Fatalf("stage(%T) = %q want %q", tc.err, stage, tc.want)
		}
		if tc.check != "" && !strings.Contains(strings.ToLower(rec), tc.check) {
			t.Fatalf("recommendation(%T) missing %q: %q", tc.err, tc.check, rec)
		}
	}
}

func TestAnnotateConnectionFailurePopulatesFields(t *testing.T) {
	out := map[string]any{}
	commErr := &nwrfc.CommunicationError{
		SDKErrorInfo: nwrfc.SDKErrorInfo{
			Code:    750,
			Group:   backend.GroupCommunicationFailure,
			Key:     "RFC_COMMUNICATION_FAILURE",
			Message: "connection reset",
		},
		Host:    "vhilfws1wd01.sap.iconic.com.br",
		Service: "sapdp00",
	}
	annotateConnectionFailure(out, commErr)

	if out["stage"] != stageGatewayReset {
		t.Fatalf("stage = %v", out["stage"])
	}
	if out["sdk_error_key"] != "RFC_COMMUNICATION_FAILURE" {
		t.Fatalf("sdk_error_key = %v", out["sdk_error_key"])
	}
	if out["sdk_error_group"] != "COMMUNICATION_FAILURE" {
		t.Fatalf("sdk_error_group = %v", out["sdk_error_group"])
	}
	if out["rc"] != 750 {
		t.Fatalf("rc = %v", out["rc"])
	}
	if out["gateway_host"] != "vhilfws1wd01.sap.iconic.com.br" {
		t.Fatalf("gateway_host = %v", out["gateway_host"])
	}
	if out["gateway_service"] != "sapdp00" {
		t.Fatalf("gateway_service = %v", out["gateway_service"])
	}
	if got := out["auth_reached"]; got != false {
		t.Fatalf("auth_reached = %v want false", got)
	}
}

func TestAnnotateConnectionFailureRedactsSecrets(t *testing.T) {
	t.Setenv("GORFC_TEST_PASSWD", "very-sensitive-rfc-secret")
	out := map[string]any{}
	annotateConnectionFailure(out, errors.New("openErr: invalid credentials for very-sensitive-rfc-secret"))
	got, _ := out["error"].(string)
	if strings.Contains(got, "very-sensitive-rfc-secret") {
		t.Fatalf("secret leaked into error: %q", got)
	}
}

func TestSdkGroupNameStableLabels(t *testing.T) {
	if sdkGroupName(backend.GroupCommunicationFailure) != "COMMUNICATION_FAILURE" {
		t.Fatalf("communication group label drift")
	}
	if sdkGroupName(backend.GroupLogonFailure) != "LOGON_FAILURE" {
		t.Fatalf("logon group label drift")
	}
	if sdkGroupName(backend.GroupAbapApplicationFailure) != "ABAP_APPLICATION_FAILURE" {
		t.Fatalf("abap-app group label drift")
	}
	if sdkGroupName(99) != "UNKNOWN" {
		t.Fatalf("unknown group label drift")
	}
}

func TestRecommendationCoversAllStages(t *testing.T) {
	for _, stage := range []string{
		stageOK, stageSDKUnavailable, stageParamsMissing, stageNetworkUnreachable,
		stageGatewayReset, stageAuthFailed, stageBrokenConnection, stageTimeout, stagePingFailed,
	} {
		rec := recommendationFor(stage)
		if rec == "" || strings.Contains(rec, "no recommendation derived") {
			t.Fatalf("missing recommendation for stage %q", stage)
		}
	}
}

func TestProbeGatewayHandlesEmptyHostAndPort(t *testing.T) {
	if got := probeGateway("", 0, 0); got != nil {
		t.Fatalf("probe with no coords = %v want nil", got)
	}
	if got := probeGateway("host.invalid.local", 0, 0); got != nil {
		t.Fatalf("probe with no port = %v want nil", got)
	}
}
