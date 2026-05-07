// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"sync"
)

// DestinationProvider produces connection parameters on
// demand. Lets services resolve destinations from a control
// plane (Vault, Consul, etcd, custom registry) instead of the
// static `sapnwrfc.ini` file.
//
// Inspired by JCo's `JCoDestinationDataProvider` and YaNco's
// pluggable runtime; the Go shape is intentionally minimal:
// callers register one provider per process via
// [SetDestinationProvider]; subsequent [OpenDest] calls
// resolve through it.
type DestinationProvider interface {
	// GetDestination resolves the named destination to
	// connection parameters. The returned Params should be
	// safe to log via slog (the wrapper redacts on its own).
	// Returning a nil error with empty Params signals "no
	// such destination" — the wrapper translates that to
	// *ConfigError.
	GetDestination(ctx context.Context, name string) (Params, error)
}

// ServerProvider produces inbound RFC server configuration
// for a named registration. Used by the Tier 2 sync server
// (T2.7) and the Tier 3 transactional server.
type ServerProvider interface {
	GetServer(ctx context.Context, name string) (ServerConfig, error)
}

// ServerConfig describes an inbound RFC server registration.
// Mirrors the SDK's `RFC_CONNECTION_PARAMETER` set for the
// server side: PROGRAM_ID, GWHOST, GWSERV, ...
type ServerConfig struct {
	ProgramID                                 string
	GwHost                                    string
	GwService                                 string
	Repository                                Params // params used to fetch metadata against an admin SAP system
	SncQOP, SncMyName, SncPartnerName, SncLib string
	Trace                                     string
	Extra                                     map[string]string
}

var (
	providerMu     sync.RWMutex
	destProvider   DestinationProvider
	serverProvider ServerProvider
)

// SetDestinationProvider installs p as the process-wide
// destination resolver. Pass nil to remove the resolver and
// fall back to sapnwrfc.ini.
func SetDestinationProvider(p DestinationProvider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	destProvider = p
}

// SetServerProvider installs p as the process-wide server
// configuration source.
func SetServerProvider(p ServerProvider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	serverProvider = p
}

// resolveDestination returns Params for the named destination,
// using the registered DestinationProvider if any. Returns
// (Params{Dest: name}, nil) when no provider is registered so
// the SDK falls through to sapnwrfc.ini lookup.
func resolveDestination(ctx context.Context, name string) (Params, error) {
	providerMu.RLock()
	p := destProvider
	providerMu.RUnlock()
	if p == nil {
		return Params{Dest: name}, nil
	}
	got, err := p.GetDestination(ctx, name)
	if err != nil {
		return Params{}, err
	}
	if got.AsHost == "" && got.MsHost == "" && got.WSHost == "" && got.Dest == "" {
		return Params{}, &ConfigError{Field: "dest", Hint: "destination not found: " + name}
	}
	return got, nil
}

// resolveServer returns ServerConfig for the named server.
func resolveServer(ctx context.Context, name string) (ServerConfig, error) {
	providerMu.RLock()
	p := serverProvider
	providerMu.RUnlock()
	if p == nil {
		return ServerConfig{}, &ConfigError{Field: "ServerProvider", Hint: "no provider registered (call SetServerProvider)"}
	}
	return p.GetServer(ctx, name)
}
