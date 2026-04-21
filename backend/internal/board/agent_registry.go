// Package board's AgentRegistry caches *AgentManager instances per
// (user, repo) across HTTP requests.
//
// Why this exists: StartAgent / StopAgent / SubmitFeedback need to reach
// the SAME manager instance from successive HTTP requests because the
// manager owns the runtime state for in-flight agent goroutines (cancel
// funcs, feedback queues, the runs map). Earlier code constructed a
// fresh manager per request, which made StopAgent and SubmitFeedback
// no-ops — they targeted a freshly built map with zero entries.
//
// Cache key: (userID, repoID, settings signature). Multi-repo support
// means the same user can have N concurrent boards, each with its own
// agent manager. The repo dimension is part of the cache key so a
// stop-on-board-A doesn't accidentally cancel a run on board-B.
//
// The builder is called on every For() and returns both a manager and
// a signature. When the signature matches the cached entry, we discard
// the freshly built manager and return the cached one so in-flight
// runs remain reachable. When the signature differs (e.g. the user
// changed their codegen agent), we cancel any in-flight runs on the
// stale manager and replace the cache entry. Concurrent For() calls
// for the same (user, repo) are coalesced via a singleflight so the
// builder runs at most once.
package board

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/google/uuid"
)

// ManagerBuilder constructs a *AgentManager for (userID, repoID) and
// returns a stable signature string derived from the inputs that
// affect manager construction. The registry uses the signature to
// detect when stored settings have changed and the cached manager
// must be replaced.
type ManagerBuilder func(ctx context.Context, userID, repoID uuid.UUID) (*AgentManager, string, error)

// AgentRegistry caches AgentManager instances per (user, repo). The
// zero value is not usable; construct via NewAgentRegistry.
type AgentRegistry struct {
	builder ManagerBuilder

	mu       sync.Mutex
	cache    map[cacheKey]registryEntry
	building map[cacheKey]*buildCall
}

// cacheKey is the (user, repo) pair the registry indexes managers on.
// Both fields are required; the registry has no concept of a
// "default" — that resolution happens upstream in the HTTP handler.
type cacheKey struct {
	UserID uuid.UUID
	RepoID uuid.UUID
}

type registryEntry struct {
	manager *AgentManager
	sig     string
}

// buildCall coalesces concurrent For() calls for the same key so the
// builder is invoked at most once per active request burst.
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
		cache:    make(map[cacheKey]registryEntry),
		building: make(map[cacheKey]*buildCall),
	}
}

// For returns the cached manager for (userID, repoID), building a new
// one if absent or if the settings signature has changed. Concurrent
// callers for the same key share a single builder invocation.
// Cancelling ctx aborts the wait but does not cancel the in-flight
// builder.
func (r *AgentRegistry) For(ctx context.Context, userID, repoID uuid.UUID) (*AgentManager, error) {
	if r == nil {
		return nil, errors.New("agent registry is nil")
	}
	key := cacheKey{UserID: userID, RepoID: repoID}

	r.mu.Lock()
	if call, ok := r.building[key]; ok {
		r.mu.Unlock()
		select {
		case <-call.done:
			return call.mgr, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &buildCall{done: make(chan struct{})}
	r.building[key] = call
	cached, hadCached := r.cache[key]
	r.mu.Unlock()

	mgr, sig, err := r.builder(ctx, userID, repoID)

	r.mu.Lock()
	delete(r.building, key)
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
		r.cache[key] = registryEntry{manager: mgr, sig: sig}
	}
	call.mgr = final
	close(call.done)
	r.mu.Unlock()

	if stale != nil {
		stale.StopAll()
	}
	return final, nil
}

// Forget removes every cached manager for userID and cancels any
// in-flight runs they own. Used after settings/token updates that
// affect every board the user has connected. Use ForgetRepo when the
// change only affects a single repo.
func (r *AgentRegistry) Forget(userID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	stale := make([]*AgentManager, 0)
	for k, entry := range r.cache {
		if k.UserID != userID {
			continue
		}
		stale = append(stale, entry.manager)
		delete(r.cache, k)
	}
	r.mu.Unlock()
	log.Printf("board.AgentRegistry: Forget user=%s evicted=%d", userID, len(stale))
	for _, mgr := range stale {
		if mgr != nil {
			mgr.StopAll()
		}
	}
}

// ForgetRepo removes the cached manager for one (user, repo) pair
// only. Use when a per-repo change (e.g. removing or reconnecting a
// repo) shouldn't disrupt the user's other boards.
func (r *AgentRegistry) ForgetRepo(userID, repoID uuid.UUID) {
	if r == nil {
		return
	}
	key := cacheKey{UserID: userID, RepoID: repoID}
	r.mu.Lock()
	entry, ok := r.cache[key]
	if ok {
		delete(r.cache, key)
	}
	r.mu.Unlock()
	log.Printf("board.AgentRegistry: ForgetRepo user=%s repo=%s evicted=%t", userID, repoID, ok)
	if ok && entry.manager != nil {
		entry.manager.StopAll()
	}
}

// Snapshot returns the (userID, repoID) pairs currently cached.
// Order is undefined. Intended for debugging and tests; do not use
// to drive production logic.
func (r *AgentRegistry) Snapshot() []cacheKey {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]cacheKey, 0, len(r.cache))
	for k := range r.cache {
		out = append(out, k)
	}
	return out
}
