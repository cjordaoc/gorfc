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
	"strings"
	"time"

	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := nwrfc.EnsureSDK(); err != nil {
		fail("nwrfc CLI requires SDK: %v", err)
	}
	switch os.Args[1] {
	case "version":
		fmt.Printf("nwrfc CLI; SDK %s; capabilities=%+v\n",
			nwrfc.SDKVersion(), nwrfc.Capabilities())
	case "ping":
		ping()
	case "describe":
		if len(os.Args) < 3 {
			fail("usage: nwrfc describe FUNCTION_NAME")
		}
		describe(os.Args[2])
	case "call":
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
  describe FUNCTION     Print the function descriptor as JSON.
  call FUNCTION KV...   Invoke a function with KEY=VALUE params.
  version               Print SDK + library versions.

Connection parameters come from GORFC_TEST_USER, GORFC_TEST_PASSWD,
GORFC_TEST_ASHOST, GORFC_TEST_SYSNR, GORFC_TEST_CLIENT, GORFC_TEST_LANG.`)
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
		fail("Open: %v", err)
	}
	defer c.Close()
	if err := c.Ping(ctx); err != nil {
		fail("Ping: %v", err)
	}
	attrs, _ := c.Attributes()
	fmt.Printf("OK: %s/%s as %s\n", attrs.SysID, attrs.Client, attrs.User)
}

func describe(fn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := nwrfc.Open(ctx, paramsFromEnv())
	if err != nil {
		fail("Open: %v", err)
	}
	defer c.Close()
	d, err := c.Describe(ctx, fn)
	if err != nil {
		fail("Describe: %v", err)
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
		fail("Open: %v", err)
	}
	defer c.Close()
	out, err := nwrfc.CallMap(ctx, c, fn, in)
	if err != nil {
		fail("Call %s: %v", fn, err)
	}
	bytes, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(bytes))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nwrfc: "+format+"\n", args...)
	os.Exit(1)
}
