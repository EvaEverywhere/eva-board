---
title: Introduction
slug: /
sidebar_position: 1
---

# Eva Board

**Autonomous dev board — builds, verifies, reviews, and ships code without you in the loop.**

Tools like Vibe Kanban make humans faster at reviewing agent work. Eva Board
removes humans from the loop entirely. The agent verifies against acceptance
criteria, self-reviews its own diff, retries on failure, and creates the PR.

## How it works

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│   Create card ──► Develop ──► Verify ──► Review ──► PR  │
│       ▲                         │          │            │
│       │                         ▼          ▼            │
│       │                     ┌──────────────────┐        │
│       │                     │  Failed? Retry   │        │
│       │                     │  with feedback   │        │
│       │                     └──────────────────┘        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

1. Create a card with acceptance criteria
2. Move to "Develop" — agent starts automatically
3. Agent codes, commits, pushes
4. The agent scores itself against your acceptance criteria
5. A second agent session reviews the diff for quality — fresh context, no self-bias
6. Failed either check? Agent retries with the feedback automatically
7. Both pass? PR opened on GitHub automatically

Plus autonomous backlog maintenance: triage analyzes your repo and proposes
new issues; spring clean finds orphan branches and stale worktrees.

## Tech stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.23, Fiber, pgx, PostgreSQL 16 |
| Frontend | Expo (web + native), React, NativeWind |
| Auth | Email magic-link (passwordless) |
| Agents | Pluggable: Claude Code, any CLI agent |
| LLM / Reviewer | Codegen agent (Claude Code default; pluggable to Codex, Aider, OpenHands, Cline, or any CLI) |
| CI | GitHub Actions |

## Where to go next

- **[Quickstart](./quickstart.md)** — get the stack running locally with Docker Compose or the iOS simulator.
- **[Autonomous loop](./concepts/autonomous-loop.md)** — the 7-step build → verify → review → ship cycle, in detail.
- **[Codegen agents](./concepts/codegen-agents.md)** — swap Claude Code for Codex, Aider, OpenHands, Cline, or any CLI.
- **[Cards & repos](./concepts/cards-and-repos.md)** — multi-repo support, AI-drafted cards, GitHub issue sync, board switcher.
- **[Self-hosting](./self-hosting.md)** — Docker Compose, single binary, reverse proxy, backups, GitHub webhooks.
- **[Architecture](./architecture.md)** — system overview, package map, data model, SSE design, security model.
- **[Mobile](./mobile.md)** — install on a real iPhone or Android device with hot-reload from your Mac.
- **[Contributing](./contributing.md)** — setup, code style, PR conventions, DCO sign-off.
