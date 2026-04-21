package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// ReposService is backed by Postgres so the tests are integration-style;
// they piggyback on the same TEST_DATABASE_URL guard the cards service
// tests use. Skipping in CI environments without a live database keeps
// the unit-test loop fast.

func TestReposService_AddFirstRepoBecomesDefault(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	r, err := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "widgets", RepoPath: "/tmp/widgets"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !r.IsDefault {
		t.Fatalf("first repo must be marked default, got is_default=false")
	}
	if r.DefaultBranch != "main" {
		t.Fatalf("default branch fallback wrong: %q", r.DefaultBranch)
	}

	got, err := repos.GetDefault(ctx, user)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if got.ID != r.ID {
		t.Fatalf("default mismatch: %s vs %s", got.ID, r.ID)
	}
	_ = svc
}

func TestReposService_OnlyOneDefaultPerUser(t *testing.T) {
	_, pool := newTestService(t)
	user := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	a, err := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "first", RepoPath: "/tmp/a"})
	if err != nil {
		t.Fatalf("Add a: %v", err)
	}
	b, err := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "second", RepoPath: "/tmp/b", SetDefault: true})
	if err != nil {
		t.Fatalf("Add b: %v", err)
	}

	list, err := repos.List(ctx, user)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	defaults := 0
	for _, r := range list {
		if r.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default, got %d", defaults)
	}
	got, _ := repos.GetDefault(ctx, user)
	if got.ID != b.ID {
		t.Fatalf("expected b to be default after SetDefault=true, got %s (a=%s)", got.ID, a.ID)
	}

	if err := repos.SetDefault(ctx, user, a.ID); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	got, _ = repos.GetDefault(ctx, user)
	if got.ID != a.ID {
		t.Fatalf("SetDefault did not switch default; got %s want %s", got.ID, a.ID)
	}
}

func TestReposService_AddDuplicateConflicts(t *testing.T) {
	_, pool := newTestService(t)
	user := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	if _, err := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "dup", RepoPath: "/tmp/x"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "dup", RepoPath: "/tmp/y"})
	if !errors.Is(err, ErrRepoConflict) {
		t.Fatalf("expected ErrRepoConflict on duplicate (owner,name); got %v", err)
	}
}

func TestReposService_RemoveDoesNotAutoPromoteDefault(t *testing.T) {
	_, pool := newTestService(t)
	user := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	a, _ := repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "a", RepoPath: "/tmp/a"})
	_, _ = repos.Add(ctx, user, AddRepoRequest{Owner: "acme", Name: "b", RepoPath: "/tmp/b"})

	// Removing the default leaves the user without a default. The UI
	// is expected to prompt the user to pick one explicitly — auto
	// promoting would silently change which repo new cards land in.
	if err := repos.Remove(ctx, user, a.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := repos.GetDefault(ctx, user); !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("expected no default after removing it, got err=%v", err)
	}
}

func TestReposService_RemoveScopedToUser(t *testing.T) {
	_, pool := newTestService(t)
	userA := makeUser(t, pool)
	userB := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	a, _ := repos.Add(ctx, userA, AddRepoRequest{Owner: "acme", Name: "x", RepoPath: "/tmp/x"})

	if err := repos.Remove(ctx, userB, a.ID); !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("user B must not be able to delete A's repo, got %v", err)
	}
}

func TestReposService_AddValidatesRequiredFields(t *testing.T) {
	_, pool := newTestService(t)
	user := makeUser(t, pool)
	repos := NewReposService(pool)
	ctx := context.Background()

	if _, err := repos.Add(ctx, user, AddRepoRequest{Name: "x", RepoPath: "/tmp"}); !errors.Is(err, ErrRepoOwnerRequired) {
		t.Fatalf("missing owner: %v", err)
	}
	if _, err := repos.Add(ctx, user, AddRepoRequest{Owner: "x", RepoPath: "/tmp"}); !errors.Is(err, ErrRepoNameRequired) {
		t.Fatalf("missing name: %v", err)
	}
	if _, err := repos.Add(ctx, user, AddRepoRequest{Owner: "x", Name: "y"}); !errors.Is(err, ErrRepoPathRequired) {
		t.Fatalf("missing path: %v", err)
	}
	_ = uuid.Nil
}
