---
title: Architecture
sidebar_position: 5
---

# Eva Board architecture

Eva Board is a Go API (Fiber) plus an Expo web UI, backed by PostgreSQL,
that runs an autonomous build → verify → review → ship loop against a user's
GitHub repository. This document is the deep-dive companion to the
self-hosting guide.

---

## 1. System overview

```
              ┌────────────────────────────────────────────────┐
              │                                                │
  Web UI ─────┤                                                │
  (Expo)      │                                                │
              │       Backend API (Go / Fiber, port 8080)      │       ┌──────────────┐
              │                                                ├──────▶│ PostgreSQL 16 │
  GitHub      │                                                │       └──────────────┘
  Webhooks ───┤                                                │
              │                       │                        │
              │                       ├──▶ Coding agent CLI ───┼──▶ git worktree ──▶ git push
              │                       │   (Claude Code, etc.)  │
              │                       │                        │
              │                       ├──▶ Codegen agent       │   verification + review
              │                       │   (Claude Code in repo │   (same CLI, separate prompts)
              │                       │    worktree)           │
              │                       │                        │
              │                       └──▶ GitHub REST API     │   open PR
              │                                                │
              └────────────────────────────────────────────────┘
```

The API process is the only long-running component. Coding-agent CLIs are
forked per card and live for the duration of a single invocation. Git
worktrees are created on demand under `<RepoPath>/../worktrees/<short-id>/`.

---

## 2. The autonomous loop

The loop is owned by `AgentManager` in
[`backend/internal/board/agent.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/agent.go) and
executed by `runAgent` in
[`backend/internal/board/agent_runner.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/agent_runner.go).
A card is moved to `develop` (via the API or UI), which calls
`AgentManager.StartAgent(cardID)`. That method is idempotent — only one run
per card may exist at a time.

Each `StartAgent` spawns a goroutine that performs the following steps:

1. **Prepare worktree.** `prepareWorktree` resolves the base ref
   (`origin/<base>` preferred, falls back to local `<base>`) and runs
   `git worktree add` at `<repo>/../worktrees/<shortID>`, creating branch
   `<BranchPrefix><shortID>` (default prefix `eva-board/`). Existing
   worktrees are pruned and retried once so re-runs are idempotent.

2. **Initial coding-agent invocation.** `invokeCodingAgent` builds a prompt
   from the card (title, description, acceptance criteria, plus any queued
   feedback) and hands it to the configured `codegen.Agent`
   ([`backend/internal/codegen/agent.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/codegen/agent.go)).
   The CLI is forked with the worktree as its working directory and the
   prompt on stdin. Combined output is captured (default cap: 10 MiB).

3. **Auto-commit + force push.** `autoCommitIfNeeded` stages anything the
   agent left dirty and commits with a pass-numbered message. `pushBranch`
   force-pushes to `origin`. If a per-user GitHub PAT is configured we
   splice it into the remote URL with `x-access-token` so we don't mutate
   the shared remote config.

4. **Verification loop** (≤ `MaxVerifyIterations`, default 3). For each
   iteration: reload the card (so user edits to acceptance criteria are
   honoured), compute `git diff <base>...<branch>`, score it against the
   criteria with an LLM call (`verifyCard` in
   [`backend/internal/board/verify.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/verify.go)).
   On `AllPassed`, exit the loop. Otherwise, re-invoke the coding agent
   with a "failed criteria" feedback string and repeat. Exhaustion → card
   ends in `failed` agent status.

5. **Review loop** (≤ `MaxReviewCycles`, default 5). `ReviewCard`
   ([`backend/internal/board/review.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/review.go))
   asks the LLM to verdict the diff as `APPROVE` or `REQUEST_CHANGES`. On
   `APPROVE`, break out. On `REQUEST_CHANGES`, re-invoke the agent with
   the suggestions, commit + push, **re-run the full verification loop**,
   then loop back to review. Re-verification failure during a review cycle
   is fatal — we don't ship code that no longer satisfies criteria.

6. **Open PR.** `openPullRequest` posts to GitHub via
   [`backend/internal/github/pulls.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/github/pulls.go),
   persists `pr_number`/`pr_url` on the card, moves it to the `pr` column,
   and stamps `review_status = APPROVE`.

7. **Webhook follow-up.** `WebhookHandler`
   ([`backend/internal/board/webhook_handler.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/webhook_handler.go))
   listens for GitHub `pull_request` events. Merged → card moves to
   `done`; closed unmerged → card moves back to `review`.

`StopAgent(cardID)` cancels the run's context. `SubmitFeedback(cardID, ...)`
appends to a per-run queue that's drained into the next agent prompt;
useful while the loop is iterating.

---

## 3. Package map

All packages live under [`backend/internal/`](https://github.com/EvaEverywhere/eva-board/tree/main/backend/internal).

| Package | One-liner |
|---|---|
| `apperrors` | Typed HTTP errors and a Fiber-compatible handler. |
| `auth` | Identity service for the magic-link auth flow + Fiber middleware. |
| `board` | Cards, agent loop, verification, review, settings, curate/triage/spring-clean, SSE broker, GitHub webhook handler. |
| `bootstrap` | Wires config + DB pool + cipher into a `Core` struct used by `cmd/server`. |
| `codegen` | Pluggable coding-agent CLI wrapper. `claude-code` and `generic` implementations. |
| `config` | Env-var loader. The single source of truth for all knobs. |
| `db` | pgx pool helpers and embedded SQL migrations. |
| `github` | GitHub REST client (PRs, issues, users) and webhook signature verification. |
| `httputil` | Small Fiber helpers (current user ID extraction, JSON shape helpers). |
| `llm` | OpenRouter / OpenAI-compatible client used by verify + review. |
| `security` | AES-GCM cipher used to encrypt GitHub PATs at rest. |

Entry points live under `backend/cmd/`:

| Binary | Purpose |
|---|---|
| `cmd/server` | The HTTP API (port 8080). |
| `cmd/migrate` | CLI for `up`, `down N`, `version`. |

---

## 4. Data model

Migrations are numbered `.up.sql`/`.down.sql` pairs under
[`backend/internal/db/migrations/`](https://github.com/EvaEverywhere/eva-board/tree/main/backend/internal/db/migrations) and
embedded into the binary via `embed.go`.

| Table | Migration | Purpose |
|---|---|---|
| `users` | `001_users.up.sql` | Identity row keyed by `identity_key` (`magiclink:<email>` or `dev:<email>`). |
| `auth_codes` | `002_auth_codes.up.sql` | One-time magic-link codes / tokens; consumed on verify. |
| `board_cards` | `003_board_cards.up.sql` | Card content, `column_name` (`backlog`/`develop`/`review`/`pr`/`done`), `agent_status` (`idle`/`running`/`verifying`/`reviewing`/`failed`/`succeeded`), `worktree_branch`, `pr_number`, `pr_url`, `review_status`, freeform `metadata` JSONB. |
| `board_settings` | `004_board_settings.up.sql` | Per-user GitHub config: AES-GCM-encrypted PAT, owner/repo, local `repo_path`, `codegen_agent`, `max_verify_iterations`, `max_review_cycles`. |

Cards are owned by a user (`user_id` FK with `ON DELETE CASCADE`). Position
ordering is per-user-per-column via `(user_id, column_name, position)`.
There is a partial unique index on `pr_number WHERE pr_number IS NOT NULL`
so the webhook handler can resolve `card by PR number` cheaply.

---

## 5. Real-time updates

Live agent progress streams from the API to the UI over Server-Sent Events.

```
agent_runner.go ──▶ Broker.Publish(Event)
                          │
                          ├──▶ in-memory subscriber map keyed by user_id
                          │
                          ▼
                   EventsHandler.Stream  (GET /api/board/events, SSE)
                          │
                          ▼
                   Browser EventSource
```

`Broker` ([`backend/internal/board/events.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/events.go))
keeps a per-process subscriber map and a 256-event ring buffer for resume.
Each subscriber has a 64-event channel; if a client falls behind, the
oldest queued event for that subscriber is dropped silently rather than
blocking the publisher.

`EventsHandler.Stream`
([`backend/internal/board/events_handler.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/board/events_handler.go))
sets `text/event-stream`, honours `Last-Event-ID` for resume, sends a
heartbeat comment every 15 s, and routes only events whose `UserID`
matches the authenticated session — no cross-tenant fan-out.

Event types: `agent_started`, `agent_progress`, `agent_finished`,
`verification_started`, `verification_result`, `review_started`,
`review_result`, `pr_created`, `card_moved`, `error`.

---

## 6. Security model

**Auth.** Email magic-link via the `magiclink-auth-go` library. Successful
verification mints an HS256 JWT signed with `JWT_SECRET`. The JWT is
attached as a bearer token by the Expo client; `auth.Middleware`
([`backend/internal/auth/middleware.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/auth/middleware.go))
verifies it and resolves an internal `user_id` per request. All `/api/*`
routes are mounted behind `authMW.RequireAuth()` (see
[`backend/cmd/server/main.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/cmd/server/main.go)).

**GitHub PATs.** Stored encrypted at rest using AES-GCM with a 32-byte key
sourced from `TOKEN_ENCRYPTION_KEY`
([`backend/internal/security/encryption.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/security/encryption.go)).
Plaintext only ever lives in memory while the agent loop is running and is
never logged. Rotating the key invalidates all stored PATs and forces users
to reconnect GitHub.

**Webhook deliveries.** `POST /webhooks/github` is mounted **outside** the
auth middleware on purpose — GitHub authenticates with the
`X-Hub-Signature-256` HMAC. `github.VerifySignature`
([`backend/internal/github/webhook.go`](https://github.com/EvaEverywhere/eva-board/blob/main/backend/internal/github/webhook.go))
constant-time-compares the delivered HMAC against `HMAC-SHA256(body,
GITHUB_WEBHOOK_SECRET)`. Empty secret = misconfiguration → reject.

**Internal IDs.** Cards expose UUIDs externally; the agent loop uses the
first 8 hex chars of the UUID for branch names and worktree paths so
collisions are extremely unlikely but the values stay human-readable.

---

## 7. What's NOT in v1

The v1 launch is intentionally single-tenant, single-process, single-user-
per-account. Features that have been deliberately deferred:

- **Multi-tenancy beyond per-user isolation.** Each user has their own
  cards, settings, GitHub PAT, and agent runs, but there is no
  organisation/team layer, no shared boards, no role-based access.
- **HA / horizontally scaled API.** The SSE broker is in-process. Running
  more than one API replica would split subscribers and lose events. A
  Redis-backed (or NATS-backed) broker is the obvious next step.
- **Background scheduler binary.** Triage and spring-clean run on demand
  via the curate handler. There is no separate `cmd/scheduler` cron
  process yet.
- **Native mobile apps.** Native iOS and Android via Expo + EAS. See
  [`docs/mobile.md`](./mobile.md) for the simulator +
  device install path.
- **Billing / metering.** No Stripe, no per-tenant LLM cost accounting.
- **GDPR delete + export tooling.** `users` cascades to cards and
  settings, but there is no audited self-serve export/delete flow.
- **Pluggable LLM providers beyond OpenAI-compatible.** The `llm` package
  speaks the OpenAI chat-completions schema via OpenRouter. Anthropic
  native, Bedrock, Vertex, etc. would each need an adapter.

These are tracked as future v1.1+ work; see the GitHub issue tracker for
the latest list.
