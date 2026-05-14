// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// TableStream is a lazy RFC table response. The stream owns the
// live RFC function handle and pins the Conn until Close is
// called; always use defer res.Close() immediately after a
// successful CallTableStream.
//
// Example:
//
//	res, err := nwrfc.CallTableStream(ctx, conn, "BAPI_MATERIAL_GETLIST", "MATNRLIST", in)
//	if err != nil {
//	    return err
//	}
//	defer res.Close()
//
//	for {
//	    row, err := res.Next(ctx)
//	    if errors.Is(err, io.EOF) {
//	        break
//	    }
//	    if err != nil {
//	        return err
//	    }
//	    _ = row["MATERIAL"]
//	}
type TableStream struct {
	conn   *Conn
	stream backend.TableStream
	fn     string
	table  string

	closed atomic.Bool
	once   sync.Once
	err    error
}

// CallTableStream invokes fn and returns a lazy reader for one
// TABLES parameter. Call and CallMap continue to materialize
// results; this API is only for large tables where the caller
// accepts explicit resource ownership.
//
// While the returned stream is open, c is locked and must not be
// returned to a Pool. Close releases the backend stream, destroys
// the SDK function handle in the cgo backend, and unlocks c.
func CallTableStream(ctx context.Context, c *Conn, fn string, table string, in any, optsOpt ...CallOptions) (*TableStream, error) {
	if c == nil {
		return nil, &BrokenConnectionError{Reason: "nil Conn", Cause: ErrConnClosed}
	}
	if !c.Alive() {
		return nil, &BrokenConnectionError{Reason: "closed Conn", Cause: ErrConnClosed}
	}
	if table == "" {
		return nil, &ConfigError{Field: "table", Hint: "TABLES parameter name is required"}
	}

	sb, ok := c.backend.(backend.StreamingBackend)
	if !ok {
		return nil, &UnsupportedFeatureError{Feature: "lazy table streaming", CurrentVersion: c.backend.Version()}
	}

	var opts CallOptions
	if len(optsOpt) > 0 {
		opts = optsOpt[0]
	}
	inMap, err := marshalInput(in)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	stream, err := sb.InvokeTableStream(ctx, c.handle, fn, table, inMap, opts.toBackend())
	if err != nil {
		c.mu.Unlock()
		return nil, mapContextOrBackendError(fn, err)
	}
	return &TableStream{
		conn:   c,
		stream: stream,
		fn:     fn,
		table:  table,
	}, nil
}

// Next returns the next table row. It returns io.EOF after the
// final row. A context cancellation or non-EOF iteration error
// closes the stream idempotently before returning the mapped
// error.
func (s *TableStream) Next(ctx context.Context) (map[string]any, error) {
	if s == nil || s.closed.Load() {
		return nil, ErrStreamClosed
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, mapContextOrBackendError(s.fn, err)
	}
	row, err := s.stream.Next(ctx)
	if err == nil || errors.Is(err, io.EOF) {
		return row, err
	}
	_ = s.Close()
	return nil, mapContextOrBackendError(s.fn, err)
}

// Close releases the lazy stream. It is idempotent and should
// be called after EOF, early break, or any iteration error.
func (s *TableStream) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		s.closed.Store(true)
		if s.stream != nil {
			s.err = mapBackendError(s.stream.Close())
		}
		if s.conn != nil {
			s.conn.mu.Unlock()
		}
	})
	return s.err
}

func mapContextOrBackendError(fn string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Function: fn, Deadline: time.Now()}
	}
	if errors.Is(err, context.Canceled) {
		return &CancelledError{Function: fn, Cause: err}
	}
	mapped := mapBackendError(err)
	if mapped != err {
		return mapped
	}
	return fmt.Errorf("nwrfc: table stream %s: %w", fn, err)
}
