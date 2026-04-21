package board

import (
	"context"
	"testing"
	"time"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

type stubGH struct {
	github.Client
	issues []github.Issue
	closed []int
	err    error
}

func (s *stubGH) ListIssues(ctx context.Context, owner, repo string, opts github.ListIssuesOptions) ([]github.Issue, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.issues, nil
}

func (s *stubGH) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	s.closed = append(s.closed, number)
	return nil
}

func TestClassifyDeadIssue_ByLabel(t *testing.T) {
	iss := github.Issue{Number: 1, Labels: []github.Label{{Name: "wontfix"}}, UpdatedAt: time.Now()}
	reason, dead := classifyDeadIssue(iss, time.Now().Add(-365*24*time.Hour))
	if !dead {
		t.Fatal("expected dead by label")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestClassifyDeadIssue_ByAge(t *testing.T) {
	old := time.Now().Add(-200 * 24 * time.Hour)
	iss := github.Issue{Number: 2, UpdatedAt: old}
	cutoff := time.Now().Add(-90 * 24 * time.Hour)
	if _, dead := classifyDeadIssue(iss, cutoff); !dead {
		t.Fatal("expected stale issue to be classified dead")
	}

	fresh := github.Issue{Number: 3, UpdatedAt: time.Now()}
	if _, dead := classifyDeadIssue(fresh, cutoff); dead {
		t.Fatal("fresh issue should not be dead")
	}
}

func TestParseIssueTarget(t *testing.T) {
	owner, repo, num, err := parseIssueTarget("acme/widgets#42")
	if err != nil {
		t.Fatalf("parseIssueTarget: %v", err)
	}
	if owner != "acme" || repo != "widgets" || num != 42 {
		t.Fatalf("parseIssueTarget mismatch: %s %s %d", owner, repo, num)
	}

	for _, bad := range []string{"acme/widgets", "acme#1", "acme/widgets#", "acme/widgets#abc", "/widgets#1", ""} {
		if _, _, _, err := parseIssueTarget(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestIsProtectedBranch(t *testing.T) {
	if !isProtectedBranch("main", "main") {
		t.Error("main must be protected")
	}
	if !isProtectedBranch("MASTER", "main") {
		t.Error("master must be protected case-insensitive")
	}
	if !isProtectedBranch("trunk", "trunk") {
		t.Error("configured default branch must be protected")
	}
	if isProtectedBranch("eva-board/feature-x", "main") {
		t.Error("feature branch must not be protected")
	}
}

func TestSpringCleanService_AuditRepo_DeadIssuesOnly(t *testing.T) {
	stub := &stubGH{
		issues: []github.Issue{
			{Number: 10, Labels: []github.Label{{Name: "stale"}}, UpdatedAt: time.Now()},
			{Number: 11, UpdatedAt: time.Now()}, // fresh, not dead
		},
	}
	svc := NewSpringCleanService(stub, SpringCleanConfig{
		RepoOwner: "acme",
		RepoName:  "widgets",
	})

	got, err := svc.AuditRepo(context.Background())
	if err != nil {
		t.Fatalf("AuditRepo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cleanup action, got %d (%+v)", len(got), got)
	}
	if got[0].Type != CleanupCloseIssue || got[0].Target != "acme/widgets#10" {
		t.Errorf("unexpected action: %+v", got[0])
	}
}

func TestSpringCleanService_ApplyActions_GuardsProtectedBranch(t *testing.T) {
	svc := NewSpringCleanService(nil, SpringCleanConfig{RepoPath: "/tmp", BranchPrefix: "eva-board/", DefaultBranch: "main"})
	err := svc.ApplyActions(context.Background(), []CleanupAction{{Type: CleanupDeleteBranch, Target: "main"}})
	if err == nil {
		t.Fatal("expected refusal to delete main")
	}
}

func TestSpringCleanService_ApplyActions_CloseIssue(t *testing.T) {
	stub := &stubGH{}
	svc := NewSpringCleanService(stub, SpringCleanConfig{RepoOwner: "acme", RepoName: "widgets"})
	err := svc.ApplyActions(context.Background(), []CleanupAction{{Type: CleanupCloseIssue, Target: "acme/widgets#5"}})
	if err != nil {
		t.Fatalf("ApplyActions: %v", err)
	}
	if len(stub.closed) != 1 || stub.closed[0] != 5 {
		t.Fatalf("expected issue 5 closed, got %v", stub.closed)
	}
}
