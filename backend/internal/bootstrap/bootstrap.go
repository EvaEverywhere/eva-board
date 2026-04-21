package bootstrap

import (
	"context"
	"log"

	"github.com/joho/godotenv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/EvaEverywhere/eva-board/backend/internal/config"
	"github.com/EvaEverywhere/eva-board/backend/internal/db"
	"github.com/EvaEverywhere/eva-board/backend/internal/security"
)

type Core struct {
	Cfg    config.Config
	Pool   *pgxpool.Pool
	Cipher *security.TokenCipher
}

func Init(ctx context.Context) (*Core, error) {
	_ = godotenv.Load()

	cfg := config.Load()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	var cipher *security.TokenCipher
	if cfg.TokenEncryptionKey != "" {
		cipher, err = security.NewTokenCipher(cfg.TokenEncryptionKey)
		if err != nil {
			return nil, err
		}
	} else {
		log.Printf("warning: TOKEN_ENCRYPTION_KEY not set; per-user GitHub tokens cannot be stored")
	}

	return &Core{
		Cfg:    cfg,
		Pool:   pool,
		Cipher: cipher,
	}, nil
}
