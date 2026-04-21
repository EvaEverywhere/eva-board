// Package board — triage flow.
//
// TriageService analyzes the user's backlog cards against the actual repo
// state (open GitHub issues) and proposes maintenance actions: new cards
// to add, cards to close, and cards whose title/description/AC should be
// rewritten.
//
// All proposals are read-only suggestions. Callers MUST surface them to
// the user for approval and pass only the approved subset to
// ApplyProposals.
package board

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
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
	// Model is the LLM model identifier (e.g. "openai/gpt-4o-mini").
	Model string
	// Temperature passed to the LLM. Defaults to 0.2 if zero.
	Temperature float64
	// MaxTokens for the LLM completion. Defaults to 4000 if <= 0.
	MaxTokens int
	// GitHub is an optional GitHub client used to fetch open issues for
	// extra repo context. If nil, triage runs against backlog cards only.
	GitHub github.Client
	// RepoOwner / RepoName identify the repo for GitHub lookups. Required
	// when GitHub is non-nil.
	RepoOwner string
	RepoName  string
}

// TriageService produces TriageProposals via an LLM.
type TriageService struct {
	cards cardStore
	llm   llm.Client
	cfg   TriageConfig
}

// NewTriageService constructs a TriageService.
func NewTriageService(cards cardStore, llmClient llm.Client, cfg TriageConfig) *TriageService {
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.2
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4000
	}
	return &TriageService{cards: cards, llm: llmClient, cfg: cfg}
}

// AnalyzeBacklog reads the user's backlog cards and (optionally) the
// repo's open GitHub issues, asks the LLM for triage proposals, and
// returns them. Read-only.
func (s *TriageService) AnalyzeBacklog(ctx context.Context, userID uuid.UUID) ([]TriageProposal, error) {
	if s == nil || s.cards == nil {
		return nil, fmt.Errorf("triage service not configured")
	}
	if s.llm == nil {
		return nil, fmt.Errorf("triage requires an llm client")
	}

	backlog, err := s.cards.List(ctx, userID, ColumnBacklog)
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

	prompt, err := buildTriagePrompt(backlog, openIssues)
	if err != nil {
		return nil, err
	}

	resp, err := s.llm.ChatCompletion(ctx, llm.CompletionRequest{
		Model:       s.cfg.Model,
		Temperature: s.cfg.Temperature,
		MaxTokens:   s.cfg.MaxTokens,
		Messages: []llm.Message{
			{Role: "system", Content: triageSystemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm triage: %w", err)
	}

	proposals, err := parseTriageProposals(resp, backlog)
	if err != nil {
		return nil, fmt.Errorf("parse triage proposals: %w", err)
	}
	return proposals, nil
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
			if _, err := s.cards.Create(ctx, userID, CreateRequest{Title: title, Description: desc}); err != nil {
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

const triageSystemPrompt = `You are a senior software engineer triaging a developer's backlog.
You compare the backlog cards against the actual repo state (open GitHub issues) and propose maintenance actions.
You ONLY propose actions; the user reviews and approves before anything is applied.

You may propose three kinds of actions:
- "create": a new backlog card for an open GitHub issue that is not yet tracked, or for engineering work the backlog clearly missed.
- "close": close (delete) a backlog card that is no longer relevant — already done, superseded, or obsolete.
- "rewrite": rewrite a card whose title or description is vague or missing acceptance criteria, leaving its meaning intact.

Return ONLY valid JSON (no markdown fences, no commentary) with this shape:
{
  "proposals": [
    {
      "type": "create" | "close" | "rewrite",
      "card_id": "uuid string when type is close or rewrite; omit for create",
      "title": "proposed title (create or rewrite)",
      "description": "proposed markdown body (create or rewrite)",
      "acceptance_criteria": ["...", "..."],
      "reason": "why this proposal"
    }
  ]
}

Rules:
- Be conservative. Prefer fewer high-quality proposals over many speculative ones.
- For "close" and "rewrite", card_id MUST be one of the backlog card ids provided.
- For "create", do NOT set card_id.
- For "create" and "rewrite", title MUST be concise and imperative.
- Always include a "reason".`

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

func parseTriageProposals(raw string, backlog []Card) ([]TriageProposal, error) {
	cleaned := llm.CleanJSON(raw)
	if cleaned == "" {
		return []TriageProposal{}, nil
	}
	var wire triageWire
	if err := json.Unmarshal([]byte(cleaned), &wire); err != nil {
		return nil, fmt.Errorf("invalid triage json: %w", err)
	}

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
	return out, nil
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

type triageBacklogCard struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type triageRepoIssue struct {
	Number  int       `json:"number"`
	Title   string    `json:"title"`
	Body    string    `json:"body,omitempty"`
	Updated time.Time `json:"updated_at,omitempty"`
}

func buildTriagePrompt(backlog []Card, openIssues []github.Issue) (string, error) {
	cards := make([]triageBacklogCard, 0, len(backlog))
	for _, c := range backlog {
		cards = append(cards, triageBacklogCard{
			ID:          c.ID.String(),
			Title:       strings.TrimSpace(c.Title),
			Description: strings.TrimSpace(c.Description),
		})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })

	issues := make([]triageRepoIssue, 0, len(openIssues))
	for _, i := range openIssues {
		issues = append(issues, triageRepoIssue{
			Number:  i.Number,
			Title:   strings.TrimSpace(i.Title),
			Body:    strings.TrimSpace(i.Body),
			Updated: i.UpdatedAt,
		})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })

	cardsJSON, err := json.MarshalIndent(cards, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal backlog cards: %w", err)
	}
	issuesJSON, err := json.MarshalIndent(issues, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal repo issues: %w", err)
	}

	var b strings.Builder
	b.WriteString("## Current backlog cards (JSON)\n")
	b.Write(cardsJSON)
	b.WriteString("\n\n## Repo open GitHub issues (JSON)\n")
	b.Write(issuesJSON)
	b.WriteString("\n\nReturn the JSON object described in the system prompt.")
	return b.String(), nil
}
