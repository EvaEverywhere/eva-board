package board

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// runAgent executes the full autonomous loop for one card. It runs in its
// own goroutine and is the only goroutine that mutates `run` aside from the
// feedback queue. The supplied ctx is the run-scoped context; cancellation
// (via StopAgent) is treated as graceful shutdown rather than a failure
// state on the card.
//
// The loop mirrors the eva monolith's hard-won shape:
//  1. Resolve card; create per-card branch + git worktree.
//  2. Initial coding-agent invocation (with any queued feedback).
//  3. Auto-commit + push.
//  4. Verification loop (≤ MaxVerifyIterations): score diff, on fail
//     reinvoke with failed-criteria feedback; on pass continue.
//  5. Review loop (≤ MaxReviewCycles): LLM review; REQUEST_CHANGES
//     reinvokes the coding agent and re-runs verification before reviewing
//     again; APPROVE breaks out.
//  6. Open PR via the GitHub client; persist PR number/URL on the card and
//     move it to the `pr` column.
func (m *AgentManager) runAgent(ctx context.Context, run *agentRun) error {
	cardID := run.cardID

	if strings.TrimSpace(m.cfg.RepoPath) == "" {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return errors.New("repo path is not configured")
	}

	card, err := m.cards.GetByID(ctx, cardID)
	if err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("load card: %w", err)
	}

	branch := m.cfg.BranchPrefix + shortID(cardID)
	worktreeDir := filepath.Join(filepath.Dir(m.cfg.RepoPath), "worktrees", shortID(cardID))
	run.branch = branch
	run.worktreeDir = worktreeDir

	if err := m.prepareWorktree(ctx, branch, worktreeDir); err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("prepare worktree: %w", err)
	}

	if err := m.cards.SetWorktreeBranch(ctx, cardID, branch); err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("persist worktree branch: %w", err)
	}
	if err := m.cards.SetAgentStatus(ctx, cardID, AgentStatusRunning); err != nil {
		return fmt.Errorf("set running status: %w", err)
	}

	log.Printf("[board-agent] starting agent loop for card %s on branch %s", shortID(cardID), branch)

	// Phase 1: initial coding-agent invocation.
	if err := m.invokeCodingAgent(ctx, run, *card, run.drainFeedback()); err != nil {
		if isCancelled(ctx, err) {
			_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusIdle)
			return err
		}
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("initial agent invocation: %w", err)
	}
	if err := m.commitAndPush(ctx, run, 0); err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("commit+push after initial invocation: %w", err)
	}

	// Phase 2: verification loop.
	verifyResult, err := m.runVerificationLoop(ctx, run)
	if err != nil {
		if isCancelled(ctx, err) {
			_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusIdle)
			return err
		}
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("verification loop: %w", err)
	}
	if !verifyResult.AllPassed {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("verification failed after %d iterations", m.cfg.MaxVerifyIterations)
	}

	// Phase 3: review loop.
	finalReview, err := m.runReviewLoop(ctx, run)
	if err != nil {
		if isCancelled(ctx, err) {
			_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusIdle)
			return err
		}
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("review loop: %w", err)
	}
	if finalReview.Verdict != ReviewApprove {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("review never approved after %d cycles", m.cfg.MaxReviewCycles)
	}

	// Phase 4: open the PR and move the card to ColumnPR.
	freshCard, err := m.cards.GetByID(ctx, cardID)
	if err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("reload card before PR: %w", err)
	}
	if err := m.openPullRequest(ctx, *freshCard, run, verifyResult.Verdicts, finalReview); err != nil {
		_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusFailed)
		return fmt.Errorf("open PR: %w", err)
	}

	_ = m.cards.SetAgentStatus(context.Background(), cardID, AgentStatusSucceeded)
	log.Printf("[board-agent] agent loop succeeded for card %s", shortID(cardID))
	return nil
}

// prepareWorktree creates the per-card git worktree at worktreeDir checked
// out to branch. If the worktree already exists from a previous run we prune
// it first so re-runs are idempotent. The branch is created off
// origin/<base> (preferred) or refs/heads/<base> as a fallback.
func (m *AgentManager) prepareWorktree(ctx context.Context, branch, worktreeDir string) error {
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	baseRef, err := resolveBaseRef(ctx, m.cfg.RepoPath, m.cfg.BaseBranch)
	if err != nil {
		return err
	}

	args, err := worktreeAddArgs(ctx, m.cfg.RepoPath, branch, worktreeDir, baseRef)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", m.cfg.RepoPath, "worktree", "add"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	outStr := string(out)
	// Reuse-existing-worktree paths: prune and retry once.
	if strings.Contains(outStr, "already exists") || strings.Contains(outStr, "already checked out") || strings.Contains(outStr, "already used by") {
		_ = os.RemoveAll(worktreeDir)
		_ = exec.CommandContext(ctx, "git", "-C", m.cfg.RepoPath, "worktree", "prune").Run()
		retry, retryErr := worktreeAddArgs(ctx, m.cfg.RepoPath, branch, worktreeDir, baseRef)
		if retryErr != nil {
			return retryErr
		}
		retryCmd := exec.CommandContext(ctx, "git", append([]string{"-C", m.cfg.RepoPath, "worktree", "add"}, retry...)...)
		if retryOut, retryErr := retryCmd.CombinedOutput(); retryErr != nil {
			return fmt.Errorf("git worktree add (retry): %s: %w", string(retryOut), retryErr)
		}
		return nil
	}
	return fmt.Errorf("git worktree add: %s: %w", outStr, err)
}

// resolveBaseRef finds an existing ref to start the new branch from. We try
// origin/<base> first (the canonical state), then the local branch. This
// matches the eva monolith's behaviour and keeps the loop resilient on
// freshly cloned repos that may not have the local branch yet.
func resolveBaseRef(ctx context.Context, repoPath, base string) (string, error) {
	candidates := []string{
		"refs/remotes/origin/" + base,
		"origin/" + base,
		"refs/heads/" + base,
		base,
	}
	for _, ref := range candidates {
		if err := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run(); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no git ref found for base branch %q", base)
}

// worktreeAddArgs picks the right `git worktree add` argv based on whether
// the branch already exists locally. New branches are created with -b off
// the resolved base ref; existing branches are checked out as-is.
func worktreeAddArgs(ctx context.Context, repoPath, branch, worktreeDir, baseRef string) ([]string, error) {
	exists, err := branchExists(ctx, repoPath, branch)
	if err != nil {
		return nil, err
	}
	if exists {
		return []string{worktreeDir, branch}, nil
	}
	return []string{"-b", branch, worktreeDir, baseRef}, nil
}

func branchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	err := exec.CommandContext(ctx, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check branch ref %q: %w", branch, err)
}

// invokeCodingAgent runs the pluggable codegen.Agent on the worktree with a
// prompt built from the card. Any queued feedback is appended to the prompt.
// The agent is expected to make file changes inside run.worktreeDir; commit
// and push happen separately via commitAndPush.
func (m *AgentManager) invokeCodingAgent(ctx context.Context, run *agentRun, card Card, feedback string) error {
	if m.code == nil {
		return errors.New("coding agent is not configured")
	}
	prompt := buildAgentPrompt(card, feedback)
	res, err := m.code.Run(ctx, prompt, run.worktreeDir)
	if err != nil {
		// Surface the captured tail of the agent's combined output so logs
		// show why the CLI failed.
		out := res.Output
		if len(out) > 4000 {
			out = out[len(out)-4000:]
		}
		return fmt.Errorf("%s run failed (exit=%d): %w\n--- agent output (tail) ---\n%s", m.code.Name(), res.ExitCode, err, out)
	}
	return nil
}

// commitAndPush stages anything the agent left uncommitted, commits with a
// pass-numbered message, and force-pushes the branch to origin so the diff
// is visible to verification/review and any human onlooker.
func (m *AgentManager) commitAndPush(ctx context.Context, run *agentRun, iteration int) error {
	if _, err := autoCommitIfNeeded(ctx, run.worktreeDir, iteration); err != nil {
		return fmt.Errorf("auto-commit: %w", err)
	}
	if err := pushBranch(ctx, run.worktreeDir, run.branch, m.cfg.GitHubToken); err != nil {
		return fmt.Errorf("push branch: %w", err)
	}
	return nil
}

// autoCommitIfNeeded stages and commits any uncommitted changes in the
// worktree. Returns true if a commit was created. Agents that can edit files
// but cannot drive git themselves rely on this.
func autoCommitIfNeeded(ctx context.Context, worktreeDir string, iteration int) (bool, error) {
	if strings.TrimSpace(worktreeDir) == "" {
		return false, nil
	}
	statusCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "status", "--porcelain")
	out, err := statusCmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, nil
	}
	if err := exec.CommandContext(ctx, "git", "-C", worktreeDir, "add", "-A").Run(); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}
	msg := fmt.Sprintf("agent: auto-commit changes from pass %d", iteration)
	commitCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "commit", "-m", msg)
	if commitOut, err := commitCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git commit: %s: %w", strings.TrimSpace(string(commitOut)), err)
	}
	return true, nil
}

// pushBranch force-pushes the branch to origin. When a GitHub token is
// configured we splice it into the origin URL so we don't have to mutate the
// shared remote config (matching the eva monolith's race-safe approach).
func pushBranch(ctx context.Context, worktreeDir, branch, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		cmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "push", "--force", "-u", "origin", branch)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	origURLBytes, err := exec.CommandContext(ctx, "git", "-C", worktreeDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return fmt.Errorf("get remote url: %w", err)
	}
	authedURL, err := authenticatedCloneURL(strings.TrimSpace(string(origURLBytes)), token)
	if err != nil {
		return fmt.Errorf("build authenticated url: %w", err)
	}
	pushCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "push", "--force", authedURL, branch+":refs/heads/"+branch)
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// authenticatedCloneURL splices an x-access-token into an https GitHub URL.
// SSH and other schemes are returned unchanged because they auth via keys,
// not the URL. Returns an error only if the input is not a parseable URL.
func authenticatedCloneURL(remoteURL, token string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return "", fmt.Errorf("empty remote url")
	}
	if !strings.HasPrefix(remoteURL, "https://") && !strings.HasPrefix(remoteURL, "http://") {
		return remoteURL, nil
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("parse remote url: %w", err)
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String(), nil
}

// gitDiff returns the unified diff of the branch against the configured base
// branch. Used by both verification and review.
func (m *AgentManager) gitDiff(ctx context.Context, branch string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", m.cfg.RepoPath, "diff", m.cfg.BaseBranch+"..."+branch)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

// runVerificationLoop is the verify-and-retry loop. Each iteration:
//  1. Reload card (so user edits to acceptance criteria are picked up).
//  2. Compute diff.
//  3. Score diff with VerifyAgentWork.
//  4. On AllPassed → return success.
//  5. On fail and iterations remaining → reinvoke coding agent with the
//     failed-criteria feedback, commit, push, status back to verifying.
//  6. On exhaustion → return the last result with AllPassed=false.
func (m *AgentManager) runVerificationLoop(ctx context.Context, run *agentRun) (VerificationResult, error) {
	cardID := run.cardID
	_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusVerifying)

	var last VerificationResult
	for iteration := 1; iteration <= m.cfg.MaxVerifyIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}

		card, err := m.cards.GetByID(ctx, cardID)
		if err != nil {
			return last, fmt.Errorf("reload card: %w", err)
		}

		// Empty-diff short-circuit: if the agent didn't change anything,
		// fail every criterion without spending a Claude call.
		diff, diffErr := m.gitDiff(ctx, run.branch)
		if diffErr == nil && strings.TrimSpace(diff) == "" {
			criteria := ParseAcceptanceCriteria(card.Description)
			last = VerificationResult{
				AllPassed: len(criteria) == 0,
				Verdicts:  makeAllFail(criteria, "No code changes found on the branch."),
				Summary:   "Agent produced no code changes.",
			}
			log.Printf("[board-agent] verification %d/%d for card %s: %s", iteration, m.cfg.MaxVerifyIterations, shortID(cardID), last.Summary)
			if last.AllPassed {
				return last, nil
			}
			if iteration == m.cfg.MaxVerifyIterations {
				return last, nil
			}
			feedback := formatFailedCriteriaFeedback(last.Verdicts)
			_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusRunning)
			if err := m.invokeCodingAgent(ctx, run, *card, feedback); err != nil {
				return last, fmt.Errorf("reinvoke after empty-diff: %w", err)
			}
			if err := m.commitAndPush(ctx, run, iteration); err != nil {
				return last, fmt.Errorf("commit+push after empty-diff retry: %w", err)
			}
			_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusVerifying)
			continue
		}

		result, err := verifyCard(ctx, m.code, *card, run.worktreeDir)
		if err != nil {
			return last, err
		}
		last = result
		log.Printf("[board-agent] verification %d/%d for card %s: %s", iteration, m.cfg.MaxVerifyIterations, shortID(cardID), result.Summary)

		if result.AllPassed {
			return result, nil
		}
		if iteration == m.cfg.MaxVerifyIterations {
			return result, nil
		}

		feedback := formatFailedCriteriaFeedback(result.Verdicts)
		_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusRunning)
		if err := m.invokeCodingAgent(ctx, run, *card, feedback); err != nil {
			return last, fmt.Errorf("reinvoke after verification failure: %w", err)
		}
		if err := m.commitAndPush(ctx, run, iteration); err != nil {
			return last, fmt.Errorf("commit+push after verification retry: %w", err)
		}
		_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusVerifying)
	}
	return last, nil
}

// runReviewLoop is the LLM code-review retry loop. APPROVE → return; on
// REQUEST_CHANGES we reinvoke the coding agent with the suggestions and
// re-run verification. Verification failure during a review cycle terminates
// the loop with an error (we don't pretend to ship code that no longer
// satisfies the criteria).
func (m *AgentManager) runReviewLoop(ctx context.Context, run *agentRun) (ReviewResult, error) {
	cardID := run.cardID
	_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusReviewing)

	var last ReviewResult
	for cycle := 1; cycle <= m.cfg.MaxReviewCycles; cycle++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}

		card, err := m.cards.GetByID(ctx, cardID)
		if err != nil {
			return last, fmt.Errorf("reload card: %w", err)
		}

		review, err := ReviewCard(ctx, m.code, *card, run.worktreeDir)
		if err != nil {
			return last, err
		}
		last = review
		log.Printf("[board-agent] review %d/%d for card %s: %s", cycle, m.cfg.MaxReviewCycles, shortID(cardID), review.Verdict)
		_ = m.cards.SetReviewStatus(ctx, cardID, string(review.Verdict))

		if review.Verdict == ReviewApprove {
			return review, nil
		}
		if cycle == m.cfg.MaxReviewCycles {
			return review, nil
		}

		feedback := formatReviewFeedback(review)
		_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusRunning)
		if err := m.invokeCodingAgent(ctx, run, *card, feedback); err != nil {
			return last, fmt.Errorf("reinvoke after review changes: %w", err)
		}
		if err := m.commitAndPush(ctx, run, cycle); err != nil {
			return last, fmt.Errorf("commit+push after review retry: %w", err)
		}

		// Re-verify before the next review cycle. A reinvocation that
		// breaks acceptance criteria must not slip past review.
		_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusVerifying)
		verifyResult, err := m.runVerificationLoop(ctx, run)
		if err != nil {
			return last, fmt.Errorf("re-verify during review cycle %d: %w", cycle, err)
		}
		if !verifyResult.AllPassed {
			return last, fmt.Errorf("re-verification failed during review cycle %d", cycle)
		}
		_ = m.cards.SetAgentStatus(ctx, cardID, AgentStatusReviewing)
	}
	return last, nil
}

// openPullRequest creates the GitHub PR, persists number+url on the card,
// moves the card to ColumnPR, and records the review status.
func (m *AgentManager) openPullRequest(ctx context.Context, card Card, run *agentRun, criteria []CriterionResult, review ReviewResult) error {
	if m.gh == nil {
		return errors.New("github client is not configured")
	}
	body := buildPRBody(card, criteria, review)
	pr, err := m.gh.CreatePR(ctx, m.cfg.RepoOwner, m.cfg.RepoName, github.CreatePRRequest{
		Head:  run.branch,
		Base:  m.cfg.BaseBranch,
		Title: card.Title,
		Body:  body,
	})
	if err != nil {
		return fmt.Errorf("create PR: %w", err)
	}

	if err := m.cards.SetPR(ctx, card.ID, pr.Number, pr.HTMLURL); err != nil {
		return fmt.Errorf("persist PR: %w", err)
	}
	if _, err := m.cards.Move(ctx, card.UserID, card.ID, ColumnPR, 0); err != nil {
		return fmt.Errorf("move card to %s column: %w", ColumnPR, err)
	}
	_ = m.cards.SetReviewStatus(ctx, card.ID, string(ReviewApprove))
	return nil
}

// isCancelled is true when err arose from ctx cancellation rather than a
// real failure. We treat cancellation as graceful shutdown so StopAgent
// doesn't leave cards stuck in `failed`.
func isCancelled(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx != nil && (errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return true
	}
	return false
}

// Compile-time assertion that codegen.Agent matches the interface we use.
// Cheap insurance against future signature drift.
var _ codegen.Agent = (codegen.Agent)(nil)
