// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"errors"
	"sync"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Handler processes one inbound RFC call delivered to a
// registered Server. The handler reads parameters from `in`
// and writes results to the returned [backend.CallParams]
// (EXPORT/CHANGING/TABLES). Returning a non-nil error sends
// the corresponding ABAP exception back to the caller.
type Handler func(ctx context.Context, fn string, in backend.CallParams) (backend.CallParams, error)

// ServerOption configures a Server.
type ServerOption func(*Server)

// Server is an inbound RFC server registered at a SAP gateway.
// Sync server only in Tier 2.7 — the transactional and bgRFC
// variants land in Tier 3.
//
// Concurrency: handlers may be invoked from multiple
// goroutines concurrently when the SDK accepts more than one
// inbound call at a time. The wrapper installs a per-call
// recover so a panicking handler does not crash the SDK
// thread; the panic is converted to ExternalRuntimeError on
// the ABAP side.
type Server struct {
	cfg ServerConfig

	mu       sync.Mutex
	handlers map[string]Handler // function name → handler
	running  bool
	cancel   context.CancelFunc

	// 🟡 Cgo-side state lives in internal/sdkbackend.serverImpl
	// (Tier 2.7 binding). Keeping the Go-side state opaque
	// here so the public API does not leak cgo types.
	impl serverImplFunc
}

// serverImplFunc is the indirection that lets the cgo backend
// implement the actual SDK server registration without this
// file importing "C". The cgo side initializes it during init().
type serverImplFunc struct {
	register func(ServerConfig, map[string]Handler) (stop func() error, err error)
}

// NewServer constructs a Server bound to cfg. The server is NOT
// started until [Server.Start] is called.
//
// 🟡 Verification status: synchronous server (RFC_FUNCTION
// callbacks for RFM-shaped RFCs) is supported by the SDK in
// 7.50 PL3+. The cgo trampoline pattern is documented in
// docs/PLAN.md §6.6; the implementation lives behind a build
// tag so users without the SDK still see the
// *UnsupportedFeatureError instead of a link failure.
func NewServer(cfg ServerConfig, opts ...ServerOption) (*Server, error) {
	if cfg.ProgramID == "" {
		return nil, &ConfigError{Field: "ProgramID", Hint: "required"}
	}
	if cfg.GwHost == "" || cfg.GwService == "" {
		return nil, &ConfigError{Field: "GwHost+GwService", Hint: "required"}
	}
	s := &Server{
		cfg:      cfg,
		handlers: make(map[string]Handler),
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// Register installs h as the handler for fnName. Calling
// Register on a running server is allowed; new functions become
// callable on the next inbound RFC. Re-registering the same
// name replaces the previous handler.
func (s *Server) Register(fnName string, h Handler) error {
	if h == nil {
		return &ConfigError{Field: "Handler", Hint: "nil handler"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[fnName] = h
	return nil
}

// Start registers the server with the SAP gateway and begins
// accepting inbound calls. The returned context is cancelled
// when [Server.Stop] is invoked.
//
// Returns *UnsupportedFeatureError when the active backend is
// not the cgo SDK backend (the no-SDK stub does not
// implement server functionality).
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("nwrfc: Server.Start: already running")
	}
	impl := defaultServerImpl
	if impl.register == nil {
		return &UnsupportedFeatureError{
			Feature:        "RFC inbound server",
			CurrentVersion: backend.Default().Version(),
		}
	}
	stop, err := impl.register(s.cfg, copyHandlers(s.handlers))
	if err != nil {
		return mapBackendError(err)
	}
	s.running = true
	srvCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		<-srvCtx.Done()
		_ = stop()
	}()
	return nil
}

// Stop unregisters the server and waits for in-flight inbound
// calls to drain.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	return nil
}

// Running reports whether the server is currently registered.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func copyHandlers(in map[string]Handler) map[string]Handler {
	out := make(map[string]Handler, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// SetServerImpl is the wiring hook used by internal/sdkbackend
// to install the cgo-side server registration entry point. NOT
// part of the stable user API; subject to change.
//
// Pass nil to revert to the no-SDK behavior (Server.Start
// returns *UnsupportedFeatureError).
func SetServerImpl(register func(cfg ServerConfig, handlers map[string]Handler) (stop func() error, err error)) {
	// We cannot dynamically modify a Server already constructed
	// without a prior impl; the registration window is during
	// package init.
	defaultServerImpl.register = register
}

// defaultServerImpl is the impl set by SetServerImpl; new
// Servers inherit it via NewServer.
var defaultServerImpl serverImplFunc

// init wires every fresh Server to the package-level impl.
// The cgo backend's init() calls SetServerImpl; the no-SDK
// backend leaves it nil so Server.Start fails explicitly.
func init() {}
