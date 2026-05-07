// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build (linux || darwin || windows) && cgo && !nwrfc_nosdk

// Example: STFC_STRUCTURE round-trip.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

type ImportStruct struct {
	RFCFLOAT float64      `rfc:"RFCFLOAT"`
	RFCCHAR1 string       `rfc:"RFCCHAR1"`
	RFCCHAR2 string       `rfc:"RFCCHAR2"`
	RFCCHAR4 string       `rfc:"RFCCHAR4"`
	RFCINT1  int          `rfc:"RFCINT1"`
	RFCINT2  int          `rfc:"RFCINT2"`
	RFCINT4  int          `rfc:"RFCINT4"`
	RFCHEX3  []byte       `rfc:"RFCHEX3"`
	RFCTIME  backend.Time `rfc:"RFCTIME"`
	RFCDATE  backend.Date `rfc:"RFCDATE"`
	RFCDATA1 string       `rfc:"RFCDATA1"`
	RFCDATA2 string       `rfc:"RFCDATA2"`
}

type In struct {
	IMPORTSTRUCT ImportStruct `rfc:"IMPORTSTRUCT"`
}

type Out struct {
	ECHOSTRUCT ImportStruct `rfc:"ECHOSTRUCT"`
}

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

	now := time.Now().UTC()
	in := In{
		IMPORTSTRUCT: ImportStruct{
			RFCFLOAT: 1.23456789,
			RFCCHAR1: "A",
			RFCCHAR2: "BC",
			RFCCHAR4: "ÄBC",
			RFCINT1:  254,
			RFCINT2:  32766,
			RFCINT4:  999999999,
			RFCHEX3:  []byte{255, 254, 253},
			RFCTIME:  backend.Time{Hour: uint8(now.Hour()), Minute: uint8(now.Minute()), Second: uint8(now.Second())},
			RFCDATE:  backend.Date{Year: uint16(now.Year()), Month: uint16(now.Month()), Day: uint16(now.Day())},
			RFCDATA1: "HELLÖ SÄP",
			RFCDATA2: "DATA222",
		},
	}
	var out Out
	_, err = nwrfc.Call(ctx, conn, "STFC_STRUCTURE", in, &out)
	if err != nil {
		log.Fatalf("Call: %v", err)
	}
	fmt.Printf("echoed: %+v\n", out.ECHOSTRUCT)
}
