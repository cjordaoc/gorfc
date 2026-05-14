// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"sync/atomic"
	"time"
)

// Throughput accumulates per-connection traffic statistics.
// In Tier 2 the SDK-side counters require SAP NWRFC SDK 7.53+
// (`RfcCreateThroughput` and friends, 🟡 verify); when the
// loaded SDK is older — or the active backend is the no-SDK
// stub / mock — the wrapper falls back to Go-side counters fed
// by the Conn call path.
//
// Use:
//
//	tp := nwrfc.NewThroughput()
//	tp.Attach(conn)
//	... rfc calls ...
//	stats := tp.Stats()
//
// The struct is safe for concurrent use; every field is updated
// atomically.
type Throughput struct {
	calls           atomic.Int64
	bytesSent       atomic.Int64
	bytesReceived   atomic.Int64
	applicationTime atomic.Int64 // microseconds
	totalTime       atomic.Int64 // microseconds
	serializeTime   atomic.Int64
	deserializeTime atomic.Int64
	createdAtNano   atomic.Int64 // time.Time.UnixNano of the anchor
}

// NewThroughput constructs an empty counter anchored at the
// current time.
func NewThroughput() *Throughput {
	tp := &Throughput{}
	tp.createdAtNano.Store(time.Now().UnixNano())
	return tp
}

// Attach binds tp to c so subsequent successful [Call]s on c
// update tp's counters via the Go-side fallback path.
//
// The Go-side fallback increments Calls on every successful
// invocation but does NOT populate the bytes/timing counters
// (BytesSent, BytesReceived, ApplicationTime, TotalTime,
// SerializeTime, DeserializeTime) — those are only available
// from the SDK-side throughput counters, which require SAP
// NWRFC SDK 7.53+ (`RfcSetThroughputOnConnection`, 🟡
// verification pending) and are wired in a separate
// capability-gated PR.
//
// Attach returns an error only when c is nil. A backend that
// does not implement the SDK-side throughput hook is not an
// error: the Go-side fallback is always available and
// sufficient for call counting.
func (tp *Throughput) Attach(c *Conn) error {
	if c == nil {
		return &BrokenConnectionError{Reason: "nil Conn", Cause: ErrConnClosed}
	}
	c.tp = tp
	return nil
}

// ThroughputStats is a snapshot of the counters.
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

// Stats returns a snapshot. WallTime is computed as the elapsed
// time since the anchor recorded at [NewThroughput] / [Reset]
// (stored as a UnixNano atomic so concurrent Stats / Reset is
// race-free).
func (tp *Throughput) Stats() ThroughputStats {
	return ThroughputStats{
		Calls:           tp.calls.Load(),
		BytesSent:       tp.bytesSent.Load(),
		BytesReceived:   tp.bytesReceived.Load(),
		ApplicationTime: time.Duration(tp.applicationTime.Load()) * time.Microsecond,
		TotalTime:       time.Duration(tp.totalTime.Load()) * time.Microsecond,
		SerializeTime:   time.Duration(tp.serializeTime.Load()) * time.Microsecond,
		DeserializeTime: time.Duration(tp.deserializeTime.Load()) * time.Microsecond,
		WallTime:        time.Since(time.Unix(0, tp.createdAtNano.Load())),
	}
}

// Reset clears all counters and re-anchors WallTime to the
// current time.
func (tp *Throughput) Reset() {
	tp.calls.Store(0)
	tp.bytesSent.Store(0)
	tp.bytesReceived.Store(0)
	tp.applicationTime.Store(0)
	tp.totalTime.Store(0)
	tp.serializeTime.Store(0)
	tp.deserializeTime.Store(0)
	tp.createdAtNano.Store(time.Now().UnixNano())
}

// observe updates counters from a single completed call. Used
// by the Go-side fallback path wired in [Call]; on that path
// the bytes/timing arguments are zero (only Calls is
// meaningful). SDK-side throughput populates the full set of
// counters via the SDK callbacks instead.
func (tp *Throughput) observe(bytesSent, bytesReceived int64, app, total time.Duration) {
	tp.calls.Add(1)
	tp.bytesSent.Add(bytesSent)
	tp.bytesReceived.Add(bytesReceived)
	tp.applicationTime.Add(int64(app / time.Microsecond))
	tp.totalTime.Add(int64(total / time.Microsecond))
}
