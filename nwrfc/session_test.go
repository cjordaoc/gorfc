// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcmock"
)

func TestSession_FailedCommitAllowsRollbackCleanup(t *testing.T) {
	mock := nwrfcmock.New()
	commitErr := errors.New("commit failed before closure")
	var commitCount atomic.Int64
	var rollbackCount atomic.Int64
	mock.HandleFunc("BAPI_TRANSACTION_COMMIT", func(context.Context, backend.CallParams) (backend.CallParams, error) {
		commitCount.Add(1)
		return nil, commitErr
	})
	mock.HandleFunc("BAPI_TRANSACTION_ROLLBACK", func(context.Context, backend.CallParams) (backend.CallParams, error) {
		rollbackCount.Add(1)
		return backend.CallParams{}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	session, err := nwrfc.NewSession(context.Background(), conn)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := session.Commit(context.Background(), false); !errors.Is(err, commitErr) {
		t.Fatalf("Commit error = %v, want %v", err, commitErr)
	}
	if err := session.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback after failed Commit: %v", err)
	}

	if got := commitCount.Load(); got != 1 {
		t.Fatalf("commit calls = %d, want 1", got)
	}
	if got := rollbackCount.Load(); got != 1 {
		t.Fatalf("rollback calls = %d, want 1", got)
	}
}

func TestSession_ConcurrentCommitRollbackSingleWinner(t *testing.T) {
	mock := nwrfcmock.New()
	var commitCount atomic.Int64
	var rollbackCount atomic.Int64
	entered := make(chan string, 2)
	releaseInvoke := make(chan struct{})
	mock.HandleFunc("BAPI_TRANSACTION_COMMIT", func(context.Context, backend.CallParams) (backend.CallParams, error) {
		commitCount.Add(1)
		entered <- "commit"
		<-releaseInvoke
		return backend.CallParams{}, nil
	})
	mock.HandleFunc("BAPI_TRANSACTION_ROLLBACK", func(context.Context, backend.CallParams) (backend.CallParams, error) {
		rollbackCount.Add(1)
		entered <- "rollback"
		<-releaseInvoke
		return backend.CallParams{}, nil
	})
	restore := nwrfcmock.Install(mock)
	t.Cleanup(restore)

	conn, err := nwrfc.Open(context.Background(), nwrfc.Params{
		AsHost: "h", SysNr: "00", User: "u", Passwd: "p", Client: "100",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	session, err := nwrfc.NewSession(context.Background(), conn)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	before := mock.CallCount()
	start := make(chan struct{})
	done := make(chan error, 2)
	ready := make(chan struct{}, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ready <- struct{}{}
		<-start
		done <- session.Commit(context.Background(), false)
	}()
	go func() {
		defer wg.Done()
		ready <- struct{}{}
		<-start
		done <- session.Rollback(context.Background())
	}()

	<-ready
	<-ready
	close(start)

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no close BAPI reached backend")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("losing close path returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("competing close path did not finish while winning backend call was blocked")
	}

	select {
	case fn := <-entered:
		t.Fatalf("second close path reached backend as %s", fn)
	default:
	}

	close(releaseInvoke)
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatalf("winning close path returned error: %v", err)
	}

	if got := mock.CallCount() - before; got != 1 {
		t.Fatalf("concurrent close reached backend %d times, want 1", got)
	}
	if got := commitCount.Load() + rollbackCount.Load(); got != 1 {
		t.Fatalf("close BAPI calls = %d, want 1", got)
	}
}
