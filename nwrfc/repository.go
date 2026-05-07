// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"context"
	"sync"

	"github.com/cjordaoc/gorfc/internal/backend"
)

// Repository is a cross-connection metadata cache. The SDK
// already maintains a per-process cache via
// `RfcGetFunctionDesc`; Repository sits on top of it and adds:
//
//   - Pre-load of a known list of functions during process
//     start (saves the first-call latency hit on every Conn).
//   - Snapshot / Apply for serializing a metadata bundle to
//     disk and reloading it (useful for code generators that
//     want to operate on a frozen schema).
//   - Cross-Conn sharing: multiple Conns to the same SAP
//     system share the same descriptor instances, reducing
//     memory pressure when the function set is large.
//
// Repository is process-level state; one Repository covers
// every Conn opened to a given SAP system. The first Describe
// for a function populates the cache; subsequent Describes on
// any Conn read from the cache directly.
//
// 🟡 The SDK-side cache is keyed by SYSID; Repository
// follows the same convention via [Repository.Use].
type Repository struct {
	mu    sync.RWMutex
	bySys map[string]map[string]backend.FunctionDescriptor
}

// NewRepository constructs an empty Repository.
func NewRepository() *Repository {
	return &Repository{
		bySys: map[string]map[string]backend.FunctionDescriptor{},
	}
}

// Use returns a Repository scoped to the SAP system identified
// by the connection. The returned helper provides Describe /
// Preload / Snapshot / Apply for that system specifically.
func (r *Repository) Use(c *Conn) *RepositoryView {
	attrs, _ := c.Attributes()
	return &RepositoryView{
		repo:  r,
		conn:  c,
		sysID: attrs.SysID,
	}
}

// RepositoryView is a Conn-bound handle on the shared
// Repository.
type RepositoryView struct {
	repo  *Repository
	conn  *Conn
	sysID string
}

// Describe returns the descriptor for fn, fetching from the
// SAP system on cache miss.
func (v *RepositoryView) Describe(ctx context.Context, fn string) (backend.FunctionDescriptor, error) {
	if d, ok := v.lookup(fn); ok {
		return d, nil
	}
	d, err := v.conn.Describe(ctx, fn)
	if err != nil {
		return backend.FunctionDescriptor{}, err
	}
	v.store(fn, d)
	return d, nil
}

// Preload fetches descriptors for every name in fns and caches
// them. Useful at process start to avoid first-call latency.
func (v *RepositoryView) Preload(ctx context.Context, fns ...string) error {
	for _, fn := range fns {
		if _, err := v.Describe(ctx, fn); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot returns a copy of the cached descriptors for the
// view's SAP system. Suitable for serializing to disk for
// codegen tools.
func (v *RepositoryView) Snapshot() map[string]backend.FunctionDescriptor {
	v.repo.mu.RLock()
	defer v.repo.mu.RUnlock()
	src := v.repo.bySys[v.sysID]
	out := make(map[string]backend.FunctionDescriptor, len(src))
	for k, d := range src {
		out[k] = d
	}
	return out
}

// Apply replaces the in-memory cache with the snapshot.
// Subsequent Describes hit the snapshot instead of the SAP
// system.
func (v *RepositoryView) Apply(snapshot map[string]backend.FunctionDescriptor) {
	v.repo.mu.Lock()
	defer v.repo.mu.Unlock()
	v.repo.bySys[v.sysID] = make(map[string]backend.FunctionDescriptor, len(snapshot))
	for k, d := range snapshot {
		v.repo.bySys[v.sysID][k] = d
	}
}

// Invalidate drops a cached descriptor and asks the SDK to
// drop its own cache entry as well.
func (v *RepositoryView) Invalidate(fn string) error {
	v.repo.mu.Lock()
	if m, ok := v.repo.bySys[v.sysID]; ok {
		delete(m, fn)
	}
	v.repo.mu.Unlock()
	return InvalidateMetadata(fn)
}

func (v *RepositoryView) lookup(fn string) (backend.FunctionDescriptor, bool) {
	v.repo.mu.RLock()
	defer v.repo.mu.RUnlock()
	if m, ok := v.repo.bySys[v.sysID]; ok {
		d, ok := m[fn]
		return d, ok
	}
	return backend.FunctionDescriptor{}, false
}

func (v *RepositoryView) store(fn string, d backend.FunctionDescriptor) {
	v.repo.mu.Lock()
	defer v.repo.mu.Unlock()
	if v.repo.bySys[v.sysID] == nil {
		v.repo.bySys[v.sysID] = map[string]backend.FunctionDescriptor{}
	}
	v.repo.bySys[v.sysID][fn] = d
}
