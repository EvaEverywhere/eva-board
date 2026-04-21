---
title: Cards & repos
sidebar_position: 3
---

# Cards & repos

Eva Board organises work as **cards** scoped to **repos**. A card has
a title, a description, and a list of acceptance criteria. A repo is a
GitHub repository connected to your board with a local checkout path
the agent loop runs against.

This page covers the four pieces of card / repo workflow that aren't
the [autonomous loop](./autonomous-loop.md) itself: multi-repo
support, the AI drafting flow, GitHub issue sync, and the board
switcher.

---

## Multi-repo support

Each user can connect N GitHub repositories. Each repo is its own
board with its own backlog, develop, review, PR, and done columns.
Cards are scoped to a repo via `board_cards.repo_id`.

Invariants enforced by the backend (see
`backend/internal/board/repos.go`):

- `(user_id, owner, name)` is unique. Connecting the same repo twice
  surfaces a 409.
- At most one row per user has `is_default = true`. The default repo is
  what the UI loads on cold start.
- Removing the currently-default repo intentionally does **not**
  auto-promote another repo to default. You pick the next default
  explicitly. This keeps behaviour predictable for the UI and avoids
  surprising the agent loop with a different repo on the next request.

Connect a repo from **Settings → Repos**: enter owner, name, the local
checkout path (must already exist on disk — Eva Board does not clone
for you), and the default branch.

---

## AI drafting flow

The **New card** modal can draft a card for you. You give it a rough
title and (optionally) a sentence of context, and the configured
codegen agent runs in your repo's worktree to produce a structured
card with:

- A polished title
- A description grounded in the actual code
- Testable acceptance criteria
- A "reasoning" string showing why it picked those criteria, so you
  can decide whether to trust the draft

The draft is **read-only** — no cards are persisted by drafting. You
review the draft inline, edit anything you want, then submit. Only
then does the normal create-card endpoint run.

The flow lives in `backend/internal/board/draft.go`. The prompt is
`BuildCardDraftPrompt` in `backend/internal/board/prompts.go`.

The agent has `cd` / `ls` / `cat` access to the repo so it can ground
the draft in real files. It is explicitly told **not** to modify any
files — drafting is a planning task, not a code-edit task.

---

## GitHub issue sync (triage)

Triage analyses your backlog against the actual repo state plus open
GitHub issues, and proposes maintenance actions:

- **`create`** — new cards to add (e.g. an open GitHub issue you
  haven't tracked yet, or a code smell the model thinks deserves a
  card).
- **`close`** — backlog cards whose work has already shipped, or
  whose underlying issue was closed on GitHub.
- **`rewrite`** — cards whose title / description / acceptance
  criteria the model thinks should be updated to match reality.

All proposals are **read-only suggestions**. The UI surfaces them and
you pass only the approved subset to `ApplyProposals`. Nothing changes
on your board until you approve.

Triage takes optional GitHub config (`RepoOwner`, `RepoName`, and a
GitHub client) so the model sees open issues alongside your backlog.
With GitHub disabled, triage runs against backlog cards only.

The flow lives in `backend/internal/board/triage.go`. It runs in
parallel with **spring clean** (orphan branches, stale worktrees) via
`backend/internal/board/curate.go` — both legs are read-only.

---

## Board switcher

The web UI has a **board switcher** in the top nav that swaps between
the repos you've connected. Switching boards reloads cards scoped to
the newly-selected repo without a page refresh, and updates the SSE
subscription so live agent events for the new repo start streaming
immediately.

Under the hood the switcher just changes the `repo_id` query
parameter the cards endpoint is called with. The default repo
(`is_default = true`) is what loads on cold start.

To rename a board, edit the GitHub repo's name on GitHub and update
the corresponding row in **Settings → Repos** to match.

---

## Lifecycle, columns, and statuses

Cards move through these columns:

- **`backlog`** — created but not started. AI drafting and triage both
  put cards here.
- **`develop`** — the autonomous loop is running. Drag here to start.
- **`review`** — verification + review passed; waiting on the PR step,
  or the PR was closed unmerged and the card was bounced back.
- **`pr`** — PR is open on GitHub. Waiting on merge.
- **`done`** — PR merged. Terminal state.

Cards also carry an orthogonal `agent_status`: `idle`, `running`,
`verifying`, `reviewing`, `failed`, `succeeded`. This is what drives
the per-card status pill and the live event stream.

See [Architecture → Data model](../architecture.md#4-data-model) for
the full schema.
