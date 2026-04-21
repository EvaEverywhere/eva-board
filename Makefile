SHELL := /bin/bash
COMPOSE := docker compose

.PHONY: help setup doctor up down restart logs dev dev-db build test lint fmt migrate db-shell db-reset mobile mobile-web mobile-install seed token clean sim-ios sim-ios-prebuild phone-build-ios phone-build-android phone-dev phone-tunnel

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

setup: ## Bootstrap .env files and install mobile deps
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@if [ ! -f backend/.env ]; then cp backend/.env.example backend/.env; fi
	@if [ ! -f mobile/.env.local ]; then cp mobile/.env.example mobile/.env.local; fi
	@npm install --prefix mobile
	@echo "Setup complete. Run 'make dev' or 'make up'."

doctor: ## Check local prerequisites (Docker, Go, Node, claude, .env)
	@echo "Eva Board — first-run prerequisite check"
	@echo "----------------------------------------"
	@ok=1; \
	if command -v docker >/dev/null 2>&1; then \
	  echo "  [ok]   docker:  $$(docker --version)"; \
	else \
	  echo "  [FAIL] docker:  not found (install Docker Desktop)"; ok=0; \
	fi; \
	if command -v go >/dev/null 2>&1; then \
	  goVer=$$(go version | awk '{print $$3}' | sed 's/go//'); \
	  goMajor=$$(echo $$goVer | cut -d. -f1); goMinor=$$(echo $$goVer | cut -d. -f2); \
	  if [ "$$goMajor" -gt 1 ] || { [ "$$goMajor" -eq 1 ] && [ "$$goMinor" -ge 23 ]; }; then \
	    echo "  [ok]   go:      $$goVer"; \
	  else \
	    echo "  [FAIL] go:      $$goVer (need >= 1.23)"; ok=0; \
	  fi; \
	else \
	  echo "  [FAIL] go:      not found (need >= 1.23 for host dev)"; ok=0; \
	fi; \
	if command -v node >/dev/null 2>&1; then \
	  nodeVer=$$(node --version | sed 's/v//'); \
	  nodeMajor=$$(echo $$nodeVer | cut -d. -f1); \
	  if [ "$$nodeMajor" -ge 20 ]; then \
	    echo "  [ok]   node:    $$nodeVer"; \
	  else \
	    echo "  [FAIL] node:    $$nodeVer (need >= 20)"; ok=0; \
	  fi; \
	else \
	  echo "  [FAIL] node:    not found (need >= 20 for mobile)"; ok=0; \
	fi; \
	if command -v claude >/dev/null 2>&1; then \
	  echo "  [ok]   claude:  $$(command -v claude)"; \
	else \
	  echo "  [warn] claude:  not found (only required for codegen agent runs)"; \
	fi; \
	if [ -f .env ]; then \
	  echo "  [ok]   .env:    present"; \
	else \
	  if [ -f .env.example ]; then \
	    cp .env.example .env; \
	    echo "  [ok]   .env:    created from .env.example"; \
	  else \
	    echo "  [FAIL] .env:    missing and no .env.example to copy"; ok=0; \
	  fi; \
	fi; \
	if [ -f backend/.env ]; then \
	  echo "  [ok]   backend/.env: present"; \
	else \
	  if [ -f backend/.env.example ]; then \
	    cp backend/.env.example backend/.env; \
	    echo "  [ok]   backend/.env: created from backend/.env.example"; \
	  else \
	    echo "  [FAIL] backend/.env: missing"; ok=0; \
	  fi; \
	fi; \
	echo "----------------------------------------"; \
	if [ $$ok -eq 1 ]; then \
	  echo "All required prerequisites satisfied. Try: make up"; \
	else \
	  echo "One or more required prerequisites are missing. See [FAIL] lines above."; exit 1; \
	fi

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
	(set -a; source backend/.env; set +a; cd backend && DATABASE_URL=postgres://postgres:postgres@localhost:5433/eva_board?sslmode=disable go run ./cmd/server 2>&1 | sed 's/^/[api] /') & \
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
	docker exec -it eva-board-postgres psql -U postgres -d eva_board

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
	curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" -d '{"email":"dev@evaboard.local","name":"Dev User"}'

token: seed ## Alias for seed token command

clean: ## Remove generated artifacts
	$(COMPOSE) down -v
	rm -rf backend/bin mobile/node_modules mobile/.expo

sim-ios-prebuild: ## One-time: generate mobile/ios/ and install pods
	cd mobile && npx expo prebuild --platform ios --no-install
	cd mobile/ios && pod install

sim-ios: ## Build + install + launch Eva Board in the iOS Simulator (uses local xcodebuild, no EAS account needed)
	cd mobile && npx expo run:ios

phone-build-ios: ## Build iOS dev client via EAS (cloud build, ~15min)
	cd mobile && npx eas build --profile development --platform ios

phone-build-android: ## Build Android dev client via EAS
	cd mobile && npx eas build --profile development --platform android

phone-dev: ## Start Metro on LAN for installed dev client to connect
	cd mobile && npx expo start --dev-client --host lan

phone-tunnel: ## Expose local API via ngrok so the phone can reach it
	ngrok http 8080
