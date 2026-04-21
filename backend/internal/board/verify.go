package board

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
)

// CriterionResult is the verdict for a single acceptance criterion.
type CriterionResult struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Reason    string `json:"reason"`
}

// VerificationResult bundles the per-criterion verdicts plus a free-text
// summary and a derived AllPassed flag.
type VerificationResult struct {
	AllPassed bool              `json:"all_passed"`
	Verdicts  []CriterionResult `json:"verdicts"`
	Summary   string            `json:"summary"`
}

// AcceptanceCriterion is a parsed checkbox from a card description. The
// Checked field reflects the original markdown state (`[x]` vs `[ ]`); the
// agent loop verifies all criteria regardless of that initial state.
type AcceptanceCriterion struct {
	Text    string
	Checked bool
}

// checkboxPattern matches markdown checklist lines like "- [ ] criterion" or
// "* [x] criterion". Both `-` and `*` bullets and any case of `x` are
// accepted. We use `[ \t]*` rather than `\s*` because Go's `\s` matches
// newlines, which would let the trailing capture group cross line boundaries
// and produce bogus criterion text on lines that are actually empty.
var checkboxPattern = regexp.MustCompile(`(?mi)^[-*][ \t]*\[( |x)\][ \t]*(.+)$`)

// ParseAcceptanceCriteriaDetailed returns each parsed checkbox with its
// original checked state.
func ParseAcceptanceCriteriaDetailed(description string) []AcceptanceCriterion {
	matches := checkboxPattern.FindAllStringSubmatch(description, -1)
	out := make([]AcceptanceCriterion, 0, len(matches))
	for _, m := range matches {
		text := strings.TrimSpace(m[2])
		if text == "" {
			continue
		}
		out = append(out, AcceptanceCriterion{
			Text:    text,
			Checked: strings.EqualFold(m[1], "x"),
		})
	}
	return out
}

// ParseAcceptanceCriteria returns just the criterion text in document order.
// This is the form fed to the verification agent.
func ParseAcceptanceCriteria(description string) []string {
	parsed := ParseAcceptanceCriteriaDetailed(description)
	out := make([]string, 0, len(parsed))
	for _, c := range parsed {
		out = append(out, c.Text)
	}
	return out
}

// VerifyAgentWork scores the worktree against each acceptance criterion
// using a Codegen reviewer agent. The reviewer runs in worktreeDir so it
// can read the changed files, follow imports, and inspect the diff
// directly — strictly more context than a blind diff blob.
//
// When there are no criteria the caller should treat it as auto-pass; this
// helper still returns nil so the caller can short-circuit on len == 0.
func VerifyAgentWork(ctx context.Context, agent codegen.Agent, criteria []string, worktreeDir string) ([]CriterionResult, error) {
	if agent == nil {
		return nil, fmt.Errorf("verify: codegen agent is nil")
	}
	if len(criteria) == 0 {
		return nil, nil
	}

	prompt := buildVerifyPrompt(Card{Title: "Verification"}, criteria)
	return runVerificationAgent(ctx, agent, prompt, worktreeDir)
}

// verifyCard is the card-aware variant used by the agent loop so the issue
// title and description reach the reviewer.
func verifyCard(ctx context.Context, agent codegen.Agent, card Card, worktreeDir string) (VerificationResult, error) {
	criteria := ParseAcceptanceCriteria(card.Description)
	if len(criteria) == 0 {
		return VerificationResult{
			AllPassed: true,
			Verdicts:  nil,
			Summary:   "No acceptance criteria found — auto-passing.",
		}, nil
	}

	prompt := buildVerifyPrompt(card, criteria)
	verdicts, err := runVerificationAgent(ctx, agent, prompt, worktreeDir)
	if err != nil {
		return VerificationResult{}, err
	}
	allPassed := true
	for _, v := range verdicts {
		if !v.Met {
			allPassed = false
			break
		}
	}
	return VerificationResult{
		AllPassed: allPassed,
		Verdicts:  verdicts,
		Summary:   summarizeVerdicts(verdicts),
	}, nil
}

func runVerificationAgent(ctx context.Context, agent codegen.Agent, prompt, worktreeDir string) ([]CriterionResult, error) {
	var parsed struct {
		// Accept either `results` (new prompt) or `verdicts` (legacy)
		// so we don't fail open if Claude picks the older key.
		Results  []CriterionResult `json:"results"`
		Verdicts []CriterionResult `json:"verdicts"`
		Summary  string            `json:"summary"`
	}
	if err := codegen.RunJSON(ctx, agent, prompt, worktreeDir, &parsed); err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	if len(parsed.Results) > 0 {
		return parsed.Results, nil
	}
	return parsed.Verdicts, nil
}

func makeAllFail(criteria []string, reason string) []CriterionResult {
	verdicts := make([]CriterionResult, len(criteria))
	for i, c := range criteria {
		verdicts[i] = CriterionResult{Criterion: c, Met: false, Reason: reason}
	}
	return verdicts
}

func summarizeVerdicts(verdicts []CriterionResult) string {
	if len(verdicts) == 0 {
		return "No criteria were evaluated."
	}
	passed := 0
	for _, v := range verdicts {
		if v.Met {
			passed++
		}
	}
	return fmt.Sprintf("%d of %d acceptance criteria met.", passed, len(verdicts))
}

func formatFailedCriteriaFeedback(verdicts []CriterionResult) string {
	var b strings.Builder
	b.WriteString("The following acceptance criteria were NOT met. Please fix them:\n\n")
	for _, v := range verdicts {
		if v.Met {
			continue
		}
		fmt.Fprintf(&b, "- %s — %s\n", v.Criterion, strings.TrimSpace(v.Reason))
	}
	return b.String()
}
