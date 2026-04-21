# Self-hosting Eva Board

Eva Board is a single-tenant, single-process Go API plus a static Expo web build.
The only external dependencies are PostgreSQL, an LLM provider (OpenRouter by
default), GitHub, and a coding-agent CLI installed on the host that runs the API.

This guide covers everything from a one-command Docker Compose setup to a
production single-binary install behind a reverse proxy.

---

## 1. Prerequisites

You need **either** the Docker path or the from-source path:

**Docker path (recommended):**

- Docker 24+ with the Compose plugin (`docker compose ...`)

**From-source path:**

- Go 1.23+
- Node 20+ (only required if you want to build the web UI; not needed for
  headless API-only deployments)
- PostgreSQL 16

**Always required:**

- A coding-agent CLI on the host that runs the API. The default is
  [Claude Code](https://docs.anthropic.com/claude/docs/claude-code); any CLI
  that reads a prompt from stdin and edits files in its working directory works
  with `CODEGEN_AGENT=generic`.
- A GitHub Personal Access Token with `repo` scope (per-user, stored
  encrypted at rest — see Section 3).
- An [OpenRouter](https://openrouter.ai) API key (or any OpenAI-compatible
  endpoint via `LLM_BASE_URL`).
- `git` on `PATH` — the agent loop creates per-card git worktrees and
  refuses to start without it.

---

## 2. Quickstart with Docker Compose

```bash
git clone https://github.com/EvaEverywhere/eva-board.git
cd eva-board
cp .env.example .env
# Edit .env — at minimum set LLM_API_KEY and TOKEN_ENCRYPTION_KEY
make up
# UI: http://localhost:8081 (run `make mobile-web` in another shell)
# API: http://localhost:8080
```

`make up` builds and runs three containers:

1. `postgres` — Postgres 16 on host port `5433`.
2. `migrate` — runs `migrate up` once and exits.
3. `api` — the Go server on host port `8080`.

The web UI is not in `docker-compose.yml` yet; run it on the host with
`make mobile-web` (Expo on `http://localhost:8081`) or build a static bundle
and serve it from any web server.

To generate the encryption key:

```bash
openssl rand -base64 32
```

---

## 3. Required environment variables

The backend reads `.env` via `godotenv`. All defaults below match
[`backend/internal/config/config.go`](../backend/internal/config/config.go).

### Core

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | no | `8080` | HTTP port the API listens on. |
| `DATABASE_URL` | yes | `postgres://postgres:postgres@localhost:5433/eva_board?sslmode=disable` | Postgres connection string (pgx). |
| `APP_URL` | no | `http://localhost:8080` | Base URL used in magic-link emails and OAuth redirects. |
| `CORS_ALLOWED_ORIGINS` | no | `http://localhost:8081,http://localhost:8082,http://localhost:19006` | Comma-separated allowlist for the Expo web origin(s). |

### Auth

| Variable | Required | Default | Description |
|---|---|---|---|
| `JWT_SECRET` | yes | dev placeholder | HS256 signing key. **Set a long random value in production.** |
| `RESEND_API_KEY` | no | empty | If set, magic links are emailed via Resend; if empty the dev login endpoint logs the code. |
| `AUTH_EMAIL_FROM` | no | `Eva Board <onboarding@example.com>` | From address for magic-link emails. |
| `MOBILE_APP_SCHEME` | no | `eva-board` | Deep-link scheme used when opening the app from email. |

### LLM

| Variable | Required | Default | Description |
|---|---|---|---|
| `LLM_API_KEY` | yes | empty | OpenRouter (or compatible) API key used for verification + review. |
| `LLM_MODEL` | no | `openai/gpt-4o-mini` | Model passed to the LLM client. |
| `LLM_BASE_URL` | no | `https://openrouter.ai/api/v1` | OpenAI-compatible endpoint. Override to point at another provider. |

### GitHub

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_API_BASE_URL` | no | `https://api.github.com` | Override for GitHub Enterprise. |
| `GITHUB_WEBHOOK_SECRET` | yes for webhooks | empty | Shared secret for `X-Hub-Signature-256` HMAC verification on `/webhooks/github`. Without it, webhook deliveries are rejected. |

The user's per-account GitHub PAT is supplied through the Settings UI and
stored AES-GCM encrypted in `board_settings.github_token_encrypted` — it is
**not** an environment variable.

### Codegen

| Variable | Required | Default | Description |
|---|---|---|---|
| `CODEGEN_AGENT` | no | `claude-code` | `claude-code` (default) or `generic`. |
| `CODEGEN_MODEL` | no | empty | Model override passed to `claude --model`. Ignored by `generic`. |
| `CODEGEN_TIMEOUT` | no | `30m` | Per-invocation timeout (Go duration, e.g. `45m`). |
| `CODEGEN_MAX_OUTPUT_BYTES` | no | `10485760` | Cap on captured combined stdout/stderr (10 MiB default). Negative disables the cap. |
| `CODEGEN_COMMAND` | required for `generic` | empty | Binary the generic CLI agent invokes. |
| `CODEGEN_ARGS` | no | empty | Comma-separated extra args prepended to the generic CLI argv. |

### Encryption

| Variable | Required | Default | Description |
|---|---|---|---|
| `TOKEN_ENCRYPTION_KEY` | yes | empty | 32-byte AES key, base64-encoded, used to encrypt GitHub PATs at rest. Generate with `openssl rand -base64 32`. **Rotating this key invalidates all stored PATs**, requiring users to reconnect GitHub. |

---

## 4. Single-binary deployment

If you don't want Docker, compile a static binary and run it under your
preferred process manager.

```bash
cd backend
go build -o eva-board ./cmd/server
go build -o eva-board-migrate ./cmd/migrate

export DATABASE_URL=postgres://eva:secret@localhost:5432/eva_board?sslmode=disable
./eva-board-migrate up   # apply schema
./eva-board               # start API on $PORT (default 8080)
```

`./cmd/migrate` accepts: `up`, `down N`, `version` (see
`backend/internal/db/migrations/`).

### Sample systemd unit

`/etc/systemd/system/eva-board.service`:

```ini
[Unit]
Description=Eva Board API
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=eva
Group=eva
WorkingDirectory=/opt/eva-board
EnvironmentFile=/etc/eva-board/eva-board.env
ExecStart=/opt/eva-board/eva-board
Restart=on-failure
RestartSec=5s
# The agent shells out to git + a coding-agent CLI; PATH must include both.
Environment=PATH=/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=multi-user.target
```

Put your environment in `/etc/eva-board/eva-board.env` (mode `0600`) using
the same keys as `.env.example`. Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now eva-board
journalctl -u eva-board -f
```

---

## 5. Reverse proxy

Eva Board uses Server-Sent Events at `GET /api/board/events` to push
agent-loop progress to the UI. **Any reverse proxy must disable response
buffering on this route**, or events will batch up and the UI will look
frozen. The handler already sets `X-Accel-Buffering: no` to ask nginx to
disable buffering automatically; the examples below double down for safety.

### Caddy

`/etc/caddy/Caddyfile`:

```caddy
board.example.com {
    encode zstd gzip

    # SSE — keep connections long-lived, no buffering.
    @sse path /api/board/events
    reverse_proxy @sse 127.0.0.1:8080 {
        flush_interval -1
        transport http {
            read_timeout  24h
            write_timeout 24h
        }
    }

    # GitHub webhooks — keep raw body intact for HMAC verification.
    reverse_proxy /webhooks/* 127.0.0.1:8080

    # Everything else.
    reverse_proxy 127.0.0.1:8080
}
```

Caddy issues and renews TLS certificates automatically.

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name board.example.com;

    ssl_certificate     /etc/letsencrypt/live/board.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/board.example.com/privkey.pem;

    # SSE: long-lived, no buffering.
    location /api/board/events {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Connection "";
        proxy_set_header   Host $host;
        proxy_buffering    off;
        proxy_cache        off;
        proxy_read_timeout 24h;
        chunked_transfer_encoding on;
    }

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
    }
}
```

Eva Board does not use WebSockets in v1; SSE is the only streaming protocol
to worry about.

---

## 6. Backups

The only stateful component is PostgreSQL. Everything else is configuration
or transient git worktrees that the agent recreates on demand.

```bash
# Daily full dump
pg_dump --format=custom --no-owner \
    --dbname="$DATABASE_URL" \
    --file=/var/backups/eva-board/$(date -I).dump

# Restore
pg_restore --clean --if-exists \
    --dbname="$DATABASE_URL" \
    /var/backups/eva-board/2026-04-20.dump
```

Tables that hold real state (see [`backend/internal/db/migrations/`](../backend/internal/db/migrations/)):

- `users`, `auth_codes` — accounts and magic-link codes.
- `board_cards` — card content, column, agent status, PR linkage.
- `board_settings` — per-user GitHub config (PAT is AES-GCM encrypted).

Things you do **not** need to back up:

- Per-card git worktrees under `<repo>/../worktrees/` — recreated on next
  agent run.
- The Docker volume `pgdata` if you are already dumping with `pg_dump`.

---

## 7. Coding agent setup

The agent loop shells out to a CLI to actually edit code. Pick one.

### Claude Code (default)

```bash
npm install -g @anthropic-ai/claude-code
claude login    # opens a browser for OAuth

# In Eva Board's environment:
CODEGEN_AGENT=claude-code
# Optional model override; defaults to the Claude Code default if empty.
CODEGEN_MODEL=claude-sonnet-4-5
```

The `claude` binary must be on the API process's `PATH`. Under systemd that
means setting `Environment=PATH=...` (see Section 4) or symlinking into
`/usr/local/bin`.

### Generic CLI

Any CLI that reads a prompt from stdin and makes file edits in its working
directory works. The wrapper lives at
[`backend/internal/codegen/generic.go`](../backend/internal/codegen/generic.go).

```bash
CODEGEN_AGENT=generic
CODEGEN_COMMAND=/usr/local/bin/codex
CODEGEN_ARGS=--auto-approve,--model,gpt-4o
```

Examples:

```bash
# OpenAI Codex CLI
CODEGEN_AGENT=generic
CODEGEN_COMMAND=codex
CODEGEN_ARGS=--auto-approve

# Aider
CODEGEN_AGENT=generic
CODEGEN_COMMAND=aider
CODEGEN_ARGS=--yes,--no-stream,--message-file,-
```

Aider's `--message-file -` flag tells it to read the prompt from stdin,
which is what the wrapper supplies. Tune flags so the CLI runs
non-interactively and exits when done.

---

## 8. GitHub webhook setup

Eva Board's webhook receiver lives at `POST /webhooks/github` and processes
`pull_request` events to advance cards (merged → `done`, closed unmerged →
`review`). It verifies every delivery's `X-Hub-Signature-256` HMAC against
`GITHUB_WEBHOOK_SECRET`.

1. Set a strong shared secret:

   ```bash
   GITHUB_WEBHOOK_SECRET=$(openssl rand -hex 32)
   ```

   Restart Eva Board with the new value.

2. In your GitHub repo, go to **Settings → Webhooks → Add webhook**:

   - **Payload URL:** `https://board.example.com/webhooks/github`
   - **Content type:** `application/json`
   - **Secret:** the value of `GITHUB_WEBHOOK_SECRET`
   - **Events:** select **Let me select individual events** → check
     **Pull requests** only.
   - **Active:** ✓

3. Click **Add webhook**, then trigger a redelivery from the
   "Recent Deliveries" tab to confirm a `200`.

### Local development

Expose your local API to GitHub with a tunnel:

```bash
# ngrok
ngrok http 8080
# → use https://<random>.ngrok.app/webhooks/github as the Payload URL

# or cloudflared
cloudflared tunnel --url http://localhost:8080
```

Update the GitHub webhook to the tunnel URL while you're testing. Keep the
same secret in both ends.

---

## 9. Upgrades

```bash
cd /opt/eva-board                       # or wherever your checkout lives
git pull
# Docker
make up

# From source
cd backend
go build -o eva-board ./cmd/server
go build -o eva-board-migrate ./cmd/migrate
./eva-board-migrate up
sudo systemctl restart eva-board
```

Migrations are forward-only in normal operation; `down` exists but is
intended for development. Always take a `pg_dump` before applying a release
that bumps the migration count.
