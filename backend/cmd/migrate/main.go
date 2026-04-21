package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"github.com/EvaEverywhere/eva-board/backend/internal/config"
	dbmigrations "github.com/EvaEverywhere/eva-board/backend/internal/db/migrations"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	driver, err := iofs.New(dbmigrations.Files, ".")
	if err != nil {
		log.Fatalf("create migration source: %v", err)
	}

	dbConn, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer dbConn.Close()

	dbDriver, err := postgres.WithInstance(dbConn, &postgres.Config{})
	if err != nil {
		log.Fatalf("create postgres driver: %v", err)
	}

	m, err := migrate.NewWithInstance("iofs", driver, "postgres", dbDriver)
	if err != nil {
		log.Fatalf("create migrate instance: %v", err)
	}
	defer m.Close()

	switch command {
	case "up":
		runUp(m)
	case "down":
		runDown(m)
	case "version":
		runVersion(m)
	default:
		log.Fatalf("unknown command %q (supported: up, down, version)", command)
	}
}

func runUp(m *migrate.Migrate) {
	if len(os.Args) >= 3 {
		steps, err := strconv.Atoi(os.Args[2])
		if err != nil || steps <= 0 {
			log.Fatalf("invalid up steps %q", os.Args[2])
		}
		if err := m.Steps(steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up %d: %v", steps, err)
		}
		fmt.Printf("applied up %d step(s)\n", steps)
		return
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("migrate up: %v", err)
	}
	fmt.Println("migrations up complete")
}

func runDown(m *migrate.Migrate) {
	if len(os.Args) >= 3 {
		steps, err := strconv.Atoi(os.Args[2])
		if err != nil || steps <= 0 {
			log.Fatalf("invalid down steps %q", os.Args[2])
		}
		if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate down %d: %v", steps, err)
		}
		fmt.Printf("applied down %d step(s)\n", steps)
		return
	}

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("migrate down: %v", err)
	}
	fmt.Println("migrations down complete")
}

func runVersion(m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("version: nil (no migrations applied)")
			return
		}
		log.Fatalf("read migration version: %v", err)
	}
	fmt.Printf("version=%d dirty=%t\n", version, dirty)
}
