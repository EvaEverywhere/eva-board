package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/teslashibe/codegen-go"
)

// ReviewVerdict is the reviewer's high-level disposition of the change.
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

// ParseVerdict normalizes free-form verdict strings from the reviewer into
// one of the two supported values. Unknown / empty verdicts default to
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

// ReviewCard runs a Codegen reviewer agent against the worktree and returns
// a normalized verdict. The reviewer is told it did NOT write this code
// and is asked to be skeptical. Running inside worktreeDir lets it read
// related files, follow imports, and run tests — far better than scoring
// a diff blob in isolation.
func ReviewCard(ctx context.Context, agent codegen.Agent, card Card, worktreeDir string) (ReviewResult, error) {
	if agent == nil {
		return ReviewResult{}, fmt.Errorf("review: codegen agent is nil")
	}

	prompt := buildReviewPrompt(card)

	var parsed struct {
		Verdict     string   `json:"verdict"`
		Summary     string   `json:"summary"`
		Suggestions []string `json:"suggestions"`
	}
	if err := codegen.RunJSON(ctx, agent, prompt, worktreeDir, &parsed); err != nil {
		return ReviewResult{}, fmt.Errorf("review: %w", err)
	}

	return ReviewResult{
		Verdict:     ParseVerdict(parsed.Verdict),
		Summary:     strings.TrimSpace(parsed.Summary),
		Suggestions: cleanSuggestions(parsed.Suggestions),
	}, nil
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
