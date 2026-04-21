---
title: Codegen agents
sidebar_position: 2
---

# Codegen agents

Eva Board's [autonomous loop](./autonomous-loop.md) shells out to a
**coding-agent CLI** to actually edit code, verify diffs, and produce
review verdicts. The same CLI handles all three roles (implementer,
verifier, reviewer) — there is no separate LLM provider to configure.

The agent is pluggable. Claude Code is the default; you can swap in
any CLI that reads a prompt from stdin and makes file edits in its
working directory.

---

## Built-in presets

Pick a preset in **Settings → Coding agent** in the app. Each user can
pick their own — server-side env defaults are only used when a user
hasn't selected one.

| Preset | `CODEGEN_AGENT` | Notes |
|---|---|---|
| **Claude Code** | `claude-code` | Default. Best long-context reasoning today; needs the `claude` CLI on `PATH` and `claude login`. |
| **Codex CLI** | `generic` | OpenAI Codex CLI; pair with `CODEGEN_COMMAND=codex` and `CODEGEN_ARGS=--auto-approve`. |
| **Aider** | `generic` | Pair with `CODEGEN_COMMAND=aider` and `CODEGEN_ARGS=--yes,--no-stream,--message-file,-`. |
| **OpenHands** | `generic` | Pair with `CODEGEN_COMMAND=openhands` and any non-interactive flags your install requires. |
| **Cline** | `generic` | Wrap the Cline CLI shim; pair with `CODEGEN_COMMAND=cline`. |
| **Custom** | `generic` | Any CLI that reads a prompt from stdin and edits files in its working directory. |

The wrapper for the generic path lives in
`backend/internal/codegen/generic.go`. The wrapper for Claude Code lives
in `backend/internal/codegen/claude_code.go`.

---

## Claude Code (default)

```bash
npm install -g @anthropic-ai/claude-code
claude login    # opens a browser for OAuth

# In Eva Board's environment:
CODEGEN_AGENT=claude-code
# Optional model override; defaults to the Claude Code default if empty.
CODEGEN_MODEL=claude-sonnet-4-5
```

The `claude` binary must be on the API process's `PATH`. Under systemd
that means setting `Environment=PATH=...` (see
[Self-hosting → Sample systemd unit](../self-hosting.md#sample-systemd-unit))
or symlinking into `/usr/local/bin`.

---

## Generic CLI

Any CLI that reads a prompt from stdin and makes file edits in its
working directory works.

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

## Per-user overrides

Server-side `CODEGEN_AGENT`, `CODEGEN_COMMAND`, and `CODEGEN_ARGS` act
as **defaults** for users who have not picked an agent in the Settings
UI. Once a user selects a preset (Claude Code, Codex, Aider, OpenHands,
Cline, or Custom) those values are persisted in `board_settings` and
override the env defaults for that user.

The precedence rule lives in
`backend/internal/board/cards_handler.go` (`resolveCodegenAgent`).

---

## Tunables

| Variable | Default | Description |
|---|---|---|
| `CODEGEN_TIMEOUT` | `30m` | Per-invocation timeout (Go duration, e.g. `45m`). |
| `CODEGEN_MAX_OUTPUT_BYTES` | `10485760` | Cap on captured combined stdout/stderr (10 MiB). Negative disables the cap. |

Bump `CODEGEN_TIMEOUT` for large refactors; the agent will get killed
mid-run if a single invocation exceeds it, which counts as a failed
verification iteration.

---

## What the agent sees

Each invocation runs in the per-card git worktree at
`<repo>/../worktrees/<shortID>` with the prompt on stdin. The prompt
includes:

- The card title, description, and acceptance criteria
- The current diff against the base branch (after the first iteration)
- Any verifier-failure feedback from the previous iteration
- Any reviewer-suggested changes from the previous review cycle
- Any user-supplied feedback queued via `SubmitFeedback`

The agent has full read/write access to the worktree and can run shell
commands within it. It does **not** have access to the host filesystem
outside the worktree — the wrapper sets `chdir` and that's the only
sandboxing primitive Eva Board relies on. Run untrusted prompts in a
container or VM if that matters to you.
