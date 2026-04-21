package board

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeBuilder is a configurable ManagerBuilder for the registry tests.
// It tracks how many times it was invoked and returns a per-(userID,
// build#) AgentManager so tests can identify whether the registry
// returned a cached vs newly-built instance.
type fakeBuilder struct {
	mu        sync.Mutex
	calls     int64
	signature func(uuid.UUID, int) string
	err       error
	delay     time.Duration
}

func (f *fakeBuilder) build(_ context.Context, userID, repoID uuid.UUID) (*AgentManager, string, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	atomic.AddInt64(&f.calls, 1)
	f.mu.Lock()
	n := int(f.calls)
	f.mu.Unlock()
	if f.err != nil {
		return nil, "", f.err
	}
	sig := "sig"
	if f.signature != nil {
		sig = f.signature(userID, n)
	}
	mgr := NewAgentManager(nil, nil, nil, AgentConfig{RepoPath: userID.String() + ":" + repoID.String()})
	return mgr, sig, nil
}

func (f *fakeBuilder) count() int64 { return atomic.LoadInt64(&f.calls) }

func TestAgentRegistry_CachesPerUser(t *testing.T) {
	fb := &fakeBuilder{}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	rid := uuid.New()
	ctx := context.Background()

	first, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("first For: %v", err)
	}
	second, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("second For: %v", err)
	}
	if first != second {
		t.Fatalf("expected cached manager to be reused; first=%p second=%p", first, second)
	}
	// Builder is called every time (so the registry can detect signature
	// changes), but the cached manager wins when sigs match.
	if got := fb.count(); got != 2 {
		t.Fatalf("builder calls = %d, want 2", got)
	}
}

func TestAgentRegistry_RebuildsOnSignatureChange(t *testing.T) {
	fb := &fakeBuilder{
		signature: func(_ uuid.UUID, n int) string {
			if n == 1 {
				return "old"
			}
			return "new"
		},
	}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	rid := uuid.New()
	ctx := context.Background()

	first, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("first For: %v", err)
	}
	second, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("second For: %v", err)
	}
	if first == second {
		t.Fatalf("expected new manager when signature changes; got same instance %p", first)
	}
}

func TestAgentRegistry_DifferentUsers(t *testing.T) {
	fb := &fakeBuilder{
		signature: func(uid uuid.UUID, _ int) string { return uid.String() },
	}
	r := NewAgentRegistry(fb.build)
	ctx := context.Background()

	a, err := r.For(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("user a: %v", err)
	}
	b, err := r.For(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("user b: %v", err)
	}
	if a == b {
		t.Fatalf("different users must get different managers; got same %p", a)
	}
	ids := r.Snapshot()
	if len(ids) != 2 {
		t.Fatalf("snapshot size = %d, want 2", len(ids))
	}
}

func TestAgentRegistry_Forget(t *testing.T) {
	fb := &fakeBuilder{}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	rid := uuid.New()
	ctx := context.Background()

	first, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("first For: %v", err)
	}
	r.Forget(uid)
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot after forget = %v, want empty", got)
	}
	second, err := r.For(ctx, uid, rid)
	if err != nil {
		t.Fatalf("second For: %v", err)
	}
	if first == second {
		t.Fatalf("expected fresh manager after Forget; got cached %p", first)
	}
}

// TestAgentRegistry_DifferentReposSameUser verifies the cache key
// includes repoID so two boards owned by the same user get distinct
// AgentManagers and a Stop on board A does not affect board B.
func TestAgentRegistry_DifferentReposSameUser(t *testing.T) {
	fb := &fakeBuilder{
		signature: func(_ uuid.UUID, n int) string {
			// Distinct sigs per call → registry treats them as
			// different even if it didn't key on repoID; we want
			// to also assert the cache holds both entries.
			return "sig" + string(rune('0'+n))
		},
	}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	repoA := uuid.New()
	repoB := uuid.New()
	ctx := context.Background()

	a, err := r.For(ctx, uid, repoA)
	if err != nil {
		t.Fatalf("repo A: %v", err)
	}
	b, err := r.For(ctx, uid, repoB)
	if err != nil {
		t.Fatalf("repo B: %v", err)
	}
	if a == b {
		t.Fatalf("same user different repos must get distinct managers")
	}
	if got := r.Snapshot(); len(got) != 2 {
		t.Fatalf("snapshot size = %d, want 2 (one per repo)", len(got))
	}
}

// TestAgentRegistry_ForgetRepo evicts only one (user, repo) entry.
func TestAgentRegistry_ForgetRepo(t *testing.T) {
	fb := &fakeBuilder{}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	repoA := uuid.New()
	repoB := uuid.New()
	ctx := context.Background()

	if _, err := r.For(ctx, uid, repoA); err != nil {
		t.Fatalf("repo A: %v", err)
	}
	if _, err := r.For(ctx, uid, repoB); err != nil {
		t.Fatalf("repo B: %v", err)
	}
	r.ForgetRepo(uid, repoA)
	got := r.Snapshot()
	if len(got) != 1 {
		t.Fatalf("snapshot after ForgetRepo size = %d, want 1", len(got))
	}
	if got[0].RepoID != repoB {
		t.Fatalf("expected repo B to remain, got %v", got[0])
	}
}

func TestAgentRegistry_Concurrent(t *testing.T) {
	// Slow builder + many goroutines: the per-user singleflight must
	// coalesce them so the builder runs once and all goroutines see the
	// same returned manager.
	fb := &fakeBuilder{delay: 50 * time.Millisecond}
	r := NewAgentRegistry(fb.build)
	uid := uuid.New()
	rid := uuid.New()
	ctx := context.Background()

	const N = 16
	var wg sync.WaitGroup
	results := make([]*AgentManager, N)
	errs := make([]error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = r.For(ctx, uid, rid)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d: %v", i, e)
		}
	}
	for i := 1; i < N; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d got different manager (%p) from goroutine 0 (%p)", i, results[i], results[0])
		}
	}
	if got := fb.count(); got != 1 {
		t.Fatalf("builder calls = %d, want 1 (singleflight)", got)
	}
}

func TestAgentRegistry_BuilderError(t *testing.T) {
	want := errors.New("boom")
	fb := &fakeBuilder{err: want}
	r := NewAgentRegistry(fb.build)
	if _, err := r.For(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
