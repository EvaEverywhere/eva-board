package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teslashibe/template-app/backend/internal/apperrors"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) GetUser(ctx context.Context, userID string) (User, error) {
	const query = `
		SELECT id::text, identity_key, email, name, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user User
	err := s.pool.QueryRow(ctx, query, userID).Scan(
		&user.ID,
		&user.IdentityKey,
		&user.Email,
		&user.Name,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperrors.ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) GetByEmail(ctx context.Context, email string) (User, error) {
	const query = `
		SELECT id::text, identity_key, email, name, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user User
	err := s.pool.QueryRow(ctx, query, strings.ToLower(strings.TrimSpace(email))).Scan(
		&user.ID,
		&user.IdentityKey,
		&user.Email,
		&user.Name,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperrors.ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) UpsertIdentity(ctx context.Context, identityKey, email, name string) (User, error) {
	const query = `
		INSERT INTO users (identity_key, email, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (identity_key)
		DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			updated_at = NOW()
		RETURNING id::text, identity_key, email, name, created_at, updated_at
	`

	cleanEmail := strings.ToLower(strings.TrimSpace(email))
	cleanName := normalizeDisplayName(name, cleanEmail)

	var user User
	err := s.pool.QueryRow(ctx, query, strings.TrimSpace(identityKey), cleanEmail, cleanName).Scan(
		&user.ID,
		&user.IdentityKey,
		&user.Email,
		&user.Name,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func normalizeDisplayName(name, email string) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName != "" {
		return trimmedName
	}

	localPart := strings.Split(strings.TrimSpace(email), "@")[0]
	if localPart == "" {
		return "User"
	}
	return localPart
}
