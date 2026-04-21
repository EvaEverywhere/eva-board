package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	magiclink "github.com/teslashibe/magiclink-auth-go"
	"github.com/teslashibe/magiclink-auth-go/resend"

	"github.com/EvaEverywhere/eva-board/backend/internal/auth"
	"github.com/EvaEverywhere/eva-board/backend/internal/config"
)

func newMagicLinkService(cfg config.Config, pool *pgxpool.Pool, authSvc *auth.Service) (*magiclink.Service, error) {
	magicCfg := magiclink.Config{
		JWTSecret:   cfg.JWTSecret,
		AppURL:      cfg.AppURL,
		AppName:     "Eva Board",
		FromAddress: cfg.AuthEmailFrom,
		CodeTTL:     10 * time.Minute,
		TokenTTL:    30 * 24 * time.Hour,
		CodeLength:  6,
	}
	if cfg.MobileAppScheme != "" {
		magicCfg.DeepLinkURL = fmt.Sprintf("%s://auth", cfg.MobileAppScheme)
	}

	if err := magicCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid magiclink config: %w", err)
	}

	codeStore := &codeStoreAdapter{pool: pool}
	userStore := &userStoreAdapter{authSvc: authSvc}

	var sender magiclink.EmailSender = &devEmailSender{}
	if strings.TrimSpace(cfg.ResendAPIKey) != "" {
		sender = resend.New(cfg.ResendAPIKey, cfg.AuthEmailFrom)
	}

	return magiclink.New(magicCfg, codeStore, userStore, sender, nil), nil
}

type codeStoreAdapter struct {
	pool *pgxpool.Pool
}

func (s *codeStoreAdapter) Create(ctx context.Context, email, code, token string, expiresAt time.Time) error {
	const query = `
		INSERT INTO auth_codes (email, code, token, expires_at)
		VALUES ($1, $2, $3, $4)
	`

	_, err := s.pool.Exec(ctx, query, strings.ToLower(strings.TrimSpace(email)), code, token, expiresAt)
	return err
}

func (s *codeStoreAdapter) ConsumeByCode(ctx context.Context, email, code string) error {
	const lookupQuery = `
		SELECT id, used, expires_at
		FROM auth_codes
		WHERE email = $1 AND code = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	var (
		id        string
		used      bool
		expiresAt time.Time
	)
	err := s.pool.QueryRow(ctx, lookupQuery, strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(code)).Scan(&id, &used, &expiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return magiclink.ErrInvalidCode
		}
		return err
	}

	if used {
		return magiclink.ErrCodeAlreadyUsed
	}
	if time.Now().After(expiresAt) {
		return magiclink.ErrExpiredCode
	}

	const consumeQuery = `
		UPDATE auth_codes
		SET used = TRUE, used_at = NOW()
		WHERE id = $1 AND used = FALSE
	`
	cmd, err := s.pool.Exec(ctx, consumeQuery, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return magiclink.ErrCodeAlreadyUsed
	}
	return nil
}

func (s *codeStoreAdapter) LookupByToken(ctx context.Context, token string) (string, string, error) {
	const query = `
		SELECT email, code, used, expires_at
		FROM auth_codes
		WHERE token = $1
		LIMIT 1
	`

	var (
		email     string
		code      string
		used      bool
		expiresAt time.Time
	)

	err := s.pool.QueryRow(ctx, query, strings.TrimSpace(token)).Scan(&email, &code, &used, &expiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", magiclink.ErrInvalidToken
		}
		return "", "", err
	}

	if used {
		return "", "", magiclink.ErrTokenAlreadyUsed
	}
	if time.Now().After(expiresAt) {
		return "", "", magiclink.ErrExpiredToken
	}

	return email, code, nil
}

type userStoreAdapter struct {
	authSvc *auth.Service
}

func (s *userStoreAdapter) UpsertUser(ctx context.Context, identityKey, email, displayName string) (string, error) {
	user, err := s.authSvc.UpsertIdentity(ctx, identityKey, email, displayName)
	if err != nil {
		return "", err
	}
	return user.ID, nil
}

func (s *userStoreAdapter) GetUserByEmail(ctx context.Context, email string) (string, string, error) {
	user, err := s.authSvc.GetByEmail(ctx, email)
	if err != nil {
		return "", "", err
	}
	return user.ID, user.Name, nil
}

type devEmailSender struct{}

func (s *devEmailSender) Send(_ context.Context, to, subject, htmlBody string) error {
	log.Printf("[magiclink-dev-email] to=%s subject=%q body=%q", to, subject, htmlBody)
	return nil
}
