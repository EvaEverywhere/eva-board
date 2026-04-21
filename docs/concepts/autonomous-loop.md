---
title: Autonomous loop
sidebar_position: 1
---

# The autonomous loop

Eva Board's defining feature is the **build → verify → review → ship**
cycle that runs without a human in the loop. This page walks the cycle
step-by-step so you know exactly what the agent is doing on your behalf
when you drag a card to **Develop**.

```
Card → Develop → Agent codes → Verify ACs → Review diff → PR
                    ▲                │              │
                    │                ▼              ▼
                    └── retry ←── FAILED      REQUEST_CHANGES
```

The loop is owned by `AgentManager` in `backend/internal/board/agent.go`
and executed by `runAgent` in `backend/internal/board/agent_runner.go`.
A card is moved to `develop` (via the API or UI), which calls
`AgentManager.StartAgent(cardID)`. That method is **idempotent** — only
one run per card may exist at a time.

Each `StartAgent` spawns a goroutine that performs the seven steps below.

---

## 1. Prepare worktree

`prepareWorktree` resolves the base ref (`origin/<base>` preferred,
falls back to local `<base>`) and runs `git worktree add` at
`<repo>/../worktrees/<shortID>`, creating branch
`<BranchPrefix><shortID>` (default prefix `eva-board/`).

Existing worktrees are pruned and retried once so re-runs are
idempotent — you can stop a card mid-loop and restart it without
manually cleaning up.

## 2. Initial coding-agent invocation

`invokeCodingAgent` builds a prompt from the card (title, description,
acceptance criteria, plus any queued feedback from the user) and hands
it to the configured `codegen.Agent`. The CLI is forked with the
worktree as its working directory and the prompt on stdin. Combined
output is captured (default cap: 10 MiB).

See [Codegen agents](./codegen-agents.md) for which CLIs you can swap
in.

## 3. Auto-commit + force push

`autoCommitIfNeeded` stages anything the agent left dirty and commits
with a pass-numbered message. `pushBranch` force-pushes to `origin`. If
a per-user GitHub PAT is configured we splice it into the remote URL
with `x-access-token` so we don't mutate the shared remote config.

## 4. Verification loop

```
loop up to MaxVerifyIterations (default 3):
  reload card                      # honours user edits to ACs mid-run
  diff = git diff <base>...<branch>
  result = LLM(criteria, diff)
  if result.AllPassed:
    break
  re-invoke agent with "failed criteria" feedback
```

Reloading the card on every iteration means you can edit acceptance
criteria while the loop is running and the next pass picks up your
changes. Exhausting `MaxVerifyIterations` ends the run in `failed`
agent status — Eva Board does not ship code that doesn't satisfy your
criteria.

The verifier prompt + scoring lives in
`backend/internal/board/verify.go`.

## 5. Review loop

```
loop up to MaxReviewCycles (default 5):
  verdict = LLM_review(diff)       # APPROVE | REQUEST_CHANGES
  if verdict == APPROVE:
    break
  re-invoke agent with reviewer suggestions
  commit + push
  re-run the FULL verification loop  # don't ship code that no longer satisfies ACs
```

Re-verification failure during a review cycle is **fatal** for the run.
Once the reviewer asks for changes, the new diff must pass verification
again before the run can continue.

The reviewer prompt + verdict parser lives in
`backend/internal/board/review.go`.

## 6. Open PR

`openPullRequest` posts to GitHub via `backend/internal/github/pulls.go`,
persists `pr_number` / `pr_url` on the card, moves it to the **PR**
column, and stamps `review_status = APPROVE`.

The PR body includes the card title, description, and acceptance
criteria so reviewers (if any) have full context without round-tripping
to the board.

## 7. Webhook follow-up

`WebhookHandler` (in `backend/internal/board/webhook_handler.go`)
listens for GitHub `pull_request` events. **Merged** → card moves to
**Done**. **Closed unmerged** → card moves back to **Review** with the
GitHub state preserved on the card.

See [Self-hosting → GitHub webhook setup](../self-hosting.md#8-github-webhook-setup)
for how to wire the webhook in your repo.

---

## Live controls

While the loop is running you have two levers:

- **`StopAgent(cardID)`** — cancels the run's context. The current
  agent invocation is killed, the worktree is left in place for
  inspection, and the card returns to `develop` with `agent_status =
  idle`.
- **`SubmitFeedback(cardID, feedback)`** — appends to a per-run queue
  that's drained into the next agent prompt. Useful when you're
  watching the loop iterate and want to nudge it ("don't touch the
  migrations directory") without stopping the run.

---

## Real-time progress

Every transition in the loop emits an event onto an SSE broker that
the UI subscribes to. You see verification scores, review verdicts,
and PR creation appear in the card detail panel within ~1 second of
the agent emitting them.

Event types include `agent_started`, `agent_progress`,
`verification_started`, `verification_result`, `review_started`,
`review_result`, `pr_created`, `card_moved`, and `error`.

See [Architecture → Real-time updates](../architecture.md#5-real-time-updates)
for the full SSE design.
