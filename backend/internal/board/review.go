package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
)

// ReviewVerdict is the LLM's high-level disposition of a diff.
type ReviewVerdict string

const (
	ReviewApprove        ReviewVerdict = "APPROVE"
	ReviewRequestChanges ReviewVerdict = "REQUEST_CHANGES"
)

// ReviewResult is the parsed code-review output. Suggestions are flat strings
// because the agent feeds them straight back into the next coding-agent
// invocation as bullet-point feedback.
type ReviewResult struct {
	Verdict     ReviewVerdict `json:"verdict"`
	Summary     string        `json:"summary"`
	Suggestions []string      `json:"suggestions"`
}

// ParseVerdict normalizes free-form verdict strings from the LLM into one of
// the two supported values. Unknown / empty verdicts default to
// REQUEST_CHANGES — the safer choice, because it forces another loop rather
// than auto-approving an ambiguous review.
func ParseVerdict(s string) ReviewVerdict {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "APPROVE", "APPROVED", "LGTM":
		return ReviewApprove
	case "REQUEST_CHANGES", "REQUEST CHANGES", "CHANGES_REQUESTED", "CHANGES REQUESTED":
		return ReviewRequestChanges
	default:
		return ReviewRequestChanges
	}
}

// ReviewCard runs the LLM code-review prompt against the diff and returns a
// normalized verdict. Empty diffs are treated as REQUEST_CHANGES because
// nothing to review is itself a problem.
func ReviewCard(ctx context.Context, client llm.Client, model string, card Card, diff string) (ReviewResult, error) {
	if client == nil {
		return ReviewResult{}, fmt.Errorf("review: llm client is nil")
	}
	if strings.TrimSpace(model) == "" {
		return ReviewResult{}, fmt.Errorf("review: model is required")
	}
	if strings.TrimSpace(diff) == "" {
		return ReviewResult{
			Verdict:     ReviewRequestChanges,
			Summary:     "No code changes found on the branch.",
			Suggestions: []string{"Implement the requested changes — the diff is empty."},
		}, nil
	}

	prompt := buildReviewPrompt(card, diff)
	raw, err := client.ChatCompletion(ctx, llm.CompletionRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
	})
	if err != nil {
		return ReviewResult{}, fmt.Errorf("review: llm call: %w", err)
	}

	var parsed struct {
		Verdict     string   `json:"verdict"`
		Summary     string   `json:"summary"`
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(llm.CleanJSON(raw)), &parsed); err != nil {
		return ReviewResult{}, fmt.Errorf("review: parse llm json: %w (raw=%q)", err, raw)
	}

	out := ReviewResult{
		Verdict:     ParseVerdict(parsed.Verdict),
		Summary:     strings.TrimSpace(parsed.Summary),
		Suggestions: cleanSuggestions(parsed.Suggestions),
	}
	return out, nil
}

func cleanSuggestions(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// formatReviewFeedback renders a review's suggestions as a feedback block
// suitable for the next agent invocation.
func formatReviewFeedback(review ReviewResult) string {
	var b strings.Builder
	if strings.TrimSpace(review.Summary) != "" {
		b.WriteString(review.Summary)
		b.WriteString("\n\n")
	}
	if len(review.Suggestions) > 0 {
		b.WriteString("Address each of the following:\n")
		for _, s := range review.Suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if b.Len() == 0 {
		b.WriteString("Reviewer requested changes but provided no specific suggestions; re-evaluate the diff against the issue requirements.")
	}
	return b.String()
}
