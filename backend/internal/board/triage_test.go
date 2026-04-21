package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// helper to drive the wire decoding the way the live AnalyzeBacklog
// path does, then run the same filter step.
func decodeAndFilter(t *testing.T, raw string, backlog []Card) []TriageProposal {
	t.Helper()
	var wire triageWire
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatalf("unmarshal triage wire: %v", err)
	}
	return filterTriageProposals(wire, backlog)
}

func TestFilterTriageProposals_Valid(t *testing.T) {
	cardID := uuid.New()
	backlog := []Card{{ID: cardID, Title: "old"}}

	raw := `{
  "proposals": [
    {"type": "create", "title": "Add retry logic", "description": "body", "acceptance_criteria": ["covered by tests", ""], "reason": "missing"},
    {"type": "close", "card_id": "` + cardID.String() + `", "reason": "done"},
    {"type": "rewrite", "card_id": "` + cardID.String() + `", "title": "New title", "reason": "vague"},
    {"type": "create", "title": "", "reason": "should be filtered (empty title)"},
    {"type": "close", "card_id": "` + uuid.NewString() + `", "reason": "unknown card filtered"},
    {"type": "bogus", "title": "ignore me"}
  ]
}`

	got := decodeAndFilter(t, raw, backlog)
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

func TestBuildTriagePrompt_IncludesCardsAndIssues(t *testing.T) {
	id := uuid.New()
	cards := []Card{{ID: id, Title: "Hello", Description: "World"}}
	issues := []github.Issue{{Number: 42, Title: "Repo issue", Body: "body"}}

	got := buildTriagePrompt(cards, issues)
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
	if !strings.Contains(got, "senior engineer triaging") {
		t.Error("prompt missing reviewer framing — fakeCodegen reviewer detection relies on this")
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

// TestTriageService_AnalyzeBacklog_EndToEnd exercises the full path
// from prompt through codegen to filtered proposals.
func TestTriageService_AnalyzeBacklog_EndToEnd(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeBacklogCard(store, userID)

	resp := `{"proposals":[{"type":"rewrite","card_id":"` + card.ID.String() + `","title":"Better","reason":"vague"}]}`
	fc := &fakeCodegen{reviewerOutputs: []string{resp}}
	svc := NewTriageService(store, fc, TriageConfig{})

	got, err := svc.AnalyzeBacklog(context.Background(), userID)
	if err != nil {
		t.Fatalf("AnalyzeBacklog: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(got))
	}
	if got[0].Type != TriageProposalRewrite {
		t.Errorf("type = %s, want rewrite", got[0].Type)
	}
}
