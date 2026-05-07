// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfcidoc

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	in := IDoc{
		Control: ControlRecord{
			Mandt:    "100",
			DocNum:   "0000000000012345",
			DocType:  "MATMAS05",
			IdocType: "MATMAS05",
			MesType:  "MATMAS",
			Sender:   PartnerInfo{Port: "SAPDEV", Partyp: "LS", Parnum: "DEVCLNT100", Parvw: "SP"},
			Receiver: PartnerInfo{Port: "EXTSYS", Partyp: "LS", Parnum: "EXT100", Parvw: "RC"},
			CreDate:  "20260507",
			CreTime:  "120000",
		},
		Segments: []Segment{
			{Segnam: "E1MARAM", Sdata: "MATERIAL1234567890                MAKTL=raw_data"},
			{Segnam: "E1MAKTM", Sdata: "DESCRIPTION HERE"},
		},
	}
	control, segments := Encode(in)
	if len(control) < 100 {
		t.Errorf("control too short: %d", len(control))
	}
	if !strings.HasPrefix(control, "EDI_DC40  ") {
		t.Errorf("control header wrong: %q", control[:10])
	}
	if !strings.Contains(segments[0], "E1MARAM") {
		t.Errorf("segment[0] missing segnam: %q", segments[0][:30])
	}
	out, err := Decode(control, segments)
	if err != nil {
		t.Fatal(err)
	}
	if out.Control.DocNum != in.Control.DocNum {
		t.Errorf("DocNum %q vs %q", out.Control.DocNum, in.Control.DocNum)
	}
	if out.Control.MesType != "MATMAS" {
		t.Errorf("MesType %q", out.Control.MesType)
	}
	if len(out.Segments) != 2 {
		t.Fatalf("segments = %d want 2", len(out.Segments))
	}
	if out.Segments[0].Segnam != "E1MARAM" {
		t.Errorf("Segnam %q", out.Segments[0].Segnam)
	}
}

func TestDecode_TooShort(t *testing.T) {
	if _, err := Decode("short", nil); err == nil {
		t.Error("expected error")
	}
}

func TestMustAtoi(t *testing.T) {
	if n := MustAtoi(" 42 "); n != 42 {
		t.Errorf("got %d", n)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	MustAtoi("nope")
}
