# Eva Board

[![Backend CI](https://github.com/EvaEverywhere/eva-board/actions/workflows/backend.yml/badge.svg)](https://github.com/EvaEverywhere/eva-board/actions/workflows/backend.yml)
[![Mobile CI](https://github.com/EvaEverywhere/eva-board/actions/workflows/mobile.yml/badge.svg)](https://github.com/EvaEverywhere/eva-board/actions/workflows/mobile.yml)

Autonomous dev board — builds, verifies, reviews, and ships code without you in the loop.

## How it works

Tools like Vibe Kanban make humans faster at reviewing agent work. Eva Board removes humans from the loop entirely. The agent verifies against acceptance criteria, self-reviews its own diff, retries on failure, and creates the PR.

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│   Create card ──► Develop ──► Verify ──► Review ──► PR  │
│       ▲                         │          │            │
│       │                         ▼          ▼            │
│       │                     ┌──────────────────┐        │
│       │                     │  Failed? Retry   │        │
│       │                     │  with feedback    │        │
│       │                     └──────────────────┘        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

1. Create a card with acceptance criteria
2. Move to "Develop" — agent starts automatically
3. Agent codes, commits, pushes
4. Eva Board verifies against your acceptance criteria
5. Eva Board reviews the diff for quality
6. Failed? Agent retries with feedback (automatic)
7. Passed? PR created on GitHub automatically

Plus autonomous backlog maintenance: triage analyzes your repo and proposes new issues; spring clean finds orphan branches and stale worktrees.

## Quickstart

**Prerequisites:** Go 1.23+, Node 20+, Docker, a coding agent CLI (Claude Code, etc.)

```bash
git clone https://github.com/EvaEverywhere/eva-board.git
cd eva-board
cp .env.example .env
# Fill in TOKEN_ENCRYPTION_KEY (verification + review run through Codegen)
make up
# Open http://localhost:8081
```

## Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.23, Fiber, pgx, PostgreSQL 16 |
| Frontend | Expo (web), React, NativeWind |
| Auth | Email magic-link (passwordless) |
| Agents | Pluggable: Claude Code, any CLI agent |
| LLM | OpenRouter (verification + review) |
| CI | GitHub Actions |

## Mobile (iOS / Android)

Eva Board ships as a React Native app. To install on your phone and develop with hot-reload from your Mac, see [docs/PHONE_DEV_SETUP.md](docs/PHONE_DEV_SETUP.md).

The backend URL can be changed at runtime from **Settings → Backend** in the app — no rebuild needed when switching between localhost, ngrok, or a deployed instance.

## Documentation

- [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md) — deploy with Docker Compose or a single binary, reverse proxy, backups, agent CLI setup, GitHub webhook setup.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — system overview, the autonomous loop, package map, data model, SSE design, security model.
- [CONTRIBUTING.md](CONTRIBUTING.md) — development setup and PR conventions.
- [SECURITY.md](SECURITY.md) — vulnerability disclosure policy.
- [CLAUDE.md](CLAUDE.md) — repository tour for Claude Code and other coding agents.

## License

Apache-2.0 — see [LICENSE](LICENSE)
