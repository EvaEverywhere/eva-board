# Contributing to Eva Board

Thanks for your interest in contributing! This guide covers what you need to get started.

## Developer Certificate of Origin (DCO)

All commits must be signed off to certify you wrote or have the right to submit the code:

```bash
git commit -s -m "feat: add new feature"
```

This adds a `Signed-off-by` trailer to your commit message, indicating you agree to the [DCO](https://developercertificate.org/).

## Local development setup

```bash
git clone https://github.com/EvaEverywhere/eva-board.git
cd eva-board
cp .env.example .env
# Fill in required keys (TOKEN_ENCRYPTION_KEY)
make up
# API runs on :8080, web UI on :8081
```

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

## Security vulnerabilities

Do **not** open public issues for security bugs. See [SECURITY.md](SECURITY.md) for reporting instructions.

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
