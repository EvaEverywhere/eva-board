// Package board — card drafting flow.
//
// DraftService turns a user's rough "title + description" pair into a
// structured CardDraft via the configured codegen agent, running in the
// target repo's worktree so the model can inspect files and produce a
// grounded, repo-aware draft with testable acceptance criteria.
//
// This is deliberately a read-only planning step: the caller (the New
// Card modal) shows the draft to the user for inline editing, then
// POSTs the edited result to the normal card create endpoint. No cards
// are persisted here.
package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/teslashibe/codegen-go"
)

// CardDraft is the structured output of a codegen-assisted card draft.
type CardDraft struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Reasoning          string   `json:"reasoning"`
}

// draftRepoLocator is the slice of ReposService the draft flow needs
// to resolve a repo's on-disk path. Defining it as an interface keeps
// DraftService testable without a real *ReposService (which needs a
// live DB pool).
type draftRepoLocator interface {
	Get(ctx context.Context, userID, repoID uuid.UUID) (*Repo, error)
}

// DraftService turns a user's rough "title + description" pair into a
// structured CardDraft via the configured codegen agent, running in the
// target repo's worktree so the model can inspect files.
type DraftService struct {
	repos draftRepoLocator
	agent codegen.Agent
	opts  []codegen.RunOption
}

// NewDraftService wires the production DraftService. Tests construct a
// DraftService directly with a fake locator.
func NewDraftService(repos *ReposService, agent codegen.Agent, opts ...codegen.RunOption) *DraftService {
	return &DraftService{repos: repos, agent: agent, opts: opts}
}

// Draft validates inputs, resolves the repo's on-disk path, builds the
// prompt, invokes the agent, and decodes the response. Returns a user-
// visible error when validation fails so the HTTP handler can surface
// it directly.
func (s *DraftService) Draft(ctx context.Context, userID, repoID uuid.UUID, title, description string) (CardDraft, error) {
	if s == nil || s.agent == nil || s.repos == nil {
		return CardDraft{}, fmt.Errorf("draft service not configured")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return CardDraft{}, fmt.Errorf("title is required")
	}
	repo, err := s.repos.Get(ctx, userID, repoID)
	if err != nil {
		return CardDraft{}, err
	}
	prompt := BuildCardDraftPrompt(title, strings.TrimSpace(description))
	var out CardDraft
	if err := codegen.RunJSON(ctx, s.agent, prompt, repo.RepoPath, &out, s.opts...); err != nil {
		return CardDraft{}, fmt.Errorf("codegen draft: %w", err)
	}
	// Fall back to the user's raw title if the model returned empty —
	// we'd rather save the raw title than surface an empty draft.
	if strings.TrimSpace(out.Title) == "" {
		out.Title = title
	}
	return out, nil
}
