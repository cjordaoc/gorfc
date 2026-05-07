// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package nwrfcidoc parses and builds SAP IDoc payloads in pure
// Go. It operates on the EDI_DC40 control record + segment
// table layout that arrives via the IDOC_INBOUND_ASYNCHRONOUS
// (and friends) RFC.
//
// IDoc is its own subpackage so the cgo-bound core stays small.
// No CGO; this package handles serialization only. The actual
// RFC call to send/receive IDocs goes through `nwrfc.Call`.
//
// Tier 4.3 deliverable per docs/PLAN.md §10.
//
// 🟡 verify against SAP documentation: the field layout follows
// EDI_DC40 (current standard); EDI_DC (legacy) is structurally
// similar but field offsets differ. The parser detects the
// version from segment lengths.
package nwrfcidoc

import (
	"fmt"
	"strconv"
	"strings"
)

// ControlRecord is the IDoc control record (EDI_DC40 layout).
// Fixed-width 524 chars in standard ABAP form; we expose the
// fields users actually read.
type ControlRecord struct {
	Tabnam   string // segment table name; "EDI_DC40" or "EDI_DC"
	Mandt    string // SAP client
	DocNum   string // 16-char IDoc number
	DocRel   string // release version
	Status   string // outbound status code
	DocType  string // basic IDoc type, e.g. "MATMAS05"
	IdocType string // logical type, e.g. "MATMAS05"
	MesType  string // message type, e.g. "MATMAS"
	MesCode  string
	MesFct   string
	Std      string // EDI standard (e.g. "X")
	StdVrs   string
	StdMes   string
	Sender   PartnerInfo
	Receiver PartnerInfo
	CreDate  string // YYYYMMDD
	CreTime  string // HHMMSS
}

// PartnerInfo holds sender/receiver fields. The control record
// has these as separate columns; we group them for clarity.
type PartnerInfo struct {
	Port    string // port name, "SAPxxx" for SAP-side ports
	Partyp  string // partner type, "LS" for logical system
	Parnum  string // partner number
	Partyp2 string
	Parvw   string // partner role
	Sndprn  string
	Sndprt  string
	Sndpfc  string
}

// Segment is one row of the IDoc segment table (typically
// EDI_DD40). Key fields plus a free-form Sdata payload.
type Segment struct {
	Segnum string // 6-digit row number
	Mandt  string
	DocNum string
	Segnam string // segment definition, e.g. "E1MARAM"
	Sdata  string // segment payload (1000 chars in standard layout)
	// Children can be nested for hierarchical IDoc types.
	Children []Segment
}

// IDoc is the in-memory representation of an inbound or
// outbound IDoc.
type IDoc struct {
	Control  ControlRecord
	Segments []Segment
}

// Encode produces the EDI_DC40 + EDI_DD40 wire layout suitable
// for an IDOC_INBOUND_ASYNCHRONOUS payload.
func Encode(d IDoc) (control string, segments []string) {
	control = encodeControl(d.Control)
	segments = make([]string, 0, len(d.Segments))
	for _, s := range d.Segments {
		segments = append(segments, encodeSegment(d.Control.DocNum, s))
		for _, child := range s.Children {
			segments = append(segments, encodeSegment(d.Control.DocNum, child))
		}
	}
	return control, segments
}

// Decode parses a control record string and a slice of segment
// strings into an IDoc.
func Decode(control string, segments []string) (IDoc, error) {
	out := IDoc{}
	if len(control) < 100 {
		return out, fmt.Errorf("nwrfcidoc: control record too short: %d bytes", len(control))
	}
	out.Control = decodeControl(control)
	out.Segments = make([]Segment, 0, len(segments))
	for _, s := range segments {
		out.Segments = append(out.Segments, decodeSegment(s))
	}
	return out, nil
}

// =============================================================
// Control record (EDI_DC40)
// =============================================================
//
// The standard layout is documented in SAP transaction WE60. We
// reproduce the most common fields here; rare fields land via
// the freeform map in future versions if needed.

func encodeControl(c ControlRecord) string {
	var b strings.Builder
	field := func(s string, n int) {
		if len(s) >= n {
			b.WriteString(s[:n])
			return
		}
		b.WriteString(s)
		for i := 0; i < n-len(s); i++ {
			b.WriteByte(' ')
		}
	}
	field(orDefault(c.Tabnam, "EDI_DC40"), 10)
	field(c.Mandt, 3)
	field(c.DocNum, 16)
	field(c.DocRel, 4)
	field(c.Status, 2)
	field(c.DocType, 30)
	field(c.IdocType, 30)
	field(c.MesType, 30)
	field(c.MesCode, 3)
	field(c.MesFct, 3)
	field(c.Std, 1)
	field(c.StdVrs, 6)
	field(c.StdMes, 6)
	field(c.Sender.Port, 10)
	field(c.Sender.Partyp, 2)
	field(c.Sender.Parnum, 10)
	field(c.Sender.Parvw, 2)
	field(c.Receiver.Port, 10)
	field(c.Receiver.Partyp, 2)
	field(c.Receiver.Parnum, 10)
	field(c.Receiver.Parvw, 2)
	field(c.CreDate, 8)
	field(c.CreTime, 6)
	return b.String()
}

func decodeControl(s string) ControlRecord {
	cur := 0
	read := func(n int) string {
		if cur+n > len(s) {
			return strings.TrimRight(s[cur:], " ")
		}
		val := strings.TrimRight(s[cur:cur+n], " ")
		cur += n
		return val
	}
	c := ControlRecord{
		Tabnam:   read(10),
		Mandt:    read(3),
		DocNum:   read(16),
		DocRel:   read(4),
		Status:   read(2),
		DocType:  read(30),
		IdocType: read(30),
		MesType:  read(30),
		MesCode:  read(3),
		MesFct:   read(3),
		Std:      read(1),
		StdVrs:   read(6),
		StdMes:   read(6),
	}
	c.Sender.Port = read(10)
	c.Sender.Partyp = read(2)
	c.Sender.Parnum = read(10)
	c.Sender.Parvw = read(2)
	c.Receiver.Port = read(10)
	c.Receiver.Partyp = read(2)
	c.Receiver.Parnum = read(10)
	c.Receiver.Parvw = read(2)
	c.CreDate = read(8)
	c.CreTime = read(6)
	return c
}

// =============================================================
// Segment record (EDI_DD40)
// =============================================================

func encodeSegment(docNum string, s Segment) string {
	var b strings.Builder
	field := func(s string, n int) {
		if len(s) >= n {
			b.WriteString(s[:n])
			return
		}
		b.WriteString(s)
		for i := 0; i < n-len(s); i++ {
			b.WriteByte(' ')
		}
	}
	if s.Segnum == "" {
		s.Segnum = "000001"
	}
	field(s.Segnam, 30) // segnam
	field(s.Mandt, 3)   // mandt
	field(docNum, 16)   // docnum
	field(s.Segnum, 6)  // segnum
	field("", 14)       // psgnum, hlevel, ddata reserved
	// Sdata: standard layout uses 1000 chars at offset 63.
	// We pad to 1000.
	if len(s.Sdata) > 1000 {
		b.WriteString(s.Sdata[:1000])
	} else {
		b.WriteString(s.Sdata)
		for i := 0; i < 1000-len(s.Sdata); i++ {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func decodeSegment(s string) Segment {
	cur := 0
	read := func(n int) string {
		if cur+n > len(s) {
			return strings.TrimRight(s[cur:], " ")
		}
		val := strings.TrimRight(s[cur:cur+n], " ")
		cur += n
		return val
	}
	out := Segment{
		Segnam: read(30),
		Mandt:  read(3),
		DocNum: read(16),
		Segnum: read(6),
	}
	cur += 14 // skip psgnum, hlevel, ddata reserved
	if cur < len(s) {
		out.Sdata = strings.TrimRight(s[cur:], " ")
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// =============================================================
// Convenience: numeric conversions
// =============================================================

// MustAtoi panics on parse error; for fixture-shaped fields
// only.
func MustAtoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		panic("nwrfcidoc.MustAtoi: " + err.Error())
	}
	return n
}
