// Package board's AgentRegistry caches *AgentManager instances per user
// across HTTP requests.
//
// Why this exists: StartAgent / StopAgent / SubmitFeedback need to reach
// the SAME manager instance from successive HTTP requests because the
// manager owns the runtime state for in-flight agent goroutines (cancel
// funcs, feedback queues, the runs map). Earlier code constructed a
// fresh manager per request, which made StopAgent and SubmitFeedback
// no-ops — they targeted a freshly built map with zero entries.
//
// Cache key: (userID, settings signature). The builder is called on
// every For() and returns both a manager and a signature. When the
// signature matches the cached entry, we discard the freshly built
// manager and return the cached one so in-flight runs remain
// reachable. When the signature differs (e.g. the user changed their
// repo path or codegen agent), we cancel any in-flight runs on the
// stale manager and replace the cache entry. Concurrent For() calls
// for the same user are coalesced via a per-user singleflight so the
// builder runs at most once.
package board

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

// ManagerBuilder constructs a *AgentManager for userID and returns a
// stable signature string derived from the inputs that affect manager
// construction. The registry uses the signature to detect when stored
// settings have changed and the cached manager must be replaced.
type ManagerBuilder func(ctx context.Context, userID uuid.UUID) (*AgentManager, string, error)

// AgentRegistry caches AgentManager instances per user. The zero value
// is not usable; construct via NewAgentRegistry.
type AgentRegistry struct {
	builder ManagerBuilder

	mu       sync.Mutex
	cache    map[uuid.UUID]registryEntry
	building map[uuid.UUID]*buildCall
}

type registryEntry struct {
	manager *AgentManager
	sig     string
}

// buildCall coalesces concurrent For() calls for the same userID so
// the builder is invoked at most once per active request burst.
type buildCall struct {
	done chan struct{}
	mgr  *AgentManager
	err  error
}

// NewAgentRegistry returns a registry that delegates manager
// construction to builder. Builder must be non-nil.
func NewAgentRegistry(builder ManagerBuilder) *AgentRegistry {
	if builder == nil {
		panic("board: NewAgentRegistry requires a non-nil builder")
	}
	return &AgentRegistry{
		builder:  builder,
		cache:    make(map[uuid.UUID]registryEntry),
		building: make(map[uuid.UUID]*buildCall),
	}
}

// For returns the cached manager for userID, building a new one if
// absent or if the settings signature has changed. Concurrent callers
// for the same userID share a single builder invocation. Cancelling
// ctx aborts the wait but does not cancel the in-flight builder.
func (r *AgentRegistry) For(ctx context.Context, userID uuid.UUID) (*AgentManager, error) {
	if r == nil {
		return nil, errors.New("agent registry is nil")
	}

	r.mu.Lock()
	if call, ok := r.building[userID]; ok {
		r.mu.Unlock()
		select {
		case <-call.done:
			return call.mgr, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &buildCall{done: make(chan struct{})}
	r.building[userID] = call
	cached, hadCached := r.cache[userID]
	r.mu.Unlock()

	mgr, sig, err := r.builder(ctx, userID)

	r.mu.Lock()
	delete(r.building, userID)
	if err != nil {
		call.err = err
		close(call.done)
		r.mu.Unlock()
		return nil, err
	}

	var stale *AgentManager
	final := mgr
	if hadCached && cached.sig == sig {
		final = cached.manager
	} else {
		if hadCached {
			stale = cached.manager
		}
		r.cache[userID] = registryEntry{manager: mgr, sig: sig}
	}
	call.mgr = final
	close(call.done)
	r.mu.Unlock()

	if stale != nil {
		stale.StopAll()
	}
	return final, nil
}

// Forget removes the cached manager for userID and cancels any
// in-flight runs it owned. Safe to call when no entry is cached.
// Used after settings updates, on logout, and from tests.
func (r *AgentRegistry) Forget(userID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	entry, ok := r.cache[userID]
	if ok {
		delete(r.cache, userID)
	}
	r.mu.Unlock()
	if ok && entry.manager != nil {
		entry.manager.StopAll()
	}
}

// Snapshot returns the user IDs currently cached. Order is undefined.
// Intended for debugging and tests; do not use to drive production logic.
func (r *AgentRegistry) Snapshot() []uuid.UUID {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, 0, len(r.cache))
	for id := range r.cache {
		out = append(out, id)
	}
	return out
}
