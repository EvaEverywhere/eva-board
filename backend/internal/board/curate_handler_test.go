package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestTriageService_ApplyProposals_OnlyAppliesPassedSubset verifies the
// service applies the exact proposal slice it is handed — never the
// full analyzer output. This is the contract the HTTP /apply endpoint
// relies on so the user can approve a subset.
func TestTriageService_ApplyProposals_OnlyAppliesPassedSubset(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()

	// Seed three cards: one to keep, one to close, one to rewrite.
	keep := makeBacklogCard(store, userID)
	toClose, _ := store.Create(context.Background(), userID, uuid.Nil, CreateRequest{Title: "stale"})
	toRewrite, _ := store.Create(context.Background(), userID, uuid.Nil, CreateRequest{Title: "vague"})

	// Analyzer "produced" three proposals; user approves only two
	// (close + rewrite). The create proposal in `denied` must NOT be
	// applied — that's the whole reason ApplyProposals takes a slice.
	approved := []TriageProposal{
		{Type: TriageProposalClose, CardID: &toClose.ID, Reason: "obsolete"},
		{Type: TriageProposalRewrite, CardID: &toRewrite.ID, Title: "Sharper title", Description: "body", AcceptanceCriteria: []string{"do the thing"}, Reason: "vague"},
	}
	denied := []TriageProposal{
		{Type: TriageProposalCreate, Title: "Phantom card", Reason: "not approved"},
	}

	svc := NewTriageService(store, &fakeCodegen{}, TriageConfig{})
	if err := svc.ApplyProposals(context.Background(), userID, approved); err != nil {
		t.Fatalf("ApplyProposals: %v", err)
	}

	// Approved close → card gone.
	if got, err := store.Get(context.Background(), userID, toClose.ID); err == nil {
		t.Fatalf("close proposal not applied; card still present: %+v", got)
	}
	// Approved rewrite → title updated.
	got, err := store.Get(context.Background(), userID, toRewrite.ID)
	if err != nil {
		t.Fatalf("rewrite target missing: %v", err)
	}
	if got.Title != "Sharper title" {
		t.Errorf("rewrite did not apply title; got %q", got.Title)
	}
	// Untouched card unchanged.
	if got, _ := store.Get(context.Background(), userID, keep.ID); got == nil || got.Title != "Some work" {
		t.Errorf("untouched card was mutated: %+v", got)
	}
	// Denied create must NOT have produced a card.
	all, _ := store.List(context.Background(), userID, uuid.Nil, "")
	for _, c := range all {
		if c.Title == denied[0].Title {
			t.Errorf("denied create proposal was applied: %+v", c)
		}
	}
}

// TestCurateService_Run_AggregatesBothPipelines covers the curate flow
// stitching: triage and spring-clean run in parallel and the result
// surfaces both lists. Spring clean's filesystem audit needs a repo
// path; the in-memory CurateService.Run exercise here uses real
// services with empty data so we exercise the aggregation/dedupe path
// rather than the LLM/git internals (those are covered elsewhere).
func TestCurateService_Run_AggregatesBothPipelines(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()

	// Triage agent returns one rewrite proposal.
	card := makeBacklogCard(store, userID)
	triageJSON := `{"proposals":[{"type":"rewrite","card_id":"` + card.ID.String() + `","title":"Better","reason":"vague"}]}`
	fc := &fakeCodegen{reviewerOutputs: []string{triageJSON}}
	triage := NewTriageService(store, fc, TriageConfig{})

	// Spring clean with an empty config (no repo path, no GH client)
	// returns no actions but does not error — exactly what we want
	// for the aggregation test.
	cleanup := NewSpringCleanService(nil, SpringCleanConfig{})

	res, err := NewCurateService(triage, cleanup).Run(context.Background(), userID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.TriageProposals) != 1 {
		t.Fatalf("expected 1 triage proposal in aggregate, got %d", len(res.TriageProposals))
	}
	if res.TriageProposals[0].Type != TriageProposalRewrite {
		t.Errorf("aggregated triage proposal type = %s, want rewrite", res.TriageProposals[0].Type)
	}
	if res.CleanupActions == nil {
		t.Error("CleanupActions should be a non-nil empty slice for stable JSON encoding")
	}
}

// TestCurateService_Run_PartialFailureSurfacesError checks that if one
// pipeline fails we still return the other pipeline's output instead
// of dropping it on the floor.
func TestCurateService_Run_PartialFailureSurfacesError(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	makeBacklogCard(store, userID)

	// Triage agent is broken; spring clean returns nothing (empty cfg).
	fc := &fakeCodegen{runErr: errors.New("codegen boom")}
	triage := NewTriageService(store, fc, TriageConfig{})
	cleanup := NewSpringCleanService(nil, SpringCleanConfig{})

	res, err := NewCurateService(triage, cleanup).Run(context.Background(), userID)
	if err != nil {
		// Single-pipeline failures should be surfaced via Errors,
		// not as a hard error, since the other side may have data.
		t.Fatalf("partial failure should not return a hard error: %v", err)
	}
	if _, ok := res.Errors["triage"]; !ok {
		t.Errorf("expected Errors[triage] to record the failure: %+v", res.Errors)
	}
}

// Sanity check: applying an empty proposal slice is a clean no-op.
func TestTriageService_ApplyProposals_EmptyIsNoop(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	makeBacklogCard(store, userID)

	svc := NewTriageService(store, &fakeCodegen{}, TriageConfig{})
	if err := svc.ApplyProposals(context.Background(), userID, nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
	all, _ := store.List(context.Background(), userID, uuid.Nil, "")
	if len(all) != 1 {
		t.Errorf("apply with empty slice altered card set: %d cards", len(all))
	}
}

