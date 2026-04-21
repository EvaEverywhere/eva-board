package board

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping board integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return New(pool), pool
}

// makeRepo seeds a board_repos row for the user and returns its ID.
// Cards in the integration tests are scoped to a repo via the repo_id
// FK; without this helper the tests would fail INSERT on the new
// NOT-NULL-by-convention column.
func makeRepo(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var idStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO board_repos (user_id, owner, name, repo_path, is_default)
		VALUES ($1, $2, $3, $4, true)
		RETURNING id::text
	`, userID, "owner-"+uuid.NewString()[:8], "repo-"+uuid.NewString()[:8], "/tmp/repo").Scan(&idStr); err != nil {
		t.Fatalf("insert board repo: %v", err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		t.Fatalf("parse repo id: %v", err)
	}
	return id
}

func makeUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	identity := "test-" + uuid.NewString()
	email := identity + "@example.com"
	var idStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (identity_key, email, name)
		VALUES ($1, $2, 'Tester')
		RETURNING id::text
	`, identity, email).Scan(&idStr); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func TestCreateAndGet(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	card, err := svc.Create(ctx, user, repo, CreateRequest{Title: "First card", Description: "hello"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if card.Title != "First card" || card.Description != "hello" {
		t.Fatalf("unexpected card content: %+v", card)
	}
	if card.Column != ColumnBacklog {
		t.Fatalf("expected backlog column, got %q", card.Column)
	}
	if card.Position != 0 {
		t.Fatalf("expected first card at position 0, got %d", card.Position)
	}
	if card.AgentStatus != AgentStatusIdle {
		t.Fatalf("expected idle status, got %q", card.AgentStatus)
	}

	got, err := svc.Get(ctx, user, card.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != card.ID || got.Title != card.Title {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, card)
	}

	if _, err := svc.Create(ctx, user, repo, CreateRequest{Title: "  "}); err != ErrTitleRequired {
		t.Fatalf("expected ErrTitleRequired, got %v", err)
	}
}

func TestListByColumn(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	a, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "a"})
	b, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "b"})
	c, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "c"})
	if _, err := svc.Move(ctx, user, c.ID, ColumnDevelop, 0); err != nil {
		t.Fatalf("move c: %v", err)
	}

	backlog, err := svc.List(ctx, user, repo, ColumnBacklog)
	if err != nil {
		t.Fatalf("list backlog: %v", err)
	}
	if len(backlog) != 2 {
		t.Fatalf("expected 2 backlog cards, got %d", len(backlog))
	}
	if backlog[0].ID != a.ID || backlog[1].ID != b.ID {
		t.Fatalf("backlog order wrong: %+v", backlog)
	}

	develop, err := svc.List(ctx, user, repo, ColumnDevelop)
	if err != nil {
		t.Fatalf("list develop: %v", err)
	}
	if len(develop) != 1 || develop[0].ID != c.ID {
		t.Fatalf("develop wrong: %+v", develop)
	}

	all, err := svc.List(ctx, user, repo, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 total cards, got %d", len(all))
	}

	if _, err := svc.List(ctx, user, repo, "bogus"); err != ErrInvalidColumn {
		t.Fatalf("expected ErrInvalidColumn, got %v", err)
	}
}

func TestMoveWithinColumn(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	a, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "a"})
	b, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "b"})
	c, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "c"})

	if _, err := svc.Move(ctx, user, c.ID, ColumnBacklog, 0); err != nil {
		t.Fatalf("move c to top: %v", err)
	}

	cards, err := svc.List(ctx, user, repo, ColumnBacklog)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []uuid.UUID{c.ID, a.ID, b.ID}
	for i, id := range want {
		if cards[i].ID != id {
			t.Fatalf("position %d: want %s, got %s", i, id, cards[i].ID)
		}
		if cards[i].Position != i {
			t.Fatalf("position field at %d: want %d, got %d", i, i, cards[i].Position)
		}
	}
}

func TestMoveBetweenColumns(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	a, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "a"})
	b, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "b"})
	c, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "c"})

	if _, err := svc.Move(ctx, user, b.ID, ColumnReview, 0); err != nil {
		t.Fatalf("move b: %v", err)
	}

	backlog, _ := svc.List(ctx, user, repo, ColumnBacklog)
	if len(backlog) != 2 || backlog[0].ID != a.ID || backlog[1].ID != c.ID {
		t.Fatalf("backlog after move wrong: %+v", backlog)
	}
	if backlog[1].Position != 1 {
		t.Fatalf("expected c reindexed to 1, got %d", backlog[1].Position)
	}

	review, _ := svc.List(ctx, user, repo, ColumnReview)
	if len(review) != 1 || review[0].ID != b.ID || review[0].Position != 0 {
		t.Fatalf("review wrong: %+v", review)
	}
}

func TestUpdate(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	card, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "old"})
	newTitle := "new"
	newDesc := "desc"
	updated, err := svc.Update(ctx, user, card.ID, UpdateRequest{
		Title:       &newTitle,
		Description: &newDesc,
		Metadata:    map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != newTitle || updated.Description != newDesc {
		t.Fatalf("update did not persist: %+v", updated)
	}
	if got, _ := updated.Metadata["k"].(string); got != "v" {
		t.Fatalf("metadata not stored: %+v", updated.Metadata)
	}
}

func TestDelete(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	a, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "a"})
	b, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "b"})
	c, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "c"})

	if err := svc.Delete(ctx, user, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, user, b.ID); err != ErrCardNotFound {
		t.Fatalf("expected ErrCardNotFound after delete, got %v", err)
	}

	cards, _ := svc.List(ctx, user, repo, ColumnBacklog)
	if len(cards) != 2 || cards[0].ID != a.ID || cards[1].ID != c.ID {
		t.Fatalf("after delete order wrong: %+v", cards)
	}
	if cards[1].Position != 1 {
		t.Fatalf("expected c reindexed to 1, got %d", cards[1].Position)
	}

	if err := svc.Delete(ctx, user, b.ID); err != ErrCardNotFound {
		t.Fatalf("expected ErrCardNotFound on second delete, got %v", err)
	}
}

func TestSetAgentStatus(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	card, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "a"})
	for _, status := range []string{
		AgentStatusRunning, AgentStatusVerifying, AgentStatusReviewing,
		AgentStatusFailed, AgentStatusSucceeded, AgentStatusIdle,
	} {
		if err := svc.SetAgentStatus(ctx, card.ID, status); err != nil {
			t.Fatalf("set %s: %v", status, err)
		}
		got, _ := svc.Get(ctx, user, card.ID)
		if got.AgentStatus != status {
			t.Fatalf("after set %s, got %s", status, got.AgentStatus)
		}
	}
	if err := svc.SetAgentStatus(ctx, card.ID, "garbage"); err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestUserIsolation(t *testing.T) {
	svc, pool := newTestService(t)
	userA := makeUser(t, pool)
	userB := makeUser(t, pool)
	repoA := makeRepo(t, pool, userA)
	repoB := makeRepo(t, pool, userB)
	ctx := context.Background()

	cardA, _ := svc.Create(ctx, userA, repoA, CreateRequest{Title: "A's card"})

	if _, err := svc.Get(ctx, userB, cardA.ID); err != ErrCardNotFound {
		t.Fatalf("user B should not access A's card, got %v", err)
	}
	listB, err := svc.List(ctx, userB, repoB, "")
	if err != nil {
		t.Fatalf("list for B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("user B should see no cards, got %d", len(listB))
	}
	if err := svc.Delete(ctx, userB, cardA.ID); err != ErrCardNotFound {
		t.Fatalf("user B delete should fail, got %v", err)
	}
	if _, err := svc.Move(ctx, userB, cardA.ID, ColumnDevelop, 0); err != ErrCardNotFound {
		t.Fatalf("user B move should fail, got %v", err)
	}
}

func TestSetPRAndReviewAndMetadata(t *testing.T) {
	svc, pool := newTestService(t)
	user := makeUser(t, pool)
	repo := makeRepo(t, pool, user)
	ctx := context.Background()

	card, _ := svc.Create(ctx, user, repo, CreateRequest{Title: "x"})
	if err := svc.SetWorktreeBranch(ctx, card.ID, "feat/x"); err != nil {
		t.Fatalf("set branch: %v", err)
	}
	if err := svc.SetPR(ctx, card.ID, 42, "https://example.com/pr/42"); err != nil {
		t.Fatalf("set pr: %v", err)
	}
	if err := svc.SetReviewStatus(ctx, card.ID, "approved"); err != nil {
		t.Fatalf("set review: %v", err)
	}
	if err := svc.SetMetadata(ctx, card.ID, "ci", "passing"); err != nil {
		t.Fatalf("set metadata: %v", err)
	}

	got, _ := svc.Get(ctx, user, card.ID)
	if got.WorktreeBranch == nil || *got.WorktreeBranch != "feat/x" {
		t.Fatalf("branch not set: %+v", got.WorktreeBranch)
	}
	if got.PRNumber == nil || *got.PRNumber != 42 {
		t.Fatalf("pr number not set: %+v", got.PRNumber)
	}
	if got.PRURL == nil || *got.PRURL != "https://example.com/pr/42" {
		t.Fatalf("pr url not set: %+v", got.PRURL)
	}
	if got.ReviewStatus == nil || *got.ReviewStatus != "approved" {
		t.Fatalf("review status not set: %+v", got.ReviewStatus)
	}
	if v, _ := got.Metadata["ci"].(string); v != "passing" {
		t.Fatalf("metadata not merged: %+v", got.Metadata)
	}
}
