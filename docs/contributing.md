---
title: Contributing
sidebar_position: 7
---

# Contributing to Eva Board

Thanks for your interest in contributing! This guide covers what you
need to get started.

The canonical version of this guide lives at
[`CONTRIBUTING.md`](https://github.com/EvaEverywhere/eva-board/blob/main/CONTRIBUTING.md)
in the repo root. The two are kept in sync.

## Developer Certificate of Origin (DCO)

All commits must be signed off to certify you wrote or have the right
to submit the code:

```bash
git commit -s -m "feat: add new feature"
```

This adds a `Signed-off-by` trailer to your commit message, indicating
you agree to the [DCO](https://developercertificate.org/).

## Local development setup

```bash
git clone https://github.com/EvaEverywhere/eva-board.git
cd eva-board
cp .env.example .env
# Fill in required keys (TOKEN_ENCRYPTION_KEY)
make up
# API runs on :8080, web UI on :8081
```

See [Quickstart](./quickstart.md) for the full path including iOS
simulator and dev-token sign-in.

## Code style

**Go** — Format with `gofmt` and lint with `go vet`:

```bash
cd backend && gofmt -w . && go vet ./...
```

**TypeScript** — Type-check with `tsc`:

```bash
cd mobile && npx tsc --noEmit
```

## Pull request checklist

Before opening a PR, confirm:

- [ ] `make test` passes (backend)
- [ ] `npx tsc --noEmit` passes (mobile)
- [ ] No new lint errors (`go vet ./...`)
- [ ] Commits follow conventional commit format
- [ ] Each commit is signed off (`-s` flag)

## Commit format

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add card drag-and-drop
fix: prevent duplicate webhook delivery
refactor: extract agent retry logic
docs: update quickstart instructions
chore: bump Go to 1.23.1
```

## Editing these docs

The Markdown source for this site lives in the top-level
[`docs/`](https://github.com/EvaEverywhere/eva-board/tree/main/docs)
directory. Docusaurus reads from there directly via `path: '../docs'`,
so editing a file in `docs/` updates both the terminal-readable
version and the website on the next deploy. No duplication.

To preview the site locally:

```bash
cd website
npm install
npm run start
# Open http://localhost:3000/eva-board/
```

## Security vulnerabilities

Do **not** open public issues for security bugs. See
[`SECURITY.md`](https://github.com/EvaEverywhere/eva-board/blob/main/SECURITY.md)
for reporting instructions.

## License

By contributing, you agree that your contributions will be licensed
under the
[Apache License 2.0](https://github.com/EvaEverywhere/eva-board/blob/main/LICENSE).
