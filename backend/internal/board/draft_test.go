package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fakeDraftRepoLocator is the minimum Get-only ReposService substitute
// the DraftService needs. We can't use *ReposService in tests without
// a live DB pool; this in-memory stand-in is enough to exercise the
// draft logic.
type fakeDraftRepoLocator struct {
	repo *Repo
	err  error
}

func (f *fakeDraftRepoLocator) Get(ctx context.Context, userID, repoID uuid.UUID) (*Repo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.repo, nil
}

func newDraftTestRepo() *Repo {
	return &Repo{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		Owner:         "acme",
		Name:          "widgets",
		RepoPath:      "/tmp/acme-widgets",
		DefaultBranch: "main",
	}
}

func TestDraftService_Draft_HappyPath(t *testing.T) {
	repo := newDraftTestRepo()
	canned := `{
  "title": "Add retry logic to /sync",
  "description": "The /sync endpoint currently fails open on provider 5xx...",
  "acceptance_criteria": [
    "syncHandler returns 503 with Retry-After on upstream 5xx",
    "retry count is bounded to configured max",
    "metric sync_retry_total increments on each retry"
  ],
  "reasoning": "The repo already has retry helpers — reuse them to stay consistent."
}`
	fc := &fakeCodegen{reviewerOutputs: []string{canned}}
	svc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: repo},
		agent: fc,
	}

	draft, err := svc.Draft(context.Background(), uuid.New(), repo.ID, "retry on sync fail", "sometimes it 5xxs")
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if draft.Title != "Add retry logic to /sync" {
		t.Errorf("title = %q, want %q", draft.Title, "Add retry logic to /sync")
	}
	if len(draft.AcceptanceCriteria) != 3 {
		t.Fatalf("acceptance_criteria len = %d, want 3", len(draft.AcceptanceCriteria))
	}
	if !strings.Contains(draft.Description, "/sync") {
		t.Errorf("description missing /sync reference: %q", draft.Description)
	}
	if draft.Reasoning == "" {
		t.Error("reasoning should be populated")
	}

	// The agent ran in the repo path, not in the caller's cwd —
	// that's what makes the draft repo-aware.
	if len(fc.workDirs) != 1 || fc.workDirs[0] != repo.RepoPath {
		t.Errorf("workDir = %v, want %q once", fc.workDirs, repo.RepoPath)
	}
	// The prompt got the user's raw title verbatim so Claude can
	// decide whether to keep or rewrite it.
	if !strings.Contains(fc.Prompts()[0], "retry on sync fail") {
		t.Error("prompt missing user title")
	}
}

func TestDraftService_Draft_EmptyTitle(t *testing.T) {
	svc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: newDraftTestRepo()},
		agent: &fakeCodegen{},
	}

	_, err := svc.Draft(context.Background(), uuid.New(), uuid.New(), "   ", "body")
	if err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error = %q, want mention of title", err.Error())
	}
}

func TestDraftService_Draft_RepoLookupFails(t *testing.T) {
	wantErr := errors.New("repo gone")
	svc := &DraftService{
		repos: &fakeDraftRepoLocator{err: wantErr},
		agent: &fakeCodegen{},
	}

	_, err := svc.Draft(context.Background(), uuid.New(), uuid.New(), "title", "body")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Draft err = %v, want wrapped %v", err, wantErr)
	}
}

func TestDraftService_Draft_MalformedJSON(t *testing.T) {
	fc := &fakeCodegen{reviewerOutputs: []string{`not actually json, just prose`}}
	svc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: newDraftTestRepo()},
		agent: fc,
	}

	_, err := svc.Draft(context.Background(), uuid.New(), uuid.New(), "title", "body")
	if err == nil {
		t.Fatal("expected error for malformed agent output, got nil")
	}
	if !strings.Contains(err.Error(), "codegen draft") {
		t.Errorf("error = %q, want wrapped with 'codegen draft' prefix", err.Error())
	}
}

func TestDraftService_Draft_FallbackTitle(t *testing.T) {
	// When the model returns an empty title we keep the user's raw
	// one so the user doesn't end up staring at a blank field.
	canned := `{
  "title": "",
  "description": "body",
  "acceptance_criteria": ["a"],
  "reasoning": "r"
}`
	fc := &fakeCodegen{reviewerOutputs: []string{canned}}
	svc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: newDraftTestRepo()},
		agent: fc,
	}

	draft, err := svc.Draft(context.Background(), uuid.New(), uuid.New(), "user title", "body")
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if draft.Title != "user title" {
		t.Errorf("title = %q, want fallback to user title", draft.Title)
	}
}

func TestBuildCardDraftPrompt_ContainsInputs(t *testing.T) {
	got := BuildCardDraftPrompt("hi", "there")
	if !strings.Contains(got, "hi") {
		t.Error("prompt missing user title")
	}
	if !strings.Contains(got, "there") {
		t.Error("prompt missing user description")
	}
	// fakeCodegen's prompt sniff relies on this phrase — if it
	// changes, update isReviewerPrompt too.
	if !strings.Contains(got, "senior product engineer") {
		t.Error("prompt missing 'senior product engineer' framing (fakeCodegen sniff depends on this)")
	}
	if !strings.Contains(got, "acceptance_criteria") {
		t.Error("prompt missing acceptance_criteria field name")
	}
}
