package board

import "testing"

func TestDedupeProposals_RemovesOverlappingClose(t *testing.T) {
	triage := []TriageProposal{{
		Type:   TriageProposalClose,
		Reason: "github_issue:owner/repo#7",
	}}
	cleanup := []CleanupAction{
		{Type: CleanupCloseIssue, Target: "owner/repo#7", Reason: "stale"},
		{Type: CleanupCloseIssue, Target: "owner/repo#9", Reason: "wontfix"},
		{Type: CleanupDeleteBranch, Target: "eva-board/feature-x"},
	}

	gotTri, gotClean := dedupeProposals(triage, cleanup)
	if len(gotTri) != 1 {
		t.Errorf("expected 1 triage proposal, got %d", len(gotTri))
	}
	if len(gotClean) != 2 {
		t.Fatalf("expected 2 cleanup actions after dedupe, got %d", len(gotClean))
	}
	for _, a := range gotClean {
		if a.Type == CleanupCloseIssue && a.Target == "owner/repo#7" {
			t.Errorf("dedupe failed to drop overlapping close_issue: %+v", a)
		}
	}
}

func TestDedupeProposals_PreservesWhenNoOverlap(t *testing.T) {
	triage := []TriageProposal{{Type: TriageProposalCreate, Title: "x"}}
	cleanup := []CleanupAction{{Type: CleanupRemoveWorktree, Target: "/tmp/wt"}}
	gotTri, gotClean := dedupeProposals(triage, cleanup)
	if len(gotTri) != 1 || len(gotClean) != 1 {
		t.Fatalf("dedupe altered non-overlapping inputs: %+v %+v", gotTri, gotClean)
	}
}

func TestDedupeProposals_EmptyInputs(t *testing.T) {
	tri, clean := dedupeProposals(nil, nil)
	if tri == nil || clean == nil {
		t.Fatal("expected non-nil empty slices for stable JSON encoding")
	}
	if len(tri) != 0 || len(clean) != 0 {
		t.Fatalf("expected empty results, got %d / %d", len(tri), len(clean))
	}
}
