SHELL := /bin/bash
COMPOSE := docker compose

.PHONY: help setup up down restart logs dev dev-db build test lint fmt migrate db-shell db-reset mobile mobile-web mobile-install seed token clean

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

setup: ## Bootstrap .env files and install mobile deps
	@if [ ! -f backend/.env ]; then cp backend/.env.example backend/.env; fi
	@if [ ! -f mobile/.env.local ]; then cp mobile/.env.example mobile/.env.local; fi
	@npm install --prefix mobile
	@echo "Setup complete. Run 'make dev' or 'make up'."

up: ## Run full stack in Docker (postgres + migrate + api)
	$(COMPOSE) up --build -d

down: ## Stop Docker stack
	$(COMPOSE) down

restart: ## Restart Docker stack
	$(MAKE) down && $(MAKE) up

logs: ## Tail Docker logs
	$(COMPOSE) logs -f

dev-db: ## Start postgres + migrations only
	$(COMPOSE) up -d postgres
	$(COMPOSE) up migrate

dev: dev-db ## Run API on host + Expo web
	@trap 'kill 0' EXIT; \
	(set -a; source backend/.env; set +a; cd backend && DATABASE_URL=postgres://postgres:postgres@localhost:5433/template_app?sslmode=disable go run ./cmd/server 2>&1 | sed 's/^/[api] /') & \
	(cd mobile && EXPO_PUBLIC_API_URL=http://localhost:8080 npm run web 2>&1 | sed 's/^/[expo] /') & \
	wait

build: ## Build backend binaries
	cd backend && go build ./...

test: ## Run backend tests
	cd backend && go test ./...

lint: ## Run backend vet checks
	cd backend && go vet ./...

fmt: ## Format backend source
	cd backend && gofmt -w .

migrate: ## Run migrations in Docker
	$(COMPOSE) up migrate --no-deps

db-shell: ## Open psql shell in postgres container
	docker exec -it template-app-postgres psql -U postgres -d template_app

db-reset: ## Recreate local database and rerun migrations
	$(COMPOSE) down -v
	$(COMPOSE) up -d postgres
	sleep 3
	$(COMPOSE) up migrate --no-deps

mobile: ## Run Expo native dev server
	cd mobile && npm run start

mobile-web: ## Run Expo web
	cd mobile && npm run web

mobile-install: ## Install mobile dependencies
	npm install --prefix mobile

seed: ## Create dev user token from local API
	curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" -d '{"email":"dev@template.local","name":"Dev User"}'

token: seed ## Alias for seed token command

clean: ## Remove generated artifacts
	$(COMPOSE) down -v
	rm -rf backend/bin mobile/node_modules mobile/.expo
