// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build (linux || darwin || windows) && cgo && !nwrfc_nosdk

// Example: ping an SAP system via RFC_PING.
//
// Run:
//
//	export GORFC_TEST_USER=...
//	export GORFC_TEST_PASSWD=...
//	export GORFC_TEST_ASHOST=sap.example.invalid
//	export GORFC_TEST_SYSNR=00
//	export GORFC_TEST_CLIENT=100
//	export GORFC_TEST_LANG=EN
//	go run ./example/ping
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	if err := nwrfc.EnsureSDK(); err != nil {
		log.Fatalf("nwrfc: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := nwrfc.Open(ctx, nwrfc.Params{
		AsHost: os.Getenv("GORFC_TEST_ASHOST"),
		SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
		Client: os.Getenv("GORFC_TEST_CLIENT"),
		User:   os.Getenv("GORFC_TEST_USER"),
		Passwd: os.Getenv("GORFC_TEST_PASSWD"),
		Lang:   os.Getenv("GORFC_TEST_LANG"),
	})
	if err != nil {
		log.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		log.Fatalf("Ping: %v", err)
	}
	attrs, err := conn.Attributes()
	if err != nil {
		log.Fatalf("Attributes: %v", err)
	}
	fmt.Printf("connected to %s/%s as %s\n", attrs.SysID, attrs.Client, attrs.User)
	fmt.Printf("nwrfc SDK %s, capabilities=%+v\n", nwrfc.SDKVersion(), nwrfc.Capabilities())
}
