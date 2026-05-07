// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

// Command nwrfc-bridge exposes RFC over HTTP/JSON. POST a
// JSON body to /rfc/<FUNCTION_NAME>; the bridge translates the
// payload to a CallParams map, invokes the RFC, and returns
// the response as JSON.
//
// Tier 4.4 deliverable per docs/PLAN.md §10. Useful for legacy
// services that cannot link cgo or want a tiny JSON ↔ RFC
// gateway in a sidecar.
//
// SECURITY: this command does NOT do authentication. Deploy
// behind a reverse proxy that enforces auth/authz appropriate
// to the SAP system the bridge connects to. The bridge MUST
// run on a trusted network; never expose it to the internet
// directly.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cjordaoc/gorfc/internal/backend"
	"github.com/cjordaoc/gorfc/nwrfc"
	"github.com/cjordaoc/gorfc/nwrfcotel"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	poolSize := flag.Int("pool", 8, "connection pool size")
	flag.Parse()

	if err := nwrfc.EnsureSDK(); err != nil {
		fmt.Fprintf(os.Stderr, "nwrfc-bridge: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(nwrfcotel.NewRedactHandler(slog.NewJSONHandler(os.Stderr, nil)))
	slog.SetDefault(logger)

	pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params: nwrfc.Params{
			AsHost: os.Getenv("GORFC_TEST_ASHOST"),
			SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
			Client: os.Getenv("GORFC_TEST_CLIENT"),
			User:   os.Getenv("GORFC_TEST_USER"),
			Passwd: os.Getenv("GORFC_TEST_PASSWD"),
			Lang:   os.Getenv("GORFC_TEST_LANG"),
		},
		MinSize:        2,
		MaxSize:        *poolSize,
		IdleTimeout:    5 * time.Minute,
		AcquireTimeout: *timeout,
	})
	if err != nil {
		logger.Error("pool init failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.Handle("/rfc/", &rfcHandler{pool: pool, timeout: *timeout, logger: logger})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		stats := pool.Stats()
		json.NewEncoder(w).Encode(map[string]any{
			"sdk":  nwrfc.SDKVersion().String(),
			"open": stats.Open,
			"idle": stats.Idle,
			"caps": nwrfc.Capabilities(),
		})
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

type rfcHandler struct {
	pool    *nwrfc.Pool
	timeout time.Duration
	logger  *slog.Logger
}

func (h *rfcHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	fn := strings.TrimPrefix(r.URL.Path, "/rfc/")
	if fn == "" || strings.Contains(fn, "/") {
		http.Error(w, "function name required", http.StatusBadRequest)
		return
	}
	var body backend.CallParams
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	var resp backend.CallParams
	err := h.pool.Do(ctx, func(c *nwrfc.Conn) error {
		var err error
		resp, err = nwrfc.CallMap(ctx, c, fn, body)
		return err
	})
	if err != nil {
		h.logger.Warn("rfc failed",
			"fn", fn,
			"category", nwrfc.CategoryOf(err).String(),
			"err", err)
		status := http.StatusBadGateway
		switch nwrfc.CategoryOf(err) {
		case backend.CategoryLogon, backend.CategoryConfig:
			status = http.StatusUnauthorized
		case backend.CategoryABAPApp, backend.CategoryABAPRuntime:
			status = http.StatusBadRequest
		case backend.CategoryTimeout, backend.CategoryCancelled:
			status = http.StatusGatewayTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
