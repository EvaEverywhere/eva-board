package board

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

func TestParseTriageProposals_Valid(t *testing.T) {
	cardID := uuid.New()
	backlog := []Card{{ID: cardID, Title: "old"}}

	raw := "```json\n" + `{
  "proposals": [
    {"type": "create", "title": "Add retry logic", "description": "body", "acceptance_criteria": ["covered by tests", ""], "reason": "missing"},
    {"type": "close", "card_id": "` + cardID.String() + `", "reason": "done"},
    {"type": "rewrite", "card_id": "` + cardID.String() + `", "title": "New title", "reason": "vague"},
    {"type": "create", "title": "", "reason": "should be filtered (empty title)"},
    {"type": "close", "card_id": "` + uuid.NewString() + `", "reason": "unknown card filtered"},
    {"type": "bogus", "title": "ignore me"}
  ]
}` + "\n```"

	got, err := parseTriageProposals(raw, backlog)
	if err != nil {
		t.Fatalf("parseTriageProposals: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 valid proposals, got %d: %+v", len(got), got)
	}
	if got[0].Type != TriageProposalCreate || got[0].Title != "Add retry logic" {
		t.Errorf("create proposal mismatch: %+v", got[0])
	}
	if len(got[0].AcceptanceCriteria) != 1 || got[0].AcceptanceCriteria[0] != "covered by tests" {
		t.Errorf("acceptance criteria not trimmed: %+v", got[0].AcceptanceCriteria)
	}
	if got[1].Type != TriageProposalClose || got[1].CardID == nil || *got[1].CardID != cardID {
		t.Errorf("close proposal mismatch: %+v", got[1])
	}
	if got[2].Type != TriageProposalRewrite || got[2].CardID == nil || *got[2].CardID != cardID {
		t.Errorf("rewrite proposal mismatch: %+v", got[2])
	}
}

func TestParseTriageProposals_InvalidJSON(t *testing.T) {
	if _, err := parseTriageProposals("not json", nil); err == nil {
		t.Fatal("expected error on invalid json")
	}
}

func TestParseTriageProposals_EmptyOK(t *testing.T) {
	got, err := parseTriageProposals("", nil)
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty input should yield no proposals, got %d", len(got))
	}
}

func TestBuildTriagePrompt_IncludesCardsAndIssues(t *testing.T) {
	id := uuid.New()
	cards := []Card{{ID: id, Title: "Hello", Description: "World"}}
	issues := []github.Issue{{Number: 42, Title: "Repo issue", Body: "body", UpdatedAt: time.Now()}}

	got, err := buildTriagePrompt(cards, issues)
	if err != nil {
		t.Fatalf("buildTriagePrompt: %v", err)
	}
	if !strings.Contains(got, id.String()) {
		t.Error("prompt missing card id")
	}
	if !strings.Contains(got, "Hello") {
		t.Error("prompt missing card title")
	}
	if !strings.Contains(got, `"number": 42`) {
		t.Error("prompt missing repo issue number")
	}
	if !strings.Contains(got, "Repo issue") {
		t.Error("prompt missing repo issue title")
	}
	if !strings.Contains(got, "Current backlog cards") || !strings.Contains(got, "Repo open GitHub issues") {
		t.Error("prompt missing section headers")
	}
}

func TestComposeDescription(t *testing.T) {
	got := composeDescription("body", []string{"a", "", "b"})
	want := "body\n\n## Acceptance Criteria\n- [ ] a\n- [ ] b"
	if got != want {
		t.Fatalf("composeDescription mismatch:\n got:  %q\n want: %q", got, want)
	}
	if got := composeDescription("", nil); got != "" {
		t.Fatalf("empty inputs should yield empty string, got %q", got)
	}
}
