# Eva Board

Eva Board is an autonomous dev board: Go API backend + Expo web frontend. Users connect their GitHub repo, create cards with acceptance criteria, and Eva Board autonomously builds, verifies, reviews, and ships PRs — without humans in the loop.

## Repository layout

```
backend/                Go API (Fiber)
  cmd/server/           API server (port 8080)
  cmd/migrate/          Migration CLI
  internal/             Domain packages
mobile/                 Expo web app (React, NativeWind)
  app/                  File-based routing
  components/           UI components
  services/             API clients
  theme/                Design tokens
```

## Backend architecture

Each domain follows: `handler.go` (HTTP) + `service.go` (logic) + `model.go` (types).

Planned domains:
- **board** — Card CRUD, column transitions (backlog → develop → review → pr → done)
- **board/agent** — Autonomous agent loop: code → verify → review → retry → PR
- **board/triage** — Backlog analysis, spring clean, curate
- **codegen** — Pluggable coding agent CLI (Claude Code, generic CLI)
- **github** — GitHub API client (PRs, issues, webhooks)
- **llm** — LLM client (OpenRouter) for verification + review
- **auth** — Email magic-link + JWT (from template)

## The autonomous loop

```
Card → Develop → Agent codes → Verify ACs → Review diff → PR
                    ↑                ↓              ↓
                    └── retry ←── FAIL        REQUEST_CHANGES
```

## Tech stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.23, Fiber v2, pgx v5 |
| Database | PostgreSQL 16 |
| Frontend | Expo 55, React 19, NativeWind |
| Auth | Email magic-link (HS256 JWT) |
| LLM | OpenRouter |
| CI | GitHub Actions |

## Common commands

```bash
make up          # Postgres + migrations + API (Docker)
make down        # Stop all
make dev         # API on host + Expo web
make build       # go build ./...
make test        # go test ./...
make fmt         # gofmt -w .
make lint        # go vet ./...
```

## Environment

Backend reads `.env` via godotenv. Key variables:
- `DATABASE_URL` — Postgres connection
- `JWT_SECRET` — JWT signing key
- `TOKEN_ENCRYPTION_KEY` — AES key for stored GitHub tokens
- `CODEGEN_AGENT` — `claude-code` or `generic` (also drives verification + review + triage)

## Conventions

- **Go**: `internal/` layout, handler/service/model per domain, raw SQL via pgx
- **Errors**: `apperrors` package, return errors up the stack
- **Migrations**: numbered `.up.sql`/`.down.sql` pairs
- **Frontend**: Expo Router groups, NativeWind `className`, tokens in `theme/`
- **API**: REST under `/api/*` (authed), `/webhooks/*` (signature-verified)
- **Real-time**: SSE (Server-Sent Events), not WebSocket
- **Tests**: table-driven Go tests
- **Commits**: conventional format (feat:/fix:/refactor:/docs:/chore:)
- **Do NOT** add Co-Authored-By trailers
- **Do NOT** modify git config
- Per-user codegen agent settings override server env defaults — see
  `internal/board/settings.go` and `cards_handler.go` for the precedence
  rule (`resolveCodegenAgent`).
