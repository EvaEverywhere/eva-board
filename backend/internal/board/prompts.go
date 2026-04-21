package board

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

const maxFeedbackPromptChars = 6000

// truncateForPrompt clips long feedback blobs so the prompt does not blow
// past sensible context budgets.
func truncateForPrompt(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	return trimmed[:max] + "\n\n[... truncated ...]"
}

// buildAgentPrompt is the prompt fed to the coding agent CLI for an initial
// run. Optional feedback (review feedback or user-submitted feedback) is
// appended under a clearly labelled section.
func buildAgentPrompt(card Card, feedback string) string {
	var b strings.Builder
	b.WriteString("You are an autonomous coding agent implementing a feature for the Eva Board project.\n\n")
	fmt.Fprintf(&b, "## Issue: %s\n\n", card.Title)
	if strings.TrimSpace(card.Description) != "" {
		fmt.Fprintf(&b, "## Description\n%s\n\n", card.Description)
	}
	b.WriteString("## Instructions\n")
	b.WriteString("1. Read and understand the relevant code before making changes.\n")
	b.WriteString("2. Implement the feature following existing patterns in the codebase.\n")
	b.WriteString("3. Write or update tests to cover your changes.\n")
	b.WriteString("4. Run `go build ./...` and `go test ./...` to verify everything compiles and passes.\n")
	b.WriteString("5. Commit your changes with a clear, descriptive commit message.\n")
	b.WriteString("   Use conventional commits: feat:, fix:, refactor:, etc.\n")
	b.WriteString("6. You may make multiple commits if the change is large.\n")

	feedback = strings.TrimSpace(feedback)
	if feedback != "" {
		b.WriteString("\n## Review Feedback (must address)\n")
		b.WriteString(truncateForPrompt(feedback, maxFeedbackPromptChars))
		b.WriteString("\n")
	}
	return b.String()
}

// buildVerifyPrompt frames Claude as a REVIEWER (not the implementer)
// scoring acceptance criteria against the worktree it is running inside.
// The prompt deliberately encourages the agent to read source files
// directly rather than relying on a diff blob — that's the whole reason
// we run the reviewer in the worktree.
func buildVerifyPrompt(card Card, criteria []string) string {
	var criteriaList strings.Builder
	for i, c := range criteria {
		fmt.Fprintf(&criteriaList, "%d. %s\n", i+1, c)
	}

	return fmt.Sprintf(`You are a senior code reviewer verifying acceptance criteria for a feature you did NOT write.

You are running inside the git worktree containing the candidate change. You have full access
to the repository: read source files, follow imports, run %s, inspect commit history.
Use that context. Do NOT rely on a diff blob alone.

## Issue: %s

## Description
%s

## Acceptance Criteria
%s

## What to do
1. Inspect the worktree to understand what changed. Useful commands:
   - %s to list changed files vs main
   - %s to read individual files (preferred over a diff blob)
   - %s to see the full diff if needed
2. For EACH acceptance criterion above, decide whether the implementation in this worktree
   satisfies it. Be strict: missing tests, partial implementations, or broken edge cases
   are NOT met.
3. Briefly explain your reasoning per criterion (one sentence).

## Output
Respond with ONLY a JSON object — no prose, no markdown fences. Shape:

{
  "results": [
    {"criterion": "<exact criterion text>", "met": true|false, "reason": "<short explanation>"}
  ],
  "summary": "<one or two sentences on what was built and any gaps>"
}

Rules:
- Include one entry in "results" for every criterion above, in order.
- "met": true means fully satisfied by the code in this worktree.
- "met": false means missing, broken, or only partially addressed.
- Output valid JSON only. No commentary outside the JSON.`,
		"`go test ./...`",
		card.Title,
		card.Description,
		criteriaList.String(),
		"`git diff --name-only main...HEAD`",
		"`cat <path>` / your file-reading tools",
		"`git diff main...HEAD`",
	)
}

// buildReviewPrompt frames Claude as a senior reviewer of code they did
// NOT write. Skepticism is the point — the same model wrote the code, so
// a fresh prompt + reviewer framing is what reduces rubber-stamping.
func buildReviewPrompt(card Card) string {
	return fmt.Sprintf(`You are a senior code reviewer for a GitHub issue implementation.

IMPORTANT: You did NOT write this code. Treat it with the same skepticism you would a junior
engineer's first PR. Your job is to catch bugs, missing tests, security issues, and code that
"looks fine" but breaks under load — not to validate work you remember doing.

You are running inside the git worktree with the candidate change checked out. You have full
access to read files, follow imports, and run %s. Use that context.

## Issue: %s

## Description
%s

## What to do
1. Inspect the worktree. Useful commands:
   - %s to see what changed
   - %s to read files directly (preferred)
   - %s to see the diff if you need it
2. Evaluate the change for:
   - Correctness vs the issue requirements above
   - Edge cases and potential regressions
   - Test coverage (a feature without tests is REQUEST_CHANGES)
   - Security and obvious performance footguns
   - Adherence to existing patterns in the codebase
3. Decide APPROVE or REQUEST_CHANGES.

## Output
Respond with ONLY a JSON object — no prose, no markdown fences. Shape:

{
  "verdict": "APPROVE" | "REQUEST_CHANGES",
  "summary": "<short paragraph explaining the verdict>",
  "suggestions": ["<actionable change 1>", "<actionable change 2>"]
}

Rules:
- Use REQUEST_CHANGES whenever concrete fixes are required. Bias toward REQUEST_CHANGES on
  the first review — if it's borderline, ask for the fix.
- Suggestions must be specific and actionable; the implementing agent reads them verbatim.
- If verdict is APPROVE, "suggestions" may be empty.
- Output valid JSON only. No commentary outside the JSON.`,
		"`go test ./...`",
		card.Title,
		card.Description,
		"`git diff --name-only main...HEAD`",
		"`cat <path>` / your file-reading tools",
		"`git diff main...HEAD`",
	)
}

// buildTriagePrompt frames Claude as a backlog triager analyzing both
// the user's existing cards and the actual repo state.
func buildTriagePrompt(backlog []Card, openIssues []github.Issue) string {
	cards := make([]triageCardWire, 0, len(backlog))
	for _, c := range backlog {
		cards = append(cards, triageCardWire{
			ID:          c.ID.String(),
			Title:       strings.TrimSpace(c.Title),
			Description: strings.TrimSpace(c.Description),
		})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })

	issues := make([]triageIssueWire, 0, len(openIssues))
	for _, i := range openIssues {
		issues = append(issues, triageIssueWire{
			Number: i.Number,
			Title:  strings.TrimSpace(i.Title),
			Body:   strings.TrimSpace(i.Body),
		})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })

	cardsJSON, _ := json.MarshalIndent(cards, "", "  ")
	issuesJSON, _ := json.MarshalIndent(issues, "", "  ")

	return fmt.Sprintf(`You are a senior engineer triaging a developer's backlog.

You are running inside the project's repository. Use it: read the code, look at recent commits,
inspect the file tree to understand what is built and what is missing. Compare reality against
the backlog cards and the open GitHub issues.

You may propose three kinds of actions:
- "create": a new backlog card for an open GitHub issue that is not yet tracked, or for
  engineering work the backlog clearly missed.
- "close": close (delete) a backlog card that is no longer relevant — already implemented in
  the code, superseded, or obsolete.
- "rewrite": rewrite a card whose title or description is vague or missing acceptance criteria,
  preserving its original intent.

Be conservative. Prefer fewer high-confidence proposals over many speculative ones. For "close"
in particular, only propose it if you can point to concrete evidence in the code that the work
is done.

## Current backlog cards (JSON)
%s

## Repo open GitHub issues (JSON)
%s

## Output
Respond with ONLY a JSON object — no prose, no markdown fences. Shape:

{
  "proposals": [
    {
      "type": "create" | "close" | "rewrite",
      "card_id": "<uuid; required for close and rewrite, omit for create>",
      "title": "<proposed title; required for create and rewrite>",
      "description": "<proposed markdown body>",
      "acceptance_criteria": ["...", "..."],
      "reason": "<why this proposal — cite files/issues where relevant>"
    }
  ]
}

Rules:
- For "close" and "rewrite", card_id MUST be one of the backlog card ids above.
- For "create", do NOT set card_id.
- Always include a "reason".
- Output valid JSON only. No commentary outside the JSON.`,
		string(cardsJSON), string(issuesJSON))
}

// triage*Wire types are the JSON shape we serialise into the triage prompt.
type triageCardWire struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type triageIssueWire struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
}

// buildPRBody renders the PR body shown on GitHub after the loop succeeds.
func buildPRBody(card Card, criteria []CriterionResult, review ReviewResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", card.Title)

	if strings.TrimSpace(card.Description) != "" {
		b.WriteString(card.Description)
		b.WriteString("\n\n")
	}

	if len(criteria) > 0 {
		b.WriteString("### Acceptance Criteria\n\n")
		for _, c := range criteria {
			box := "[ ]"
			if c.Met {
				box = "[x]"
			}
			fmt.Fprintf(&b, "- %s **%s** — %s\n", box, c.Criterion, c.Reason)
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(review.Summary) != "" {
		b.WriteString("### Review Summary\n\n")
		b.WriteString(review.Summary)
		b.WriteString("\n\n")
	}

	if len(review.Suggestions) > 0 {
		b.WriteString("### Reviewer Suggestions\n\n")
		for _, s := range review.Suggestions {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n*Auto-generated by the Eva Board agent loop.*\n")
	return b.String()
}
