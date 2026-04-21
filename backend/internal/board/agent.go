// Package board's agent manager owns the autonomous build-verify-review-ship
// loop. The loop is the crown jewel of Eva Board: when a card lands in
// `develop`, the manager spins up a goroutine that creates a per-card git
// worktree, runs a pluggable coding-agent CLI to implement the change,
// verifies the resulting diff against the card's acceptance criteria, asks
// an LLM reviewer to gate code quality, and finally opens a pull request and
// moves the card to the `pr` column.
//
// Concurrency model: each in-flight card has exactly one *agentRun. The
// manager guards the cardID -> *agentRun map with a mutex. StartAgent is
// idempotent (returns nil if a run for that card is already active). The
// run's context is cancelled on StopAgent so the running coding agent and
// any in-flight LLM call are unwound promptly. SubmitFeedback enqueues a
// string that the next reinvocation will inject into the agent prompt.
package board

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"sync"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/google/uuid"
)

// AgentConfig is the static configuration the manager needs to drive a single
// repository. Per-card state (branch name, worktree path, feedback queue)
// lives on agentRun.
type AgentConfig struct {
	// RepoOwner is the GitHub org/user that owns RepoName.
	RepoOwner string
	// RepoName is the GitHub repository name.
	RepoName string
	// RepoPath is the local checkout the manager will create per-card
	// worktrees from. Worktrees land at <RepoPath>/../worktrees/<short-id>.
	RepoPath string
	// BranchPrefix is prepended to the short card ID to form the branch
	// name. Defaults to "eva-board/" when empty.
	BranchPrefix string
	// BaseBranch is the branch PRs target and worktrees are created from.
	// Defaults to "main" when empty.
	BaseBranch string
	// MaxVerifyIterations caps how many times the manager will re-invoke
	// the coding agent with failed-criteria feedback before giving up.
	// Defaults to 3 when zero.
	MaxVerifyIterations int
	// MaxReviewCycles caps the reviewer retry loop. Defaults to 5.
	MaxReviewCycles int
	// GitHubToken is used for `git push` over HTTPS when set. The github
	// client receives its own token via Options.Token at construction.
	GitHubToken string
}

// AgentManager runs the autonomous loop for cards. The zero value is not
// usable; construct via NewAgentManager.
type AgentManager struct {
	cards cardStore
	code  codegen.Agent
	gh    github.Client
	cfg   AgentConfig

	mu   sync.Mutex
	runs map[uuid.UUID]*agentRun
}

// agentRun is the per-card state for one in-flight agent loop. It is owned by
// a single goroutine spawned in StartAgent; the manager only reads/clears it
// from the runs map under m.mu.
type agentRun struct {
	cardID uuid.UUID
	cancel context.CancelFunc

	branch      string
	worktreeDir string

	// fb is a per-card feedback queue. SubmitFeedback appends; the runner
	// drains it under fbMu before each agent reinvocation.
	fbMu sync.Mutex
	fb   []string
}

// NewAgentManager builds a manager and applies AgentConfig defaults. It does
// NOT verify that the coding-agent CLI or git is available — startup checks
// happen lazily inside StartAgent so a missing CLI fails loudly per-card
// instead of silently disabling the whole feature.
func NewAgentManager(cards cardStore, code codegen.Agent, gh github.Client, cfg AgentConfig) *AgentManager {
	if cfg.BranchPrefix == "" {
		cfg.BranchPrefix = "eva-board/"
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.MaxVerifyIterations <= 0 {
		cfg.MaxVerifyIterations = 3
	}
	if cfg.MaxReviewCycles <= 0 {
		cfg.MaxReviewCycles = 5
	}
	return &AgentManager{
		cards: cards,
		code:  code,
		gh:    gh,
		cfg:   cfg,
		runs:  make(map[uuid.UUID]*agentRun),
	}
}

// ErrGitUnavailable is returned by StartAgent when the `git` binary cannot be
// found on PATH. The loop is unrunnable without it.
var ErrGitUnavailable = errors.New("agent: git binary not found on PATH")

// StartAgent kicks off the autonomous loop for a card. It is idempotent —
// calling StartAgent for a card whose loop is already running returns nil
// and is a no-op, matching the issue spec.
//
// The supplied ctx is only used for synchronous setup (loading the card,
// preparing the worktree). The actual loop runs under a fresh background
// context owned by the run so the loop survives the caller's request
// lifetime. StopAgent is the only way to cancel a started loop.
func (m *AgentManager) StartAgent(ctx context.Context, cardID uuid.UUID) error {
	if m == nil {
		return errors.New("agent manager is nil")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitUnavailable
	}

	m.mu.Lock()
	if _, running := m.runs[cardID]; running {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &agentRun{
		cardID: cardID,
		cancel: cancel,
	}
	m.runs[cardID] = run
	m.mu.Unlock()

	go func() {
		defer m.removeRun(cardID)
		if err := m.runAgent(runCtx, run); err != nil {
			log.Printf("[board-agent] run for card %s exited: %v", shortID(cardID), err)
		}
	}()
	return nil
}

// StopAgent cancels the run goroutine for the given card. Idempotent.
func (m *AgentManager) StopAgent(cardID uuid.UUID) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	run, ok := m.runs[cardID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if run.cancel != nil {
		run.cancel()
	}
	return nil
}

// SubmitFeedback enqueues feedback that will be appended to the agent's next
// invocation prompt. It is safe to call regardless of whether the loop is
// currently running; queued feedback is dropped when the loop terminates.
//
// If the agent is currently inside a Run() call when feedback arrives, it
// will only be picked up on the next invocation (verification retry, review
// retry, or future StartAgent for the same card).
func (m *AgentManager) SubmitFeedback(cardID uuid.UUID, feedback string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	run, ok := m.runs[cardID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	run.fbMu.Lock()
	run.fb = append(run.fb, feedback)
	run.fbMu.Unlock()
	return nil
}

// StopAll cancels every in-flight agent run owned by this manager.
// Used by AgentRegistry when a stale manager is being evicted from the
// cache so its goroutines unwind promptly instead of leaking.
func (m *AgentManager) StopAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.runs))
	for _, run := range m.runs {
		if run.cancel != nil {
			cancels = append(cancels, run.cancel)
		}
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// IsRunning reports whether an agent loop is active for the given card.
func (m *AgentManager) IsRunning(cardID uuid.UUID) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[cardID]
	return ok
}

// drainFeedback removes and concatenates queued feedback for this run.
func (r *agentRun) drainFeedback() string {
	r.fbMu.Lock()
	defer r.fbMu.Unlock()
	if len(r.fb) == 0 {
		return ""
	}
	out := ""
	for i, fb := range r.fb {
		if i > 0 {
			out += "\n\n---\n\n"
		}
		out += fb
	}
	r.fb = r.fb[:0]
	return out
}

func (m *AgentManager) removeRun(cardID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, cardID)
}

func shortID(id uuid.UUID) string {
	s := id.String()
	if len(s) < 8 {
		return s
	}
	return s[:8]
}
