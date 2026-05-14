// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build nwrfc_nosdk

package docs_test

import (
	"context"
	"testing"

	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
	"github.com/cjordaoc/gorfc/nwrfcparam"
)

type userGetDetailIn struct {
	Username string `rfc:"USERNAME"`
}

type userAddress struct {
	FullName string `rfc:"FULLNAME"`
}

type userGetDetailOut struct {
	Address userAddress             `rfc:"ADDRESS"`
	Return  []nwrfcparam.BAPIReturn `rfc:"RETURN"`
}

func TestIntegrationGuideMockPattern(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("BAPI_USER_GET_DETAIL", func(context.Context, nwrfcmock.CallParams) (nwrfcmock.CallParams, error) {
		return nwrfcmock.CallParams{
			"ADDRESS": map[string]any{"FULLNAME": "Jane Doe"},
			"RETURN": []map[string]any{{
				"TYPE":   "S",
				"ID":     "01",
				"NUMBER": "000",
			}},
		}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "mock",
		SysNr:  "00",
		Client: "100",
		User:   "tester",
		Passwd: "secret",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var out userGetDetailOut
	raw, err := nwrfc.Call(context.Background(), conn, "BAPI_USER_GET_DETAIL", userGetDetailIn{Username: "JDOE"}, &out)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if err := nwrfcparam.CheckRETURN(map[string]any(raw)); err != nil {
		t.Fatalf("CheckRETURN: %v", err)
	}
	if out.Address.FullName != "Jane Doe" {
		t.Fatalf("FullName=%q", out.Address.FullName)
	}
}
