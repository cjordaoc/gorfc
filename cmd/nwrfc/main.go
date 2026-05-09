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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	err := nwrfc.EnsureSDK()
	out := preflightReport(err)
	if err != nil {
		out["ok"] = false
		out["connection"] = "failed"
		out["error"] = redactRuntimeSecrets(err.Error())
		printPreflight(out, args, 1)
		return
	}
	if !paramsComplete() {
		out["ok"] = false
		out["connection"] = "not_configured"
		out["error"] = "GORFC_TEST_ASHOST, GORFC_TEST_SYSNR, GORFC_TEST_CLIENT, GORFC_TEST_USER, and GORFC_TEST_PASSWD are required"
		printPreflight(out, args, 1)
		return
	}
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
	attrs, _ := c.Attributes()
	out["connection"] = "ok"
	out["attributes"] = map[string]any{"sys_id": attrs.SysID, "client": attrs.Client, "user": attrs.User}
	printPreflight(out, args, 0)
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
