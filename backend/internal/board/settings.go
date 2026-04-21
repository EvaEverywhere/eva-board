package board

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/security"
)

// Settings is the per-user board configuration returned by the API. The
// raw token is intentionally never serialized; callers should rely on
// HasGitHubToken to know whether one is stored.
type Settings struct {
	UserID              uuid.UUID `json:"user_id"`
	GitHubOwner         string    `json:"github_owner"`
	GitHubRepo          string    `json:"github_repo"`
	RepoPath            string    `json:"repo_path"`
	CodegenAgent        string    `json:"codegen_agent"`
	MaxVerifyIterations int       `json:"max_verify_iterations"`
	MaxReviewCycles     int       `json:"max_review_cycles"`
	HasGitHubToken      bool      `json:"has_github_token"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// UpsertRequest carries optional updates to a user's settings. nil pointer
// = leave field unchanged. Empty string clears the GitHub token.
type UpsertRequest struct {
	GitHubToken         *string
	GitHubOwner         *string
	GitHubRepo          *string
	RepoPath            *string
	CodegenAgent        *string
	MaxVerifyIterations *int
	MaxReviewCycles     *int
}

// Default values used both for fresh rows and for client display when no
// row exists yet for a user.
const (
	DefaultCodegenAgent        = "claude-code"
	DefaultMaxVerifyIterations = 3
	DefaultMaxReviewCycles     = 5
)

var (
	ErrCipherNotConfigured = errors.New("token cipher is not configured")
	ErrGitHubNotConfigured = errors.New("github owner and repo are required")
	ErrNoGitHubToken       = errors.New("no github token stored for user")
)

// SettingsService persists per-user board settings, encrypts the GitHub
// PAT at rest, and validates new tokens against GitHub before storing.
type SettingsService struct {
	db        *pgxpool.Pool
	cipher    *security.TokenCipher
	ghFactory github.ClientFactory
}

// NewSettingsService constructs a SettingsService. cipher may be nil in
// dev environments without TOKEN_ENCRYPTION_KEY; in that case any attempt
// to set or read a token returns ErrCipherNotConfigured. ghFactory is
// required for token validation and the repo-list endpoint.
func NewSettingsService(db *pgxpool.Pool, cipher *security.TokenCipher, ghFactory github.ClientFactory) *SettingsService {
	return &SettingsService{db: db, cipher: cipher, ghFactory: ghFactory}
}

const settingsSelect = `
	user_id::text,
	COALESCE(github_token_encrypted, ''),
	COALESCE(github_owner, ''),
	COALESCE(github_repo, ''),
	COALESCE(repo_path, ''),
	codegen_agent,
	max_verify_iterations,
	max_review_cycles,
	updated_at
`

// Get returns the user's settings (without the raw token). If no row
// exists, defaults are returned with HasGitHubToken=false.
func (s *SettingsService) Get(ctx context.Context, userID uuid.UUID) (Settings, error) {
	row := s.db.QueryRow(ctx, `SELECT `+settingsSelect+` FROM board_settings WHERE user_id = $1`, userID)
	st, err := scanSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{
			UserID:              userID,
			CodegenAgent:        DefaultCodegenAgent,
			MaxVerifyIterations: DefaultMaxVerifyIterations,
			MaxReviewCycles:     DefaultMaxReviewCycles,
		}, nil
	}
	return st, err
}

// Upsert merges req into the user's settings row. When req.GitHubToken is
// non-nil and non-empty, the token is validated against GitHub /user
// before being encrypted and stored. An empty string clears any stored
// token.
func (s *SettingsService) Upsert(ctx context.Context, userID uuid.UUID, req UpsertRequest) (Settings, error) {
	if req.MaxVerifyIterations != nil && *req.MaxVerifyIterations < 1 {
		return Settings{}, apperrors.New(http.StatusBadRequest, "max_verify_iterations must be >= 1")
	}
	if req.MaxReviewCycles != nil && *req.MaxReviewCycles < 1 {
		return Settings{}, apperrors.New(http.StatusBadRequest, "max_review_cycles must be >= 1")
	}

	tokenAction := tokenUnchanged
	var encryptedToken string
	if req.GitHubToken != nil {
		raw := strings.TrimSpace(*req.GitHubToken)
		if raw == "" {
			tokenAction = tokenClear
		} else {
			if s.cipher == nil {
				return Settings{}, ErrCipherNotConfigured
			}
			if s.ghFactory == nil {
				return Settings{}, fmt.Errorf("github client factory is not configured")
			}
			if _, err := s.ghFactory.NewClient(raw).GetUser(ctx); err != nil {
				return Settings{}, apperrors.New(http.StatusBadRequest, "invalid github token")
			}
			enc, err := s.cipher.Encrypt(raw)
			if err != nil {
				return Settings{}, fmt.Errorf("encrypt github token: %w", err)
			}
			encryptedToken = enc
			tokenAction = tokenSet
		}
	}

	current, err := s.Get(ctx, userID)
	if err != nil {
		return Settings{}, err
	}

	if req.GitHubOwner != nil {
		current.GitHubOwner = strings.TrimSpace(*req.GitHubOwner)
	}
	if req.GitHubRepo != nil {
		current.GitHubRepo = strings.TrimSpace(*req.GitHubRepo)
	}
	if req.RepoPath != nil {
		current.RepoPath = strings.TrimSpace(*req.RepoPath)
	}
	if req.CodegenAgent != nil {
		agent := strings.TrimSpace(*req.CodegenAgent)
		if agent == "" {
			agent = DefaultCodegenAgent
		}
		current.CodegenAgent = agent
	}
	if req.MaxVerifyIterations != nil {
		current.MaxVerifyIterations = *req.MaxVerifyIterations
	}
	if req.MaxReviewCycles != nil {
		current.MaxReviewCycles = *req.MaxReviewCycles
	}

	const upsertSQL = `
		INSERT INTO board_settings (
			user_id, github_token_encrypted, github_owner, github_repo,
			repo_path, codegen_agent, max_verify_iterations, max_review_cycles,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (user_id) DO UPDATE SET
			github_token_encrypted = CASE
				WHEN $9 = 'set'   THEN EXCLUDED.github_token_encrypted
				WHEN $9 = 'clear' THEN NULL
				ELSE board_settings.github_token_encrypted
			END,
			github_owner          = EXCLUDED.github_owner,
			github_repo           = EXCLUDED.github_repo,
			repo_path             = EXCLUDED.repo_path,
			codegen_agent         = EXCLUDED.codegen_agent,
			max_verify_iterations = EXCLUDED.max_verify_iterations,
			max_review_cycles     = EXCLUDED.max_review_cycles,
			updated_at            = now()
		RETURNING ` + settingsSelect

	insertToken := nullableString(encryptedToken)
	row := s.db.QueryRow(ctx, upsertSQL,
		userID,
		insertToken,
		nullableString(current.GitHubOwner),
		nullableString(current.GitHubRepo),
		nullableString(current.RepoPath),
		current.CodegenAgent,
		current.MaxVerifyIterations,
		current.MaxReviewCycles,
		string(tokenAction),
	)

	saved, err := scanSettings(row)
	if err != nil {
		return Settings{}, fmt.Errorf("upsert settings: %w", err)
	}
	return saved, nil
}

// GitHubToken returns the decrypted GitHub PAT for the user, or
// ErrNoGitHubToken if none is stored. Internal use only — never expose
// over HTTP.
func (s *SettingsService) GitHubToken(ctx context.Context, userID uuid.UUID) (string, error) {
	if s.cipher == nil {
		return "", ErrCipherNotConfigured
	}
	var encrypted string
	err := s.db.QueryRow(ctx, `SELECT COALESCE(github_token_encrypted, '') FROM board_settings WHERE user_id = $1`, userID).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNoGitHubToken
		}
		return "", err
	}
	if encrypted == "" {
		return "", ErrNoGitHubToken
	}
	return s.cipher.Decrypt(encrypted)
}

// ListRepos returns repositories accessible with the user's stored token.
// The board UI uses this to populate a repo picker.
func (s *SettingsService) ListRepos(ctx context.Context, userID uuid.UUID) ([]github.Repo, error) {
	if s.ghFactory == nil {
		return nil, fmt.Errorf("github client factory is not configured")
	}
	token, err := s.GitHubToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.ghFactory.NewClient(token).ListUserRepos(ctx, github.ListUserReposOptions{
		Affiliation: "owner,collaborator,organization_member",
		Sort:        "updated",
		PerPage:     100,
	})
}

type tokenWriteAction string

const (
	tokenUnchanged tokenWriteAction = "unchanged"
	tokenSet       tokenWriteAction = "set"
	tokenClear     tokenWriteAction = "clear"
)

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanSettings(r rowScanner) (Settings, error) {
	var (
		st            Settings
		userIDStr     string
		encryptedTok  string
	)
	if err := r.Scan(
		&userIDStr,
		&encryptedTok,
		&st.GitHubOwner,
		&st.GitHubRepo,
		&st.RepoPath,
		&st.CodegenAgent,
		&st.MaxVerifyIterations,
		&st.MaxReviewCycles,
		&st.UpdatedAt,
	); err != nil {
		return Settings{}, err
	}
	id, err := uuid.Parse(userIDStr)
	if err != nil {
		return Settings{}, fmt.Errorf("parse user id: %w", err)
	}
	st.UserID = id
	st.HasGitHubToken = encryptedTok != ""
	return st, nil
}
