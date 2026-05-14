// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

const benchmarkLargeTableRows = 1000

func BenchmarkCallSmallStruct(b *testing.B) {
	smallStructResponse := backend.CallParams{
		"CUSTOMERNAME": "Example Customer",
		"COUNTRY":      "DE",
		"BALANCE":      "123.45",
		"CURRENCY":     "EUR",
		"STATUS":       "OK",
	}
	m := nwrfcmock.New()
	m.HandleFunc("Z_SMALL_BAPI", func(_ context.Context, in backend.CallParams) (backend.CallParams, error) {
		if in["COMPANYCODE"] == "" || in["CUSTOMER"] == "" {
			return nil, fmt.Errorf("missing required scalar input")
		}
		return smallStructResponse, nil
	})
	restore := nwrfcmock.Install(m)
	defer restore()

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer c.Close()

	type smallBAPIIn struct {
		CompanyCode string `rfc:"COMPANYCODE"`
		Customer    string `rfc:"CUSTOMER"`
		FiscalYear  int    `rfc:"FISCALYEAR"`
		Currency    string `rfc:"CURRENCY"`
		MaxRows     int32  `rfc:"MAXROWS"`
	}
	type smallBAPIOut struct {
		CustomerName string `rfc:"CUSTOMERNAME"`
		Country      string `rfc:"COUNTRY"`
		Balance      string `rfc:"BALANCE"`
		Currency     string `rfc:"CURRENCY"`
		Status       string `rfc:"STATUS"`
	}

	in := smallBAPIIn{
		CompanyCode: "1000",
		Customer:    "0000004711",
		FiscalYear:  2026,
		Currency:    "EUR",
		MaxRows:     10,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out smallBAPIOut
		if _, err := nwrfc.Call(context.Background(), c, "Z_SMALL_BAPI", in, &out); err != nil {
			b.Fatalf("Call: %v", err)
		}
		if out.Status != "OK" || out.Currency != in.Currency {
			b.Fatalf("out=%+v", out)
		}
	}
}

func BenchmarkCallLargeTable_Materialize(b *testing.B) {
	rows := make([]map[string]any, 0, benchmarkLargeTableRows)
	for i := 0; i < benchmarkLargeTableRows; i++ {
		rows = append(rows, benchmarkRow(i))
	}
	largeTableResponse := backend.CallParams{"ROWS": rows}

	m := nwrfcmock.New()
	m.HandleFunc("Z_LARGE_TABLE", func(context.Context, backend.CallParams) (backend.CallParams, error) {
		return largeTableResponse, nil
	})
	restore := nwrfcmock.Install(m)
	defer restore()

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer c.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := nwrfc.CallMap(context.Background(), c, "Z_LARGE_TABLE", nil)
		if err != nil {
			b.Fatalf("CallMap: %v", err)
		}
		rows := resp["ROWS"].([]map[string]any)
		if len(rows) != benchmarkLargeTableRows {
			b.Fatalf("rows=%d", len(rows))
		}
	}
}

func BenchmarkCallLargeTable_Stream(b *testing.B) {
	m := nwrfcmock.New()
	m.HandleTableStreamFunc("Z_LARGE_TABLE", "ROWS", func(context.Context, backend.CallParams) (backend.TableStream, error) {
		return nwrfcmock.TableRows(benchmarkLargeTableRows, benchmarkRow), nil
	})
	restore := nwrfcmock.Install(m)
	defer restore()

	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer c.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := nwrfc.CallTableStream(context.Background(), c, "Z_LARGE_TABLE", "ROWS", nil)
		if err != nil {
			b.Fatalf("CallTableStream: %v", err)
		}
		count := 0
		for {
			_, err := res.Next(context.Background())
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_ = res.Close()
				b.Fatalf("Next: %v", err)
			}
			count++
		}
		if err := res.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
		if count != benchmarkLargeTableRows {
			b.Fatalf("rows=%d", count)
		}
	}
}

func benchmarkRow(i int) map[string]any {
	return map[string]any{
		"ID":      int64(i),
		"MATNR":   fmt.Sprintf("%018d", i),
		"WERKS":   "1000",
		"QUAN":    "123.450",
		"COMMENT": "benchmark row",
	}
}
