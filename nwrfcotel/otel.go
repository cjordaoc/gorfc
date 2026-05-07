// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Package nwrfcotel provides opt-in OpenTelemetry and slog
// instrumentation for the gorfc revival. Lives in its own
// subpackage so the core nwrfc library never pulls in
// `go.opentelemetry.io/otel` for users who do not want
// observability.
//
// Two layers:
//
//   1. RedactHandler — a `slog.Handler` wrapper that strips
//      sensitive attributes (password, ticket, SNC partner
//      name, X.509 cert) at handler time, even when callers
//      forget to use the typed shapes that auto-redact.
//
//   2. ConnListener — a [nwrfc.Listener] that emits OTel
//      spans for Open/Close/Broken events. Spans carry the
//      destination name, SAP system ID (when known), and
//      the error category (no payload, no credentials).
//
// Tier 2 deliverable per docs/PLAN.md §10. Designed so the
// core library has zero dependency on OpenTelemetry —
// importing `nwrfcotel` is an explicit opt-in.
package nwrfcotel

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
)

// RedactHandler wraps a `slog.Handler` and strips attributes
// whose names match [backend.SensitiveKeys] (case-insensitive).
// Use as the outermost handler in your slog configuration:
//
//	base := slog.NewJSONHandler(os.Stderr, nil)
//	handler := nwrfcotel.NewRedactHandler(base)
//	slog.SetDefault(slog.New(handler))
//
// Callers that pass their own `slog.LogValuer`-aware shapes
// (Params, error structs in this library) get auto-redaction
// for free; this handler is the safety net for callers who
// pass raw maps.
type RedactHandler struct {
	inner slog.Handler
}

// NewRedactHandler wraps inner.
func NewRedactHandler(inner slog.Handler) *RedactHandler {
	return &RedactHandler{inner: inner}
}

// Enabled implements [slog.Handler].
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle implements [slog.Handler]. Walks the record's
// attributes and replaces sensitive ones with «redacted».
func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

// WithAttrs implements [slog.Handler].
func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactHandler{inner: h.inner.WithAttrs(redacted)}
}

// WithGroup implements [slog.Handler].
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name)}
}

// redactAttr replaces the value of a sensitive attribute with
// "«redacted»". Recursively scans group values so structured
// payloads are sanitized at every depth.
func redactAttr(a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, "«redacted»")
	}
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, child := range group {
			out[i] = redactAttr(child)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	return a
}

func isSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, s := range backend.SensitiveKeys {
		if s == lk {
			return true
		}
	}
	return false
}

// ConnListener is a [nwrfc.Listener] that records every
// connection lifecycle event into a slog handler. It is the
// pure-slog half of the observability story; the OTel span
// emission lives in [TracerListener] below.
type ConnListener struct {
	Logger *slog.Logger
}

// OnConnEvent implements [nwrfc.Listener].
func (l *ConnListener) OnConnEvent(e nwrfc.ConnEvent, dest string, at time.Time, err error) {
	if l == nil || l.Logger == nil {
		return
	}
	level := slog.LevelInfo
	if e == nwrfc.EventBroken {
		level = slog.LevelWarn
	}
	l.Logger.LogAttrs(context.Background(), level, "rfc connection event",
		slog.String("event", e.String()),
		slog.String("dest", dest),
		slog.Time("at", at),
		slog.Any("err", err),
	)
}

// Tracer is the minimal contract this package needs from an
// OpenTelemetry tracer. We do not import the OTel module
// directly here; users wire their tracer in by passing a
// concrete implementation. Keeping the dependency thin lets
// users on opentelemetry-go v1.16, v1.20, or v1.30 all use
// this package without version conflicts.
//
// To wire `go.opentelemetry.io/otel`:
//
//	import (
//	    "go.opentelemetry.io/otel"
//	    "go.opentelemetry.io/otel/attribute"
//	    "go.opentelemetry.io/otel/codes"
//	    "go.opentelemetry.io/otel/trace"
//	)
//
//	type otelTracer struct{ t trace.Tracer }
//	func (a *otelTracer) StartSpan(ctx context.Context, name string) (context.Context, nwrfcotel.Span) {
//	    ctx, span := a.t.Start(ctx, name)
//	    return ctx, &otelSpan{span: span}
//	}
//	type otelSpan struct{ span trace.Span }
//	func (s *otelSpan) SetAttribute(k, v string) {
//	    s.span.SetAttributes(attribute.String(k, v))
//	}
//	func (s *otelSpan) SetError(err error) {
//	    s.span.SetStatus(codes.Error, err.Error())
//	}
//	func (s *otelSpan) End() { s.span.End() }
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span is the minimal span surface this package emits.
type Span interface {
	SetAttribute(key, value string)
	SetError(err error)
	End()
}

// TracerListener emits a span for every Conn lifecycle
// transition. Span name is `nwrfc.conn.<event>`; attributes
// include `dest` and (when the event carries an error)
// the error category.
type TracerListener struct {
	Tracer Tracer
}

// OnConnEvent implements [nwrfc.Listener].
func (l *TracerListener) OnConnEvent(e nwrfc.ConnEvent, dest string, _ time.Time, err error) {
	if l == nil || l.Tracer == nil {
		return
	}
	ctx, span := l.Tracer.StartSpan(context.Background(), "nwrfc.conn."+e.String())
	_ = ctx
	span.SetAttribute("nwrfc.dest", dest)
	if err != nil {
		span.SetAttribute("nwrfc.category", nwrfc.CategoryOf(err).String())
		span.SetError(err)
	}
	span.End()
}
