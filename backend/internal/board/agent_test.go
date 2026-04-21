package board

import (
	"context"
	"strings"
	"testing"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/google/uuid"
)

// AgentManager unit tests cover the public surface that does NOT touch git
// or the LLM (idempotency, feedback queue, status reporting). The end-to-end
// loop is covered by integration tests in #20.

type noopAgent struct{}

func (noopAgent) Name() string { return "noop" }
func (noopAgent) Run(_ context.Context, _ string, _ string, _ ...codegen.RunOption) (codegen.Result, error) {
	return codegen.Result{}, nil
}

func newTestManager(t *testing.T) *AgentManager {
	t.Helper()
	m := NewAgentManager(nil, noopAgent{}, nil, nil, AgentConfig{LLMModel: "test-model"})
	if m == nil {
		t.Fatal("NewAgentManager returned nil")
	}
	return m
}

func TestAgentConfigDefaults(t *testing.T) {
	m := NewAgentManager(nil, noopAgent{}, nil, nil, AgentConfig{LLMModel: "m"})
	if m.cfg.BranchPrefix != "eva-board/" {
		t.Errorf("BranchPrefix default = %q, want eva-board/", m.cfg.BranchPrefix)
	}
	if m.cfg.BaseBranch != "main" {
		t.Errorf("BaseBranch default = %q, want main", m.cfg.BaseBranch)
	}
	if m.cfg.MaxVerifyIterations != 3 {
		t.Errorf("MaxVerifyIterations default = %d, want 3", m.cfg.MaxVerifyIterations)
	}
	if m.cfg.MaxReviewCycles != 5 {
		t.Errorf("MaxReviewCycles default = %d, want 5", m.cfg.MaxReviewCycles)
	}
}

func TestAgentConfig_RespectsExplicitValues(t *testing.T) {
	m := NewAgentManager(nil, noopAgent{}, nil, nil, AgentConfig{
		BranchPrefix:        "team/",
		BaseBranch:          "develop",
		MaxVerifyIterations: 7,
		MaxReviewCycles:     2,
		LLMModel:            "m",
	})
	if m.cfg.BranchPrefix != "team/" || m.cfg.BaseBranch != "develop" {
		t.Fatalf("config not preserved: %+v", m.cfg)
	}
	if m.cfg.MaxVerifyIterations != 7 || m.cfg.MaxReviewCycles != 2 {
		t.Fatalf("loop caps not preserved: %+v", m.cfg)
	}
}

func TestIsRunning_NilManager(t *testing.T) {
	var m *AgentManager
	if m.IsRunning(uuid.New()) {
		t.Fatal("nil manager should not report running")
	}
}

func TestStopAgent_NilManagerNoop(t *testing.T) {
	var m *AgentManager
	if err := m.StopAgent(uuid.New()); err != nil {
		t.Fatalf("nil manager StopAgent should be noop, got %v", err)
	}
}

func TestSubmitFeedback_NilManagerNoop(t *testing.T) {
	var m *AgentManager
	if err := m.SubmitFeedback(uuid.New(), "hi"); err != nil {
		t.Fatalf("nil manager SubmitFeedback should be noop, got %v", err)
	}
}

func TestSubmitFeedback_UnknownCardIsNoop(t *testing.T) {
	m := newTestManager(t)
	if err := m.SubmitFeedback(uuid.New(), "ignored"); err != nil {
		t.Fatalf("expected noop, got error: %v", err)
	}
}

func TestStopAgent_UnknownCardIsNoop(t *testing.T) {
	m := newTestManager(t)
	if err := m.StopAgent(uuid.New()); err != nil {
		t.Fatalf("expected noop, got error: %v", err)
	}
}

func TestFeedbackQueue_DrainConcatenatesAndClears(t *testing.T) {
	run := &agentRun{cardID: uuid.New()}
	if got := run.drainFeedback(); got != "" {
		t.Fatalf("empty queue should drain to empty, got %q", got)
	}
	run.fb = []string{"first item", "second item", "third"}
	got := run.drainFeedback()
	if !strings.Contains(got, "first item") || !strings.Contains(got, "second item") || !strings.Contains(got, "third") {
		t.Fatalf("drain missing items: %q", got)
	}
	if !strings.Contains(got, "---") {
		t.Fatalf("drain should join with separator, got %q", got)
	}
	if len(run.fb) != 0 {
		t.Fatalf("queue should be empty after drain, got %d items", len(run.fb))
	}
	if got := run.drainFeedback(); got != "" {
		t.Fatalf("second drain should be empty, got %q", got)
	}
}

func TestShortID(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	if got := shortID(id); got != "12345678" {
		t.Fatalf("shortID = %q, want 12345678", got)
	}
}

func TestBuildAgentPrompt_IncludesFeedback(t *testing.T) {
	card := Card{Title: "T", Description: "D"}
	got := buildAgentPrompt(card, "please fix the bug")
	if !strings.Contains(got, "## Issue: T") {
		t.Errorf("prompt missing title: %s", got)
	}
	if !strings.Contains(got, "## Description") || !strings.Contains(got, "D") {
		t.Errorf("prompt missing description: %s", got)
	}
	if !strings.Contains(got, "## Review Feedback") || !strings.Contains(got, "please fix the bug") {
		t.Errorf("prompt missing feedback section: %s", got)
	}
}

func TestBuildAgentPrompt_OmitsEmptyFeedback(t *testing.T) {
	got := buildAgentPrompt(Card{Title: "T"}, "")
	if strings.Contains(got, "Review Feedback") {
		t.Errorf("empty feedback should not produce a feedback section: %s", got)
	}
}

func TestBuildPRBody_IncludesCriteriaAndReview(t *testing.T) {
	card := Card{Title: "Add widget", Description: "Build a widget."}
	criteria := []CriterionResult{
		{Criterion: "Has a button", Met: true, Reason: "wired up"},
		{Criterion: "Has a test", Met: false, Reason: "missing _test.go"},
	}
	review := ReviewResult{
		Verdict:     ReviewApprove,
		Summary:     "Looks great.",
		Suggestions: []string{"consider perf later"},
	}
	got := buildPRBody(card, criteria, review)
	for _, want := range []string{
		"## Add widget",
		"Build a widget.",
		"### Acceptance Criteria",
		"[x] **Has a button**",
		"[ ] **Has a test**",
		"### Review Summary",
		"Looks great.",
		"### Reviewer Suggestions",
		"consider perf later",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PR body missing %q, got:\n%s", want, got)
		}
	}
}
