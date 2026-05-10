// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Command nwrfc is a developer-friendly CLI over the gorfc
// library. Useful for spot-checking SAP RFC connectivity and
// inspecting function metadata without writing Go code.
//
// Subcommands:
//
//	nwrfc ping            - RfcPing the connection
//	nwrfc describe FN     - print the function descriptor
//	nwrfc call FN k=v ... - invoke and dump the response
//	nwrfc health          - local SDK/loadability health
//	nwrfc preflight       - SDK and optional SAP connection preflight
//	nwrfc test-connection - require SDK + SAP connection ping
//	nwrfc version         - print SDK and library versions
//
// Connection parameters come from GORFC_TEST_* env vars (the
// same set the integration tests use). See docs/INSTALL.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "--version", "version":
		version(os.Args[2:])
	case "health":
		health(os.Args[2:])
	case "preflight":
		preflight(os.Args[2:])
	case "test-connection":
		testConnection(os.Args[2:])
	case "ping":
		ensureSDK()
		ping()
	case "describe":
		ensureSDK()
		if len(os.Args) < 3 {
			fail("usage: nwrfc describe FUNCTION_NAME")
		}
		describe(os.Args[2])
	case "call":
		ensureSDK()
		if len(os.Args) < 3 {
			fail("usage: nwrfc call FUNCTION_NAME [PARAM=VALUE ...]")
		}
		call(os.Args[2], os.Args[3:])
	case "-h", "--help", "help":
		usage()
	default:
		fail("unknown subcommand %q", os.Args[1])
	}
}

func usage() {
	fmt.Println(`Usage: nwrfc <command> [args]

Commands:
  ping                  Ping the SAP system.
  health                Report SDK loadability and packaging status.
  preflight             Report SDK status and ping only when env config exists.
  test-connection       Require SDK + env config and ping the SAP system.
  describe FUNCTION     Print the function descriptor as JSON.
  call FUNCTION KV...   Invoke a function with KEY=VALUE params.
  version               Print SDK + library versions.

Connection parameters come from GORFC_TEST_USER, GORFC_TEST_PASSWD,
GORFC_TEST_ASHOST, GORFC_TEST_SYSNR, GORFC_TEST_CLIENT, GORFC_TEST_LANG.`)
}

func ensureSDK() {
	if err := nwrfc.EnsureSDK(); err != nil {
		fail("nwrfc CLI requires SDK: %v", err)
	}
}

func version(args []string) {
	out := map[string]any{
		"cli":          "nwrfc",
		"sdk_version":  nwrfc.SDKVersion().String(),
		"capabilities": nwrfc.Capabilities(),
	}
	if hasJSON(args) {
		printJSON(out)
		return
	}
	fmt.Printf("nwrfc CLI; SDK %s; capabilities=%+v\n", nwrfc.SDKVersion(), nwrfc.Capabilities())
}

func health(args []string) {
	err := nwrfc.EnsureSDK()
	out := map[string]any{
		"ok":           err == nil,
		"sdk_version":  nwrfc.SDKVersion().String(),
		"capabilities": nwrfc.Capabilities(),
	}
	if err != nil {
		out["error"] = redactRuntimeSecrets(err.Error())
	}
	if hasJSON(args) {
		printJSON(out)
	} else if err != nil {
		fmt.Printf("SDK unavailable: %v\n", err)
	} else {
		fmt.Printf("OK: SDK %s capabilities=%+v\n", nwrfc.SDKVersion(), nwrfc.Capabilities())
	}
	if err != nil {
		os.Exit(1)
	}
}

func preflight(args []string) {
	err := nwrfc.EnsureSDK()
	out := preflightReport(err)
	if err != nil {
		out["error"] = redactRuntimeSecrets(err.Error())
		printPreflight(out, args, 1)
		return
	}
	if paramsComplete() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, openErr := nwrfc.Open(ctx, paramsFromEnv())
		if openErr != nil {
			out["ok"] = false
			out["connection"] = "failed"
			out["error"] = redactRuntimeSecrets(openErr.Error())
			printPreflight(out, args, 1)
			return
		}
		defer c.Close()
		if pingErr := c.Ping(ctx); pingErr != nil {
			out["ok"] = false
			out["connection"] = "failed"
			out["error"] = redactRuntimeSecrets(pingErr.Error())
			printPreflight(out, args, 1)
			return
		}
		out["connection"] = "ok"
	}
	printPreflight(out, args, 0)
}

func preflightReport(err error) map[string]any {
	return map[string]any{
		"ok":              err == nil,
		"sdk_version":     nwrfc.SDKVersion().String(),
		"capabilities":    nwrfc.Capabilities(),
		"connection":      "not_configured",
		"sapnwrfc_home":   sapNWRFCHomeStatus(),
		"required_files":  requiredRuntimeFiles(),
		"runtime":         runtimeStatus(),
		"dynamic_loading": map[string]any{"ok": err == nil, "error": errorString(err)},
	}
}

func sapNWRFCHomeStatus() map[string]any {
	home := strings.TrimSpace(os.Getenv("SAPNWRFC_HOME"))
	return map[string]any{
		"set":    home != "",
		"path":   safePath(home),
		"exists": home != "" && pathExists(home),
	}
}

func requiredRuntimeFiles() []map[string]any {
	home := strings.TrimSpace(os.Getenv("SAPNWRFC_HOME"))
	out := []map[string]any{}
	if home != "" {
		out = append(out, fileStatus("sapnwrfc.h", filepath.Join(home, "include", "sapnwrfc.h")))
	}
	libDir := ""
	if home != "" {
		libDir = filepath.Join(home, "lib")
	}
	for _, name := range requiredLibraryNames() {
		path := ""
		if libDir != "" {
			path = filepath.Join(libDir, name)
		}
		out = append(out, fileStatus(name, path))
	}
	return out
}

func requiredLibraryNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"sapnwrfc.dll", "libsapucum.dll"}
	case "darwin":
		return []string{"libsapnwrfc.dylib", "libsapucum.dylib"}
	default:
		return []string{"libsapnwrfc.so", "libsapucum.so"}
	}
}

func fileStatus(name, path string) map[string]any {
	return map[string]any{
		"name":   name,
		"path":   safePath(path),
		"exists": strings.TrimSpace(path) != "" && pathExists(path),
	}
}

func runtimeStatus() map[string]any {
	return map[string]any{
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"cgo_linked": nwrfc.SDKVersion().String() != "no-sdk",
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func safePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return redactRuntimeSecrets(err.Error())
}

func testConnection(args []string) {
	out, code := testConnectionReport(context.Background())
	printPreflight(out, args, code)
}

// testConnectionReport runs the full test-connection diagnostic
// pipeline and returns the JSON-shaped report plus a process
// exit code (0 success, 1 failure). It is split out from the
// CLI entry point so tests can drive it deterministically.
//
// The report shape extends preflightReport with structured
// diagnostic fields used by SAP Basis / network operators when
// triaging RC 750 / RFC_COMMUNICATION_FAILURE patterns. None of
// the fields ever carry a credential — Params is redacted by
// the typed Params.LogValue path, and any SDK message that
// contains a configured GORFC_TEST_* secret is run through
// redactRuntimeSecrets before being attached to the report.
func testConnectionReport(parent context.Context) (map[string]any, int) {
	if parent == nil {
		parent = context.Background()
	}
	sdkErr := nwrfc.EnsureSDK()
	out := preflightReport(sdkErr)
	annotateGatewayCoordinates(out, paramsFromEnv())
	out["stage"] = stageInit
	out["connection_opened"] = false
	out["auth_reached"] = false
	out["network_reachable"] = nil

	if sdkErr != nil {
		out["ok"] = false
		out["connection"] = "failed"
		out["stage"] = stageSDKUnavailable
		out["error"] = redactRuntimeSecrets(sdkErr.Error())
		out["recommendation"] = recommendationFor(stageSDKUnavailable)
		return out, 1
	}
	if !paramsComplete() {
		out["ok"] = false
		out["connection"] = "not_configured"
		out["stage"] = stageParamsMissing
		out["error"] = "GORFC_TEST_ASHOST, GORFC_TEST_SYSNR, GORFC_TEST_CLIENT, GORFC_TEST_USER, and GORFC_TEST_PASSWD are required"
		out["recommendation"] = recommendationFor(stageParamsMissing)
		return out, 1
	}

	host, _, port := gatewayCoordinates(paramsFromEnv())
	out["network_reachable"] = probeGateway(host, port, 4*time.Second)
	if reach, ok := out["network_reachable"].(bool); ok && !reach {
		out["ok"] = false
		out["connection"] = "failed"
		out["stage"] = stageNetworkUnreachable
		out["error"] = fmt.Sprintf("tcp probe to %s:%d failed before sdk Open", host, port)
		out["recommendation"] = recommendationFor(stageNetworkUnreachable)
		return out, 1
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	c, openErr := nwrfc.Open(ctx, paramsFromEnv())
	if openErr != nil {
		annotateConnectionFailure(out, openErr)
		return out, 1
	}
	defer c.Close()
	out["connection_opened"] = true
	out["auth_reached"] = true

	if pingErr := c.Ping(ctx); pingErr != nil {
		annotateConnectionFailure(out, pingErr)
		// Open succeeded — the SAP system is reachable and
		// authenticated. A Ping failure is a runtime / system
		// issue, not a gateway / auth blocker.
		out["stage"] = stagePingFailed
		out["recommendation"] = recommendationFor(stagePingFailed)
		return out, 1
	}
	attrs, _ := c.Attributes()
	out["connection"] = "ok"
	out["stage"] = stageOK
	out["attributes"] = map[string]any{"sys_id": attrs.SysID, "client": attrs.Client, "user": attrs.User}
	out["recommendation"] = recommendationFor(stageOK)
	return out, 0
}

// Diagnostic stage labels emitted in the test-connection JSON
// report. Stable identifiers; consumers (SAP Basis runbooks,
// VDI execution-node logs, ledger entries) branch on these
// values.
const (
	stageInit               = "init"
	stageSDKUnavailable     = "sdk_unavailable"
	stageParamsMissing      = "params_missing"
	stageNetworkUnreachable = "network_unreachable"
	stageGatewayReset       = "gateway_communication_failure"
	stageAuthFailed         = "auth_failed"
	stageBrokenConnection   = "broken_connection"
	stageTimeout            = "timeout"
	stagePingFailed         = "ping_failed"
	stageOK                 = "ok"
)

// gatewayCoordinates derives the SAP gateway host and service
// the SDK will dial for the given Params. The returned `port`
// is the integer form of `service` for direct probes; it is
// zero when the SDK would resolve the service from /etc/services
// (e.g. WebSocket RFC with a non-numeric WSPort). Returned host
// is empty when no transport shape is configured.
func gatewayCoordinates(p nwrfc.Params) (host, service string, port int) {
	switch {
	case strings.TrimSpace(p.WSHost) != "":
		host = strings.TrimSpace(p.WSHost)
		service = strings.TrimSpace(p.WSPort)
		port, _ = strconv.Atoi(service)
		return host, service, port
	case strings.TrimSpace(p.AsHost) != "":
		host = strings.TrimSpace(p.AsHost)
		sysnr := strings.TrimSpace(p.SysNr)
		service = "sapgw" + sysnr
		// SAP convention: sapgwNN listens on TCP 3300 + NN.
		if n, err := strconv.Atoi(sysnr); err == nil && n >= 0 && n <= 99 {
			port = 3300 + n
		}
		return host, service, port
	case strings.TrimSpace(p.MsHost) != "":
		host = strings.TrimSpace(p.MsHost)
		// Message-server lookup; service name carries the SID.
		service = "sapms" + strings.TrimSpace(p.R3Name)
		return host, service, 0
	}
	return "", "", 0
}

// annotateGatewayCoordinates attaches the derived gateway host
// and service to the report. Used by both the success and
// failure branches so SAP Basis sees the dial target even when
// nothing reaches the SDK.
func annotateGatewayCoordinates(out map[string]any, p nwrfc.Params) {
	host, service, port := gatewayCoordinates(p)
	out["gateway_host"] = host
	out["gateway_service"] = service
	if port != 0 {
		out["gateway_port"] = port
	}
}

// probeGateway runs a fast TCP connect probe to host:port. It
// is a one-shot existence check, not a full handshake — used
// only to differentiate "iconic edge / SAProuter rejecting at
// L4" from "SDK Open failed somewhere later". A `nil` return
// is reported (instead of true / false) when no port is known.
func probeGateway(host string, port int, timeout time.Duration) any {
	if strings.TrimSpace(host) == "" || port == 0 {
		return nil
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// annotateConnectionFailure decodes the typed nwrfc error and
// attaches diagnostic fields. The caller has already set
// `connection_opened` / `auth_reached`; this function may
// downgrade `auth_reached` when the error is a logon failure.
func annotateConnectionFailure(out map[string]any, err error) {
	out["ok"] = false
	out["connection"] = "failed"
	out["error"] = redactRuntimeSecrets(err.Error())

	stage, recommendation := classifyConnectionFailure(err)
	out["stage"] = stage
	out["recommendation"] = recommendation

	var commErr *nwrfc.CommunicationError
	if errors.As(err, &commErr) {
		out["sdk_error_key"] = commErr.SDKErrorInfo.Key
		out["sdk_error_group"] = sdkGroupName(commErr.SDKErrorInfo.Group)
		out["rc"] = commErr.SDKErrorInfo.Code
		if commErr.Host != "" {
			out["gateway_host"] = commErr.Host
		}
		if commErr.Service != "" {
			out["gateway_service"] = commErr.Service
		}
		// Communication failures land before authentication.
		out["auth_reached"] = false
		out["connection_opened"] = false
	}

	var logonErr *nwrfc.LogonError
	if errors.As(err, &logonErr) {
		out["sdk_error_key"] = logonErr.SDKErrorInfo.Key
		out["sdk_error_group"] = sdkGroupName(logonErr.SDKErrorInfo.Group)
		out["rc"] = logonErr.SDKErrorInfo.Code
		// Reaching logon means the gateway accepted the TCP
		// stream and routed to the dispatcher. Authentication
		// itself failed, which is a different problem class.
		out["auth_reached"] = true
		out["connection_opened"] = false
	}

	var brokenErr *nwrfc.BrokenConnectionError
	if errors.As(err, &brokenErr) {
		out["connection_opened"] = false
	}
}

// classifyConnectionFailure maps a typed nwrfc error to the
// stable stage label plus a concrete next-step recommendation
// for the operator.
func classifyConnectionFailure(err error) (stage, recommendation string) {
	switch {
	case err == nil:
		return stageOK, recommendationFor(stageOK)
	case errors.Is(err, nwrfc.ErrCommunication):
		return stageGatewayReset, recommendationFor(stageGatewayReset)
	case errors.Is(err, nwrfc.ErrLogon):
		return stageAuthFailed, recommendationFor(stageAuthFailed)
	case errors.Is(err, nwrfc.ErrBrokenConn):
		return stageBrokenConnection, recommendationFor(stageBrokenConnection)
	case errors.Is(err, nwrfc.ErrTimeout):
		return stageTimeout, recommendationFor(stageTimeout)
	default:
		return stagePingFailed, recommendationFor(stagePingFailed)
	}
}

// recommendationFor returns the operator-facing follow-up text
// for each stage. Strings are deliberately concrete: every
// recommendation names the role responsible for the next step
// (network ops, SAP Basis, VDI operator) so the report can be
// pasted into a ticket without rewriting.
func recommendationFor(stage string) string {
	switch stage {
	case stageOK:
		return "connection healthy; no operator action required"
	case stageSDKUnavailable:
		return "VDI operator: install SAP NW RFC SDK and set SAPNWRFC_HOME; rerun preflight"
	case stageParamsMissing:
		return "VDI operator: provision GORFC_TEST_ASHOST/SYSNR/CLIENT/USER/PASSWD via the secret store"
	case stageNetworkUnreachable:
		return "Network ops: SAP gateway port not reachable from VDI; verify SAProuter / firewall / iconic edge allowlist for VDI source IP"
	case stageGatewayReset:
		return "SAP Basis + Network ops: TCP reached the SAP gateway but it rejected/reset before authentication; verify gw/acl_mode + gw/acl_info, SAProuter route, and source-IP allowlist on the iconic SAP edge"
	case stageAuthFailed:
		return "SAP Basis + VDI operator: gateway accepted the connection; SAP rejected the credentials. Verify user/client, password rotation, and SU01 lock state"
	case stageBrokenConnection:
		return "Retry once; if the failure persists, capture VDI dev_rfc trace and escalate to SAP Basis"
	case stageTimeout:
		return "Increase client timeout if the SAP system is under load; otherwise escalate to SAP Basis to inspect dispatcher work-process saturation"
	case stagePingFailed:
		return "SAP Basis: dispatcher reachable and authenticated, but RFC_PING failed; inspect SM21 / dev_disp for ICM/dispatcher state"
	default:
		return "no recommendation derived for stage"
	}
}

// sdkGroupName returns a stable string identifier for an SDK
// error group (RFC_ERROR_GROUP). Stable across SDK PLs.
func sdkGroupName(group uint32) string {
	switch group {
	case backend.GroupOK:
		return "OK"
	case backend.GroupAbapApplicationFailure:
		return "ABAP_APPLICATION_FAILURE"
	case backend.GroupAbapRuntimeFailure:
		return "ABAP_RUNTIME_FAILURE"
	case backend.GroupLogonFailure:
		return "LOGON_FAILURE"
	case backend.GroupCommunicationFailure:
		return "COMMUNICATION_FAILURE"
	case backend.GroupExternalRuntimeFailure:
		return "EXTERNAL_RUNTIME_FAILURE"
	case backend.GroupExternalApplicationFailure:
		return "EXTERNAL_APPLICATION_FAILURE"
	case backend.GroupExternalAuthorizationFailure:
		return "EXTERNAL_AUTHORIZATION_FAILURE"
	default:
		return "UNKNOWN"
	}
}

func printPreflight(out map[string]any, args []string, code int) {
	if hasJSON(args) {
		printJSON(out)
	} else {
		bytes, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(bytes))
	}
	if code != 0 {
		os.Exit(code)
	}
}

func paramsComplete() bool {
	p := paramsFromEnv()
	return p.AsHost != "" && p.SysNr != "" && p.Client != "" && p.User != "" && p.Passwd != ""
}

func hasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "-json" {
			return true
		}
	}
	return false
}

func printJSON(v any) {
	bytes, _ := json.Marshal(v)
	fmt.Println(string(bytes))
}

func redactRuntimeSecrets(s string) string {
	out := s
	for _, key := range []string{
		"GORFC_TEST_PASSWD",
		"GORFC_TEST_PASSWORD",
		"GORFC_TEST_MYSAPSSO2",
		"GORFC_TEST_BEARER",
		"GORFC_TEST_SAML2",
	} {
		if value := os.Getenv(key); len(value) >= 4 {
			out = strings.ReplaceAll(out, value, "«redacted»")
		}
	}
	return out
}

func paramsFromEnv() nwrfc.Params {
	return nwrfc.Params{
		AsHost: os.Getenv("GORFC_TEST_ASHOST"),
		SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
		Client: os.Getenv("GORFC_TEST_CLIENT"),
		User:   os.Getenv("GORFC_TEST_USER"),
		Passwd: os.Getenv("GORFC_TEST_PASSWD"),
		Lang:   os.Getenv("GORFC_TEST_LANG"),
	}
}

func ping() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := nwrfc.Open(ctx, paramsFromEnv())
	if err != nil {
		fail("Open: %v", redactRuntimeSecrets(err.Error()))
	}
	defer c.Close()
	if err := c.Ping(ctx); err != nil {
		fail("Ping: %v", redactRuntimeSecrets(err.Error()))
	}
	attrs, _ := c.Attributes()
	fmt.Printf("OK: %s/%s as %s\n", attrs.SysID, attrs.Client, attrs.User)
}

func describe(fn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := nwrfc.Open(ctx, paramsFromEnv())
	if err != nil {
		fail("Open: %v", redactRuntimeSecrets(err.Error()))
	}
	defer c.Close()
	d, err := c.Describe(ctx, fn)
	if err != nil {
		fail("Describe: %v", redactRuntimeSecrets(err.Error()))
	}
	bytes, _ := json.MarshalIndent(d, "", "  ")
	fmt.Println(string(bytes))
}

func call(fn string, kv []string) {
	in := map[string]any{}
	for _, pair := range kv {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			fail("expected KEY=VALUE, got %q", pair)
		}
		in[pair[:eq]] = pair[eq+1:]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := nwrfc.Open(ctx, paramsFromEnv())
	if err != nil {
		fail("Open: %v", redactRuntimeSecrets(err.Error()))
	}
	defer c.Close()
	out, err := nwrfc.CallMap(ctx, c, fn, in)
	if err != nil {
		fail("Call %s: %v", fn, redactRuntimeSecrets(err.Error()))
	}
	bytes, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(bytes))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nwrfc: "+format+"\n", args...)
	os.Exit(1)
}
