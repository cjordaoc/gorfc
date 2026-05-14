// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !nwrfc_nosdk

package nwrfc_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

func TestASAN_STFCStructure_MarshalingRoundTrip(t *testing.T) {
	if os.Getenv("GORFC_TEST_ASAN") != "1" {
		t.Skip("GORFC_TEST_ASAN=1 not set; ASan live SAP marshaling test is opt-in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := openASANTestConn(ctx, t)
	if err != nil {
		t.Fatalf("open ASan SAP test connection: %v", err)
	}
	defer conn.Close()

	row := map[string]any{
		"RFCFLOAT": "4.23456789",
		"RFCCHAR1": "A",
		"RFCCHAR2": "BC",
		"RFCCHAR4": "DEFG",
		"RFCINT1":  uint8(1),
		"RFCINT2":  int16(2),
		"RFCINT4":  int32(345),
		"RFCHEX3":  []byte{0x00, 0x0b, 0x0c},
		"RFCTIME":  "123456",
		"RFCDATE":  "20260514",
		"RFCDATA1": "HELLÖ SÄP",
		"RFCDATA2": "DATA222",
	}
	resp, err := nwrfc.CallMap(ctx, conn, "STFC_STRUCTURE", backend.CallParams{
		"IMPORTSTRUCT": row,
		"RFCTABLE":     []map[string]any{row},
	})
	if err != nil {
		t.Fatalf("STFC_STRUCTURE: %v", err)
	}

	echoStruct, ok := resp["ECHOSTRUCT"].(map[string]any)
	if !ok {
		t.Fatalf("ECHOSTRUCT has type %T, want map[string]any", resp["ECHOSTRUCT"])
	}
	if got := echoStruct["RFCDATA1"]; got != row["RFCDATA1"] {
		t.Fatalf("ECHOSTRUCT.RFCDATA1=%v, want %v", got, row["RFCDATA1"])
	}
	gotHex, ok := echoStruct["RFCHEX3"].([]byte)
	if !ok {
		t.Fatalf("ECHOSTRUCT.RFCHEX3 has type %T, want []byte", echoStruct["RFCHEX3"])
	}
	if !bytes.Equal(gotHex, row["RFCHEX3"].([]byte)) {
		t.Fatalf("ECHOSTRUCT.RFCHEX3=%v, want %v", gotHex, row["RFCHEX3"])
	}

	table, ok := resp["RFCTABLE"].([]map[string]any)
	if !ok {
		t.Fatalf("RFCTABLE has type %T, want []map[string]any", resp["RFCTABLE"])
	}
	if len(table) == 0 {
		t.Fatal("RFCTABLE is empty")
	}
	if got := table[0]["RFCDATA2"]; got != row["RFCDATA2"] {
		t.Fatalf("RFCTABLE[0].RFCDATA2=%v, want %v", got, row["RFCDATA2"])
	}
	gotTableHex, ok := table[0]["RFCHEX3"].([]byte)
	if !ok {
		t.Fatalf("RFCTABLE[0].RFCHEX3 has type %T, want []byte", table[0]["RFCHEX3"])
	}
	if !bytes.Equal(gotTableHex, row["RFCHEX3"].([]byte)) {
		t.Fatalf("RFCTABLE[0].RFCHEX3=%v, want %v", gotTableHex, row["RFCHEX3"])
	}
}

func openASANTestConn(ctx context.Context, t *testing.T) (*nwrfc.Conn, error) {
	t.Helper()
	if dest := os.Getenv("GORFC_TEST_DEST"); dest != "" {
		return nwrfc.OpenDest(ctx, dest)
	}
	required := []string{
		"GORFC_TEST_USER",
		"GORFC_TEST_PASSWD",
		"GORFC_TEST_ASHOST",
		"GORFC_TEST_SYSNR",
		"GORFC_TEST_CLIENT",
	}
	for _, name := range required {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required when GORFC_TEST_ASAN=1 and GORFC_TEST_DEST is unset", name)
		}
	}
	return nwrfc.Open(ctx, nwrfc.Params{
		User:   os.Getenv("GORFC_TEST_USER"),
		Passwd: os.Getenv("GORFC_TEST_PASSWD"),
		AsHost: os.Getenv("GORFC_TEST_ASHOST"),
		SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
		Client: os.Getenv("GORFC_TEST_CLIENT"),
		Lang:   os.Getenv("GORFC_TEST_LANG"),
	})
}
