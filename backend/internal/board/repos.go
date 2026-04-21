// Package board's ReposService persists the per-user catalog of GitHub
// repositories connected to the board. Multi-repo support means each
// user can connect N repos and treat each as a separate board; cards
// are scoped to a repo via board_cards.repo_id.
//
// Invariants enforced here:
//   - (user_id, owner, name) is unique — we surface ErrRepoConflict.
//   - At most one row per user has is_default = true (DB partial unique
//     index backs this; the service updates default flags inside a
//     transaction).
//   - Removing the currently-default repo intentionally does NOT
//     auto-promote another repo to default. The user picks the next
//     default explicitly. This keeps the behaviour predictable for the
//     UI and avoids surprising the agent loop with a different repo on
//     the next request.
package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is a single GitHub repository connected to a user's board.
type Repo struct {
	ID            uuid.UUID `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	RepoPath      string    `json:"repo_path"`
	DefaultBranch string    `json:"default_branch"`
	IsDefault     bool      `json:"is_default"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AddRepoRequest carries the inputs for connecting a new repo.
type AddRepoRequest struct {
	Owner         string
	Name          string
	RepoPath      string
	DefaultBranch string
	SetDefault    bool
}

var (
	// ErrRepoNotFound is returned when no row matches the lookup. The
	// HTTP layer maps this to 404.
	ErrRepoNotFound = errors.New("board repo not found")
	// ErrRepoConflict is returned when (user_id, owner, name) is
	// already connected. The HTTP layer maps this to 409.
	ErrRepoConflict = errors.New("board repo already connected for user")
	// ErrRepoOwnerRequired / ErrRepoNameRequired / ErrRepoPathRequired
	// guard the required fields on Add. The handler surfaces them as
	// 400s.
	ErrRepoOwnerRequired = errors.New("repo owner is required")
	ErrRepoNameRequired  = errors.New("repo name is required")
	ErrRepoPathRequired  = errors.New("repo path is required")
)

// ReposService persists per-user board repos in board_repos.
type ReposService struct {
	db *pgxpool.Pool
}

// NewReposService returns a ReposService backed by the given pool.
func NewReposService(db *pgxpool.Pool) *ReposService {
	return &ReposService{db: db}
}

const repoSelect = `
	id::text, user_id::text, owner, name, repo_path,
	default_branch, is_default, created_at, updated_at
`

// List returns every repo connected to userID, default first, then
// alphabetical owner/name. The board UI's repo switcher uses this
// ordering directly.
func (s *ReposService) List(ctx context.Context, userID uuid.UUID) ([]Repo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+repoSelect+`
		FROM board_repos
		WHERE user_id = $1
		ORDER BY is_default DESC, owner, name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list board repos: %w", err)
	}
	defer rows.Close()
	out := []Repo{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns the repo with repoID, scoped to userID. Cross-user
// access returns ErrRepoNotFound (we deliberately do not distinguish
// "wrong user" from "not exists" to avoid leaking existence).
func (s *ReposService) Get(ctx context.Context, userID, repoID uuid.UUID) (*Repo, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+repoSelect+`
		FROM board_repos
		WHERE id = $1 AND user_id = $2
	`, repoID, userID)
	r, err := scanRepo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRepoNotFound
	}
	return r, err
}

// GetByOwnerName resolves a repo by its (owner, name) tuple. Used by
// flows that still hold a reference via the legacy
// board_settings.github_owner/github_repo fields.
func (s *ReposService) GetByOwnerName(ctx context.Context, userID uuid.UUID, owner, name string) (*Repo, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+repoSelect+`
		FROM board_repos
		WHERE user_id = $1 AND owner = $2 AND name = $3
	`, userID, owner, name)
	r, err := scanRepo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRepoNotFound
	}
	return r, err
}

// GetDefault returns the user's default repo, or ErrRepoNotFound if
// the user has no repos (or no default among them).
func (s *ReposService) GetDefault(ctx context.Context, userID uuid.UUID) (*Repo, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+repoSelect+`
		FROM board_repos
		WHERE user_id = $1 AND is_default = true
	`, userID)
	r, err := scanRepo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRepoNotFound
	}
	return r, err
}

// Add connects a new repo for the user. If req.SetDefault is true, or
// this is the user's first repo, the new row is marked default and any
// prior default is cleared inside the same transaction.
//
// Owner, Name, and RepoPath are required (trimmed). DefaultBranch
// defaults to "main" when empty.
func (s *ReposService) Add(ctx context.Context, userID uuid.UUID, req AddRepoRequest) (*Repo, error) {
	owner := strings.TrimSpace(req.Owner)
	name := strings.TrimSpace(req.Name)
	repoPath := strings.TrimSpace(req.RepoPath)
	branch := strings.TrimSpace(req.DefaultBranch)
	if owner == "" {
		return nil, ErrRepoOwnerRequired
	}
	if name == "" {
		return nil, ErrRepoNameRequired
	}
	if repoPath == "" {
		return nil, ErrRepoPathRequired
	}
	if branch == "" {
		branch = "main"
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin add repo tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Decide whether this row should become the user's default. It
	// MUST be default when it is the first repo or when SetDefault was
	// requested, otherwise the user could end up with no default at
	// all and the cards endpoints would 400.
	var existingCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM board_repos WHERE user_id = $1`, userID).Scan(&existingCount); err != nil {
		return nil, fmt.Errorf("count repos: %w", err)
	}
	makeDefault := req.SetDefault || existingCount == 0
	if makeDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE board_repos SET is_default = false, updated_at = now()
			WHERE user_id = $1 AND is_default = true
		`, userID); err != nil {
			return nil, fmt.Errorf("clear prior default: %w", err)
		}
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO board_repos (user_id, owner, name, repo_path, default_branch, is_default)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+repoSelect, userID, owner, name, repoPath, branch, makeDefault)
	r, err := scanRepo(row)
	if err != nil {
		// pgx surfaces unique-constraint violations as plain errors
		// containing "23505". Detect via string match — the error
		// type comes from pgconn and adding that import to keep the
		// service tightly typed isn't worth the dep here.
		if isUniqueViolation(err) {
			return nil, ErrRepoConflict
		}
		return nil, fmt.Errorf("insert board repo: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit add repo: %w", err)
	}
	return r, nil
}

// Remove deletes the repo. Cascading deletes on board_cards.repo_id
// drop the cards too — the user explicitly asked to disconnect, so
// silently keeping orphaned cards would be confusing. We deliberately
// do NOT auto-promote another repo to default if this row was the
// default; the user picks the next default themselves.
func (s *ReposService) Remove(ctx context.Context, userID, repoID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM board_repos WHERE id = $1 AND user_id = $2`, repoID, userID)
	if err != nil {
		return fmt.Errorf("delete board repo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRepoNotFound
	}
	return nil
}

// SetDefault makes repoID the user's default. Runs in a transaction
// so the partial unique index "one default per user" never sees an
// intermediate "two defaults" state.
func (s *ReposService) SetDefault(ctx context.Context, userID, repoID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin set default: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_repos WHERE id = $1 AND user_id = $2)`, repoID, userID).Scan(&exists); err != nil {
		return fmt.Errorf("check repo exists: %w", err)
	}
	if !exists {
		return ErrRepoNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE board_repos SET is_default = false, updated_at = now()
		WHERE user_id = $1 AND is_default = true AND id <> $2
	`, userID, repoID); err != nil {
		return fmt.Errorf("clear prior default: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE board_repos SET is_default = true, updated_at = now()
		WHERE id = $1 AND user_id = $2
	`, repoID, userID); err != nil {
		return fmt.Errorf("set new default: %w", err)
	}
	return tx.Commit(ctx)
}

func scanRepo(r rowScanner) (*Repo, error) {
	var (
		repo      Repo
		idStr     string
		userIDStr string
	)
	if err := r.Scan(
		&idStr,
		&userIDStr,
		&repo.Owner,
		&repo.Name,
		&repo.RepoPath,
		&repo.DefaultBranch,
		&repo.IsDefault,
		&repo.CreatedAt,
		&repo.UpdatedAt,
	); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse repo id: %w", err)
	}
	uid, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	repo.ID = id
	repo.UserID = uid
	return &repo, nil
}

// isUniqueViolation does a substring check on the error message
// because the pgx PgError type lives in pgconn and we want to keep
// this service free of that import. The "23505" SQLSTATE is stable.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505")
}
