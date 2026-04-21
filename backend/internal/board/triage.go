// Package board — triage flow.
//
// TriageService analyzes the user's backlog cards against the actual
// repo state and proposes maintenance actions: new cards to add, cards
// to close, and cards whose title/description/AC should be rewritten.
//
// Triage runs the Codegen reviewer agent in the user's repo checkout so
// the model sees the real code (not just a list of titles). All
// proposals are read-only suggestions — callers MUST surface them to
// the user for approval and pass only the approved subset to
// ApplyProposals.
package board

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// TriageProposalType enumerates the three actions triage can propose.
type TriageProposalType string

const (
	TriageProposalCreate  TriageProposalType = "create"
	TriageProposalClose   TriageProposalType = "close"
	TriageProposalRewrite TriageProposalType = "rewrite"
)

// TriageProposal is an action proposed by the triage analyzer. All
// proposals require explicit user approval before being applied.
type TriageProposal struct {
	Type               TriageProposalType `json:"type"`
	CardID             *uuid.UUID         `json:"card_id,omitempty"` // nil for create; set for close/rewrite
	Title              string             `json:"title,omitempty"`   // proposed title (create or rewrite)
	Description        string             `json:"description,omitempty"`
	AcceptanceCriteria []string           `json:"acceptance_criteria,omitempty"`
	Reason             string             `json:"reason,omitempty"`
}

// TriageConfig configures a TriageService.
type TriageConfig struct {
	// WorkDir is the local checkout the reviewer agent runs in. When
	// empty the agent runs in its caller's cwd, which is rarely useful;
	// callers should set this to the user's RepoPath.
	WorkDir string
	// GitHub is an optional GitHub client used to fetch open issues for
	// extra repo context. If nil, triage runs against backlog cards only.
	GitHub github.Client
	// RepoOwner / RepoName identify the repo for GitHub lookups. Required
	// when GitHub is non-nil.
	RepoOwner string
	RepoName  string
	// RepoID scopes the cards.List/Create calls to a single board.
	// Multi-repo support means a user can have many backlogs; triage
	// runs against exactly one of them.
	RepoID uuid.UUID
}

// TriageService produces TriageProposals via a Codegen reviewer agent.
type TriageService struct {
	cards cardStore
	agent codegen.Agent
	cfg   TriageConfig
}

// NewTriageService constructs a TriageService.
func NewTriageService(cards cardStore, agent codegen.Agent, cfg TriageConfig) *TriageService {
	return &TriageService{cards: cards, agent: agent, cfg: cfg}
}

// AnalyzeBacklog reads the user's backlog cards and (optionally) the
// repo's open GitHub issues, asks the reviewer agent for triage
// proposals, and returns them. Read-only.
func (s *TriageService) AnalyzeBacklog(ctx context.Context, userID uuid.UUID) ([]TriageProposal, error) {
	if s == nil || s.cards == nil {
		return nil, fmt.Errorf("triage service not configured")
	}
	if s.agent == nil {
		return nil, fmt.Errorf("triage requires a codegen agent")
	}

	backlog, err := s.cards.List(ctx, userID, s.cfg.RepoID, ColumnBacklog)
	if err != nil {
		return nil, fmt.Errorf("list backlog: %w", err)
	}

	var openIssues []github.Issue
	if s.cfg.GitHub != nil && strings.TrimSpace(s.cfg.RepoOwner) != "" && strings.TrimSpace(s.cfg.RepoName) != "" {
		issues, err := s.cfg.GitHub.ListIssues(ctx, s.cfg.RepoOwner, s.cfg.RepoName, github.ListIssuesOptions{
			State:      "open",
			PerPage:    100,
			ExcludePRs: true,
		})
		if err == nil {
			openIssues = issues
		}
	}

	prompt := buildTriagePrompt(backlog, openIssues)

	var wire triageWire
	if err := codegen.RunJSON(ctx, s.agent, prompt, s.cfg.WorkDir, &wire); err != nil {
		return nil, fmt.Errorf("triage agent: %w", err)
	}
	return filterTriageProposals(wire, backlog), nil
}

// ApplyProposals applies user-approved proposals. The caller MUST filter
// to the approved subset before calling this; nothing is filtered here.
func (s *TriageService) ApplyProposals(ctx context.Context, userID uuid.UUID, proposals []TriageProposal) error {
	if s == nil || s.cards == nil {
		return fmt.Errorf("triage service not configured")
	}
	for _, p := range proposals {
		switch p.Type {
		case TriageProposalCreate:
			title := strings.TrimSpace(p.Title)
			if title == "" {
				return fmt.Errorf("create proposal missing title")
			}
			desc := composeDescription(p.Description, p.AcceptanceCriteria)
			if _, err := s.cards.Create(ctx, userID, s.cfg.RepoID, CreateRequest{Title: title, Description: desc}); err != nil {
				return fmt.Errorf("apply create %q: %w", title, err)
			}
		case TriageProposalClose:
			if p.CardID == nil {
				return fmt.Errorf("close proposal missing card id")
			}
			if err := s.cards.Delete(ctx, userID, *p.CardID); err != nil {
				return fmt.Errorf("apply close %s: %w", p.CardID, err)
			}
		case TriageProposalRewrite:
			if p.CardID == nil {
				return fmt.Errorf("rewrite proposal missing card id")
			}
			title := strings.TrimSpace(p.Title)
			desc := composeDescription(p.Description, p.AcceptanceCriteria)
			req := UpdateRequest{}
			if title != "" {
				req.Title = &title
			}
			req.Description = &desc
			if _, err := s.cards.Update(ctx, userID, *p.CardID, req); err != nil {
				return fmt.Errorf("apply rewrite %s: %w", p.CardID, err)
			}
		default:
			return fmt.Errorf("unknown triage proposal type %q", p.Type)
		}
	}
	return nil
}

func composeDescription(body string, ac []string) string {
	body = strings.TrimSpace(body)
	if len(ac) == 0 {
		return body
	}
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("## Acceptance Criteria\n")
	for _, item := range ac {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fmt.Fprintf(&b, "- [ ] %s\n", item)
	}
	return strings.TrimRight(b.String(), "\n")
}

type triageWire struct {
	Proposals []struct {
		Type               string   `json:"type"`
		CardID             string   `json:"card_id,omitempty"`
		Title              string   `json:"title,omitempty"`
		Description        string   `json:"description,omitempty"`
		AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
		Reason             string   `json:"reason,omitempty"`
	} `json:"proposals"`
}

// filterTriageProposals drops any proposal that fails validation
// (unknown type, dangling card_id, empty create title) so the caller
// only sees actionable suggestions.
func filterTriageProposals(wire triageWire, backlog []Card) []TriageProposal {
	allowed := map[uuid.UUID]struct{}{}
	for _, c := range backlog {
		allowed[c.ID] = struct{}{}
	}

	out := make([]TriageProposal, 0, len(wire.Proposals))
	for _, p := range wire.Proposals {
		t := TriageProposalType(strings.ToLower(strings.TrimSpace(p.Type)))
		switch t {
		case TriageProposalCreate, TriageProposalClose, TriageProposalRewrite:
		default:
			continue
		}
		proposal := TriageProposal{
			Type:               t,
			Title:              strings.TrimSpace(p.Title),
			Description:        strings.TrimSpace(p.Description),
			AcceptanceCriteria: trimNonEmpty(p.AcceptanceCriteria),
			Reason:             strings.TrimSpace(p.Reason),
		}
		switch t {
		case TriageProposalClose, TriageProposalRewrite:
			id, err := uuid.Parse(strings.TrimSpace(p.CardID))
			if err != nil {
				continue
			}
			if _, ok := allowed[id]; !ok {
				continue
			}
			proposal.CardID = &id
		case TriageProposalCreate:
			if proposal.Title == "" {
				continue
			}
		}
		out = append(out, proposal)
	}
	return out
}

func trimNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sortBacklogByID is exposed to keep prompt content deterministic for
// snapshot-style tests.
func sortBacklogByID(in []Card) []Card {
	out := make([]Card, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out
}
