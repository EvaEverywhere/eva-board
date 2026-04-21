package board

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// initTestRepo creates a bare-bones git repo that the AgentManager can
// safely operate on: a real local origin (a second clone) so
// `git push --force origin <branch>` from a worktree succeeds without
// touching the network. Returns the working clone path and an
// "origin URL" pointing at the bare repo. Skips the test when git is
// not on PATH.
func initTestRepo(t *testing.T) (workPath, originURL string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available on PATH; skipping agent state-machine tests")
	}
	root := t.TempDir()

	// Bare repo serves as the push target.
	originDir := filepath.Join(root, "origin.git")
	mustGit(t, root, "init", "--bare", "-b", "main", originDir)

	// Working clone with one initial commit so `main` exists.
	workDir := filepath.Join(root, "work")
	mustGit(t, root, "clone", originDir, workDir)
	mustGit(t, workDir, "config", "user.email", "agent@test.local")
	mustGit(t, workDir, "config", "user.name", "Agent Test")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	mustGit(t, workDir, "add", "README.md")
	mustGit(t, workDir, "commit", "-m", "initial")
	mustGit(t, workDir, "branch", "-M", "main")
	mustGit(t, workDir, "push", "-u", "origin", "main")

	return workDir, originDir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed in %s: %v\n%s", args, dir, err, out)
	}
}

// makeAgentCard builds a card with one or more checkbox criteria so
// the verification loop has something to evaluate.
func makeAgentCard() Card {
	return Card{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		Title:       "Add greeting",
		Description: "Add a hello-world file.\n\n## Acceptance Criteria\n- [ ] file exists\n",
		Column:      ColumnDevelop,
		AgentStatus: AgentStatusIdle,
		Metadata:    map[string]any{},
	}
}

// touchHelloFile is a default fakeCodegen.touchFile helper that drops a
// new file into the worktree so the diff is non-empty.
func touchHelloFile(workDir string, call int) error {
	return os.WriteFile(filepath.Join(workDir, fmt.Sprintf("hello-%d.txt", call)), []byte("hi\n"), 0o644)
}

// newAgentManagerForTest wires the manager with the configured fakes
// and a verify/review iteration count low enough to exhaust quickly.
func newAgentManagerForTest(
	t *testing.T,
	store *fakeCardStore,
	code *fakeCodegen,
	gh *fakeAgentGitHub,
	maxVerify, maxReview int,
	repoPath string,
) *AgentManager {
	t.Helper()
	return NewAgentManager(store, code, gh, AgentConfig{
		RepoOwner:           "owner",
		RepoName:            "repo",
		RepoPath:            repoPath,
		BranchPrefix:        "eva-board/",
		BaseBranch:          "main",
		MaxVerifyIterations: maxVerify,
		MaxReviewCycles:     maxReview,
	})
}

// waitForRunDone polls IsRunning until the goroutine clears its entry
// from the runs map (or the timeout elapses).
func waitForRunDone(t *testing.T, m *AgentManager, cardID uuid.UUID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !m.IsRunning(cardID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent run for card %s did not complete within %s", cardID, timeout)
}

const verifyAllPassedJSON = `{"verdicts":[{"criterion":"file exists","met":true,"reason":"hello-1.txt is present"}],"summary":"all good"}`
const verifyAllFailedJSON = `{"verdicts":[{"criterion":"file exists","met":false,"reason":"missing"}],"summary":"missing"}`
const reviewApproveJSON = `{"verdict":"APPROVE","summary":"lgtm","suggestions":[]}`
const reviewRequestChangesJSON = `{"verdict":"REQUEST_CHANGES","summary":"please tweak","suggestions":["use a constant"]}`

func TestAgentManager_HappyPath(t *testing.T) {
	workDir, _ := initTestRepo(t)
	store := newFakeCardStore()
	card := makeAgentCard()
	store.seed(card)

	code := &fakeCodegen{
		touchFile: touchHelloFile,
		reviewerOutputs: []string{
			verifyAllPassedJSON,
			reviewApproveJSON,
		},
	}
	gh := &fakeAgentGitHub{prNumber: 7, prHTMLURL: "https://example.com/pr/7"}
	m := newAgentManagerForTest(t, store, code, gh, 3, 3, workDir)

	if err := m.StartAgent(context.Background(), card.ID); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	waitForRunDone(t, m, card.ID, 15*time.Second)

	final := store.Snapshot(card.ID)
	if final == nil {
		t.Fatal("card disappeared from store")
	}
	if final.AgentStatus != AgentStatusSucceeded {
		t.Fatalf("final status = %q, want succeeded; history=%v", final.AgentStatus, store.Statuses())
	}
	if final.Column != ColumnPR {
		t.Fatalf("final column = %q, want pr", final.Column)
	}
	if final.PRNumber == nil || *final.PRNumber != 7 {
		t.Fatalf("PR number not persisted: %+v", final.PRNumber)
	}
	if gh.createCalls != 1 {
		t.Fatalf("expected 1 CreatePR call, got %d", gh.createCalls)
	}
	// 1 implementer call + 1 verify reviewer + 1 review reviewer = 3.
	if code.Calls() != 3 {
		t.Fatalf("expected 3 codegen calls (1 implementer + verify + review), got %d", code.Calls())
	}

	// The status history must include verifying and reviewing on the
	// way to succeeded — otherwise we shipped without gating.
	statuses := store.Statuses()
	wantPhases := []string{AgentStatusRunning, AgentStatusVerifying, AgentStatusReviewing, AgentStatusSucceeded}
	for _, want := range wantPhases {
		if !containsString(statuses, want) {
			t.Errorf("status history missing %q: %v", want, statuses)
		}
	}
}

func TestAgentManager_VerificationRetryExhausted(t *testing.T) {
	workDir, _ := initTestRepo(t)
	store := newFakeCardStore()
	card := makeAgentCard()
	store.seed(card)

	code := &fakeCodegen{
		touchFile: touchHelloFile,
		// Always return all-failed so every iteration retries until the
		// loop gives up.
		reviewerOutputs: []string{
			verifyAllFailedJSON,
			verifyAllFailedJSON,
		},
	}
	gh := &fakeAgentGitHub{}
	m := newAgentManagerForTest(t, store, code, gh, 2, 5, workDir)

	if err := m.StartAgent(context.Background(), card.ID); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	waitForRunDone(t, m, card.ID, 15*time.Second)

	final := store.Snapshot(card.ID)
	if final.AgentStatus != AgentStatusFailed {
		t.Fatalf("final status = %q, want failed; history=%v", final.AgentStatus, store.Statuses())
	}
	if gh.createCalls != 0 {
		t.Fatalf("PR must not be created when verification fails (got %d calls)", gh.createCalls)
	}
	// Implementer runs once initially + once per failed-then-retry loop.
	// With MaxVerifyIterations=2 we expect: 1 initial + 1 retry + 2 verify
	// reviewer calls = 4.
	if code.Calls() < 4 {
		t.Fatalf("expected at least 4 codegen calls (initial + retry + 2 verifies), got %d", code.Calls())
	}
}

func TestAgentManager_ReviewRetryExhausted(t *testing.T) {
	workDir, _ := initTestRepo(t)
	store := newFakeCardStore()
	card := makeAgentCard()
	store.seed(card)

	code := &fakeCodegen{
		touchFile: touchHelloFile,
		// Verification must keep passing so review is reached, but review
		// always REQUEST_CHANGES so the cycle exhausts. The review loop
		// re-verifies after each REQUEST_CHANGES, so the response order is
		// verify, review, verify (re-verify after request_changes), review
		// (final cycle).
		reviewerOutputs: []string{
			// initial verification phase
			verifyAllPassedJSON,
			// review cycle 1 → REQUEST_CHANGES
			reviewRequestChangesJSON,
			// re-verification after REQUEST_CHANGES (still passes so we
			// proceed to next review cycle)
			verifyAllPassedJSON,
			// review cycle 2 → REQUEST_CHANGES → exhausts MaxReviewCycles=2
			reviewRequestChangesJSON,
		},
	}
	gh := &fakeAgentGitHub{}
	m := newAgentManagerForTest(t, store, code, gh, 3, 2, workDir)

	if err := m.StartAgent(context.Background(), card.ID); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	waitForRunDone(t, m, card.ID, 15*time.Second)

	final := store.Snapshot(card.ID)
	if final.AgentStatus != AgentStatusFailed {
		t.Fatalf("final status = %q, want failed; history=%v", final.AgentStatus, store.Statuses())
	}
	if gh.createCalls != 0 {
		t.Fatalf("PR must not be created when review never approves (got %d calls)", gh.createCalls)
	}
	if final.ReviewStatus == nil || *final.ReviewStatus != string(ReviewRequestChanges) {
		t.Fatalf("expected last persisted review status = REQUEST_CHANGES, got %+v", final.ReviewStatus)
	}
}

func TestAgentManager_StopMidRun(t *testing.T) {
	workDir, _ := initTestRepo(t)
	store := newFakeCardStore()
	card := makeAgentCard()
	store.seed(card)

	// fakeCodegen blocks inside Run until either release is closed or
	// the run's context is cancelled. We cancel via StopAgent so the
	// loop exits via its isCancelled() branch (status → idle, never
	// failed — cancellation is graceful shutdown).
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	code := &fakeCodegen{
		blockUntil: release,
		started:    started,
		touchFile:  touchHelloFile,
	}
	gh := &fakeAgentGitHub{}
	m := newAgentManagerForTest(t, store, code, gh, 3, 3, workDir)

	if err := m.StartAgent(context.Background(), card.ID); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("codegen never started; cannot test mid-run stop")
	}

	if err := m.StopAgent(card.ID); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	waitForRunDone(t, m, card.ID, 15*time.Second)

	final := store.Snapshot(card.ID)
	// Cancellation is treated as graceful shutdown — status should
	// land back at idle, never failed (it would be misleading to mark
	// a user-initiated stop as a failure).
	if final.AgentStatus != AgentStatusIdle {
		t.Fatalf("after StopAgent expected status=idle, got %q (history=%v)",
			final.AgentStatus, store.Statuses())
	}
	if gh.createCalls != 0 {
		t.Fatalf("PR must not be created when run is cancelled mid-flight")
	}
}

func TestAgentManager_GitHubFailureSetsFailedStatus(t *testing.T) {
	workDir, _ := initTestRepo(t)
	store := newFakeCardStore()
	card := makeAgentCard()
	store.seed(card)

	code := &fakeCodegen{
		touchFile: touchHelloFile,
		reviewerOutputs: []string{
			verifyAllPassedJSON,
			reviewApproveJSON,
		},
	}
	gh := &fakeAgentGitHub{createErr: errors.New("github 502: upstream broke")}
	m := newAgentManagerForTest(t, store, code, gh, 3, 3, workDir)

	if err := m.StartAgent(context.Background(), card.ID); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	waitForRunDone(t, m, card.ID, 15*time.Second)

	final := store.Snapshot(card.ID)
	if final.AgentStatus != AgentStatusFailed {
		t.Fatalf("final status = %q, want failed when CreatePR errors; history=%v",
			final.AgentStatus, store.Statuses())
	}
	if gh.createCalls != 1 {
		t.Fatalf("expected exactly 1 CreatePR attempt, got %d", gh.createCalls)
	}
	// Card must NOT have moved to the PR column on a failed PR
	// creation — that would be silent data corruption.
	if final.Column == ColumnPR {
		t.Fatalf("card was moved to %s despite PR creation failing", ColumnPR)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
