// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfcmock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

func TestMock_RoundTrip(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("STFC_CONNECTION", func(_ context.Context, in backend.CallParams) (backend.CallParams, error) {
		return backend.CallParams{
			"ECHOTEXT": in["REQUTEXT"],
			"RESPTEXT": "pong",
		}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	type In struct {
		ReqText string `rfc:"REQUTEXT"`
	}
	type Out struct {
		EchoText string `rfc:"ECHOTEXT"`
		RespText string `rfc:"RESPTEXT"`
	}

	ctx := context.Background()
	c, err := nwrfc.Open(ctx, nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var out Out
	if _, err := nwrfc.Call(ctx, c, "STFC_CONNECTION", In{ReqText: "ping"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.EchoText != "ping" || out.RespText != "pong" {
		t.Errorf("got %+v", out)
	}
	if mock.CallCount() != 1 || mock.OpenCount() != 1 {
		t.Errorf("counters off: call=%d open=%d", mock.CallCount(), mock.OpenCount())
	}
}

func TestMock_HandlerError(t *testing.T) {
	mock := nwrfcmock.New()
	mock.HandleFunc("F", func(_ context.Context, _ backend.CallParams) (backend.CallParams, error) {
		return nil, &backend.SDKError{
			Op: "RfcInvoke",
			Info: backend.SDKErrorInfo{
				Group: backend.GroupLogonFailure,
				Key:   "RFC_LOGON_FAILURE",
			},
		}
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	ctx := context.Background()
	c, _ := nwrfc.Open(ctx, nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100"})
	defer c.Close()
	_, err := nwrfc.Call(ctx, c, "F", nil, nil)
	if !errors.Is(err, nwrfc.ErrLogon) {
		t.Errorf("err=%v want ErrLogon", err)
	}
}

func TestMock_NoHandler(t *testing.T) {
	mock := nwrfcmock.New()
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	ctx := context.Background()
	c, _ := nwrfc.Open(ctx, nwrfc.Params{AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100"})
	defer c.Close()
	_, err := nwrfc.Call(ctx, c, "UNKNOWN_FN", nil, nil)
	if err == nil {
		t.Error("expected error")
	}
}

func TestMock_VersionAndCapabilities(t *testing.T) {
	mock := nwrfcmock.New()
	if v := mock.Version(); v.Major != 7 || v.Minor != 50 {
		t.Errorf("default version wrong: %v", v)
	}
	mock.SetVersion(backend.Version{Major: 7, Minor: 53})
	if !mock.Version().AtLeast(7, 53, 0) {
		t.Error("SetVersion not effective")
	}
	caps := mock.Capabilities()
	if !caps.Throughput || !caps.BgRFC {
		t.Errorf("default caps wrong: %+v", caps)
	}
}
