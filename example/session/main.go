// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build (linux || darwin || windows) && cgo && !nwrfc_nosdk

// Example: stateful Session with explicit Commit.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	if err := nwrfc.EnsureSDK(); err != nil {
		log.Fatalf("nwrfc: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	sess, err := nwrfc.NewSession(ctx, conn)
	if err != nil {
		log.Fatalf("NewSession: %v", err)
	}

	// Two stateful calls inside one LUW.
	_, err = sess.Call(ctx, "BAPI_PING", nil, nil)
	if err != nil {
		log.Printf("BAPI_PING: %v", err)
	}
	_, err = sess.Call(ctx, "RFC_PING", nil, nil)
	if err != nil {
		log.Printf("RFC_PING: %v", err)
	}

	if err := sess.Commit(ctx, false); err != nil {
		log.Fatalf("Commit: %v", err)
	}
	log.Print("session committed")
}
