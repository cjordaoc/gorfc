// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfcotel_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcotel"
)

func TestRedactHandler_StripsSensitiveAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(nwrfcotel.NewRedactHandler(base))

	logger.Info("login",
		"user", "demo",
		"passwd", "supersecret",
		"mysapsso2", "ticket-xyz",
		"snc_partnername", "p:CN=corp.example.invalid",
		"x509cert", "-----BEGIN CERT---",
	)
	out := buf.String()
	for _, leak := range []string{"supersecret", "ticket-xyz", "p:CN=corp.example.invalid", "BEGIN CERT"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "demo") {
		t.Error("dropped non-sensitive user")
	}
	if c := strings.Count(out, "«redacted»"); c < 4 {
		t.Errorf("redacted markers=%d want >=4: %s", c, out)
	}
}

func TestRedactHandler_GroupRecursion(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(nwrfcotel.NewRedactHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("nested",
		slog.Group("creds",
			slog.String("user", "demo"),
			slog.String("passwd", "secret"),
			slog.Group("snc",
				slog.String("snc_partnername", "p:CN=corp"),
			),
		),
	)
	out := buf.String()
	for _, leak := range []string{"secret", "p:CN=corp"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q: %s", leak, out)
		}
	}
}

func TestConnListener_LogsEvents(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	l := &nwrfcotel.ConnListener{Logger: logger}

	l.OnConnEvent(nwrfc.EventOpened, "DEV", time.Now(), nil)
	l.OnConnEvent(nwrfc.EventBroken, "DEV", time.Now(), nwrfc.ErrCommunication)

	out := buf.String()
	for _, want := range []string{`"event":"opened"`, `"event":"broken"`, `"dest":"DEV"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in: %s", want, out)
		}
	}
}

// fakeTracer / fakeSpan exercise the TracerListener without
// pulling in the real OTel module.
type fakeTracer struct {
	last *fakeSpan
}

func (f *fakeTracer) StartSpan(ctx context.Context, name string) (context.Context, nwrfcotel.Span) {
	s := &fakeSpan{name: name, attrs: map[string]string{}}
	f.last = s
	return ctx, s
}

type fakeSpan struct {
	name  string
	attrs map[string]string
	err   error
	ended bool
}

func (s *fakeSpan) SetAttribute(k, v string) { s.attrs[k] = v }
func (s *fakeSpan) SetError(err error)       { s.err = err }
func (s *fakeSpan) End()                     { s.ended = true }

func TestTracerListener_EmitsSpan(t *testing.T) {
	tr := &fakeTracer{}
	l := &nwrfcotel.TracerListener{Tracer: tr}
	l.OnConnEvent(nwrfc.EventBroken, "DEV", time.Now(), nwrfc.ErrCommunication)

	if tr.last == nil {
		t.Fatal("no span")
	}
	if tr.last.name != "nwrfc.conn.broken" {
		t.Errorf("name=%q", tr.last.name)
	}
	if tr.last.attrs["nwrfc.dest"] != "DEV" {
		t.Errorf("dest attr missing: %v", tr.last.attrs)
	}
	if tr.last.attrs["nwrfc.category"] != "communication" {
		t.Errorf("category attr=%q", tr.last.attrs["nwrfc.category"])
	}
	if !tr.last.ended {
		t.Error("span not ended")
	}
}
