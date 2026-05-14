// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

func newMockConn(t *testing.T, m *nwrfcmock.Mock) *nwrfc.Conn {
	t.Helper()
	restore := nwrfcmock.Install(m)
	t.Cleanup(restore)
	c, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCallTableStream_EarlyBreakClose(t *testing.T) {
	m := nwrfcmock.New()
	var closed atomic.Int64
	m.HandleTableStreamFunc("F", "ROWS", func(context.Context, backend.CallParams) (backend.TableStream, error) {
		return &countingStream{
			rows: []map[string]any{
				{"ID": int64(1)},
				{"ID": int64(2)},
			},
			closeCount: &closed,
		}, nil
	})
	c := newMockConn(t, m)

	res, err := nwrfc.CallTableStream(context.Background(), c, "F", "ROWS", nil)
	if err != nil {
		t.Fatalf("CallTableStream: %v", err)
	}
	row, err := res.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if row["ID"] != int64(1) {
		t.Fatalf("row=%v", row)
	}
	if err := res.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := res.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := closed.Load(); got != 1 {
		t.Fatalf("close count=%d want 1", got)
	}
}

func TestCallTableStream_CancelBeforeIterationCleansUp(t *testing.T) {
	m := nwrfcmock.New()
	var closed atomic.Int64
	m.HandleTableStreamFunc("F", "ROWS", func(context.Context, backend.CallParams) (backend.TableStream, error) {
		return &countingStream{
			rows:       []map[string]any{{"ID": int64(1)}},
			closeCount: &closed,
		}, nil
	})
	c := newMockConn(t, m)

	res, err := nwrfc.CallTableStream(context.Background(), c, "F", "ROWS", nil)
	if err != nil {
		t.Fatalf("CallTableStream: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = res.Next(ctx)
	if !errors.Is(err, nwrfc.ErrCancelled) {
		t.Fatalf("Next err=%v want ErrCancelled", err)
	}
	if got := closed.Load(); got != 1 {
		t.Fatalf("close count=%d want 1", got)
	}
}

func TestCallTableStream_AfterCloseReturnsExplicitError(t *testing.T) {
	m := nwrfcmock.New()
	m.HandleTableStreamFunc("F", "ROWS", func(context.Context, backend.CallParams) (backend.TableStream, error) {
		return nwrfcmock.TableRows(1, func(i int) map[string]any {
			return map[string]any{"ID": int64(i)}
		}), nil
	})
	c := newMockConn(t, m)

	res, err := nwrfc.CallTableStream(context.Background(), c, "F", "ROWS", nil)
	if err != nil {
		t.Fatalf("CallTableStream: %v", err)
	}
	if err := res.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := res.Next(context.Background()); !errors.Is(err, nwrfc.ErrStreamClosed) {
		t.Fatalf("Next after Close err=%v want ErrStreamClosed", err)
	}
}

func TestCallTableStream_EOFStillRequiresClose(t *testing.T) {
	m := nwrfcmock.New()
	var closed atomic.Int64
	m.HandleTableStreamFunc("F", "ROWS", func(context.Context, backend.CallParams) (backend.TableStream, error) {
		return &countingStream{closeCount: &closed}, nil
	})
	c := newMockConn(t, m)

	res, err := nwrfc.CallTableStream(context.Background(), c, "F", "ROWS", nil)
	if err != nil {
		t.Fatalf("CallTableStream: %v", err)
	}
	if _, err := res.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next err=%v want EOF", err)
	}
	if got := closed.Load(); got != 0 {
		t.Fatalf("stream auto-closed on EOF; close count=%d", got)
	}
	if err := res.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

type countingStream struct {
	rows       []map[string]any
	i          int
	closed     bool
	closeCount *atomic.Int64
}

func (s *countingStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, io.ErrClosedPipe
	}
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	row := s.rows[s.i]
	s.i++
	return row, nil
}

func (s *countingStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.closeCount != nil {
		s.closeCount.Add(1)
	}
	return nil
}
