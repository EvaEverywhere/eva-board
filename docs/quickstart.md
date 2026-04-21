---
title: Quickstart
sidebar_position: 2
---

# Quickstart

Two paths get you to a running Eva Board in under 10 minutes:

- **[Docker Compose](#path-a--docker-compose-recommended)** — full stack on
  any machine with Docker.
- **[iOS Simulator](#path-b--ios-simulator-mac-only)** — backend on your Mac
  plus the native app in the simulator, no phone or Apple Developer account
  needed.

For real devices, see [Mobile (iOS / Android)](./mobile.md). For production
deployment, see [Self-hosting](./self-hosting.md).

---

## Prerequisites

- Go 1.23+
- Node 20+
- Docker 24+ with the Compose plugin (`docker compose ...`)
- A coding-agent CLI on the host that runs the API. The default is
  [Claude Code](https://docs.anthropic.com/claude/docs/claude-code); any
  CLI that reads a prompt from stdin and edits files in its working
  directory works with `CODEGEN_AGENT=generic`.
- `git` on `PATH` — the agent loop creates per-card git worktrees and
  refuses to start without it.

---

## Path A — Docker Compose (recommended)

```bash
git clone https://github.com/EvaEverywhere/eva-board.git
cd eva-board
cp .env.example .env
# Edit .env — at minimum set TOKEN_ENCRYPTION_KEY:
#   openssl rand -base64 32
make up
```

`make up` builds and runs three containers:

1. `postgres` — Postgres 16 on host port `5433`
2. `migrate` — runs migrations once and exits
3. `api` — the Go server on host port `8080`

Then start the web UI on the host:

```bash
make mobile-web
# Open http://localhost:8081
```

The web UI is intentionally not in `docker-compose.yml` yet — running Expo
on the host gives you fast hot-reload while you iterate.

### First sign-in

Eva Board uses email magic-link auth. In dev (when `RESEND_API_KEY` is
empty) the magic-link code is logged to the API container instead of being
emailed:

```bash
make logs                              # tail the API; the code shows up here
```

For a script-friendly sign-in, issue a dev JWT directly:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","name":"You"}' | jq -r .token)
echo "$TOKEN"
```

Paste the token into the web UI's "Use existing token" field, or — for the
simulator — deep-link it into the app:

```bash
xcrun simctl openurl booted "eva-board://?token=$TOKEN"
```

---

## Path B — iOS Simulator (Mac-only)

Fastest way to see Eva Board running as a native app, without touching EAS
or an Apple Developer account. Requires Xcode + CocoaPods installed.

```bash
# one-time: generates mobile/ios/ and installs pods (~3-5 min)
make sim-ios-prebuild

# builds, installs to default simulator, launches, starts Metro (~10 min first build)
make sim-ios
```

The simulator shares your Mac's loopback, so the app reaches
`http://localhost:8080` directly — no LAN IP or ngrok needed.

Sign in with the dev-token snippet from the [First sign-in](#first-sign-in)
section above.

After the first build, subsequent code changes hot-reload via Metro
automatically.

---

## Connect a repo

Once you're signed in, open **Settings** in the app:

1. Paste a **GitHub Personal Access Token** with `repo` scope. Tokens are
   AES-GCM encrypted at rest using `TOKEN_ENCRYPTION_KEY`.
2. Add a repo: pick **owner / name**, paste the **local checkout path**
   (Eva Board's agent loop runs against this checkout), and pick the
   default branch.
3. Mark it as default. Multi-repo is supported — each repo is its own
   board with its own backlog. See [Cards & repos](./concepts/cards-and-repos.md).

Now create your first card, drag it to **Develop**, and watch the
[autonomous loop](./concepts/autonomous-loop.md) take over.

---

## What's next

- **Want a different coding agent?** See [Codegen agents](./concepts/codegen-agents.md)
  for Codex, Aider, OpenHands, Cline, and custom CLIs.
- **Running on your phone?** See [Mobile (iOS / Android)](./mobile.md) for
  Expo Go and EAS dev-build paths.
- **Production deployment?** See [Self-hosting](./self-hosting.md) for
  single-binary, systemd, reverse proxy, backups, and GitHub webhook setup.
- **How does it actually work?** See [Architecture](./architecture.md) for
  the data model, SSE design, and security model.
