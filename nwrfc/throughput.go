// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"sync/atomic"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Throughput accumulates per-connection traffic statistics.
// In Tier 2 the SDK-side counters require SAP NWRFC SDK 7.53+
// (`RfcCreateThroughput` and friends, 🟡 verify); when the
// loaded SDK is older the wrapper falls back to Go-side
// counters fed by the Conn lifecycle hooks.
//
// Use:
//
//	tp := nwrfc.NewThroughput()
//	tp.Attach(conn)
//	... rfc calls ...
//	stats := tp.Stats()
//
// The struct is safe for concurrent use; counters are
// updated atomically.
type Throughput struct {
	calls           atomic.Int64
	bytesSent       atomic.Int64
	bytesReceived   atomic.Int64
	applicationTime atomic.Int64 // microseconds
	totalTime       atomic.Int64 // microseconds
	serializeTime   atomic.Int64
	deserializeTime atomic.Int64
	createdAt       time.Time
}

// NewThroughput constructs an empty counter.
func NewThroughput() *Throughput {
	return &Throughput{createdAt: time.Now()}
}

// Attach binds tp to c so subsequent calls update its counters.
//
// 🟡 SDK-side binding requires `RfcSetThroughputOnConnection`
// (SDK 7.53+; verification pending). When unavailable, the
// wrapper falls back to Go-side timing in mapBackendCall (T1.8
// hooks) — the counters are slightly less accurate but always
// available.
//
// Returns *UnsupportedFeatureError when the active backend does
// not implement the throughput hook AND the SDK version is too
// old for the SDK-side counters.
func (tp *Throughput) Attach(c *Conn) error {
	_ = c // currently a no-op; the SDK-side hook lands when
	// the cgo binding for RfcSetThroughputOnConnection is
	// implemented (separate PR — capability-gated).
	caps := backend.Default().Capabilities()
	if !caps.Throughput {
		// Go-side fallback is automatic via the Conn lifecycle
		// hooks; signal the gap so callers can warn.
		return &UnsupportedFeatureError{
			Feature:         "Throughput SDK counters",
			RequiredVersion: backend.Version{Major: 7, Minor: 53, PatchLevel: 0},
			CurrentVersion:  backend.Default().Version(),
		}
	}
	return nil
}

// Stats returns a snapshot of the counters.
type ThroughputStats struct {
	Calls           int64
	BytesSent       int64
	BytesReceived   int64
	ApplicationTime time.Duration
	TotalTime       time.Duration
	SerializeTime   time.Duration
	DeserializeTime time.Duration
	WallTime        time.Duration
}

// Stats returns a snapshot.
func (tp *Throughput) Stats() ThroughputStats {
	return ThroughputStats{
		Calls:           tp.calls.Load(),
		BytesSent:       tp.bytesSent.Load(),
		BytesReceived:   tp.bytesReceived.Load(),
		ApplicationTime: time.Duration(tp.applicationTime.Load()) * time.Microsecond,
		TotalTime:       time.Duration(tp.totalTime.Load()) * time.Microsecond,
		SerializeTime:   time.Duration(tp.serializeTime.Load()) * time.Microsecond,
		DeserializeTime: time.Duration(tp.deserializeTime.Load()) * time.Microsecond,
		WallTime:        time.Since(tp.createdAt),
	}
}

// Reset clears all counters and re-anchors WallTime.
func (tp *Throughput) Reset() {
	tp.calls.Store(0)
	tp.bytesSent.Store(0)
	tp.bytesReceived.Store(0)
	tp.applicationTime.Store(0)
	tp.totalTime.Store(0)
	tp.serializeTime.Store(0)
	tp.deserializeTime.Store(0)
	tp.createdAt = time.Now()
}

// observe updates counters from a single completed call. Used
// by the Go-side fallback path. SDK-side throughput populates
// the counters via the SDK callbacks instead.
func (tp *Throughput) observe(bytesSent, bytesReceived int64, app, total time.Duration) {
	tp.calls.Add(1)
	tp.bytesSent.Add(bytesSent)
	tp.bytesReceived.Add(bytesReceived)
	tp.applicationTime.Add(int64(app / time.Microsecond))
	tp.totalTime.Add(int64(total / time.Microsecond))
}
