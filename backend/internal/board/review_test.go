package board

import (
	"context"
	"errors"
	"testing"

	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		in   string
		want ReviewVerdict
	}{
		{"APPROVE", ReviewApprove},
		{"approve", ReviewApprove},
		{" Approved ", ReviewApprove},
		{"LGTM", ReviewApprove},
		{"REQUEST_CHANGES", ReviewRequestChanges},
		{"request_changes", ReviewRequestChanges},
		{"REQUEST CHANGES", ReviewRequestChanges},
		{"changes_requested", ReviewRequestChanges},
		{"NEEDS_DISCUSSION", ReviewRequestChanges}, // unknown → safer default
		{"", ReviewRequestChanges},
		{"banana", ReviewRequestChanges},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ParseVerdict(tc.in); got != tc.want {
				t.Fatalf("ParseVerdict(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanSuggestions_TrimsAndDropsBlanks(t *testing.T) {
	got := cleanSuggestions([]string{"  one  ", "", "   ", "two", "\t\n"})
	want := []string{"one", "two"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestFormatReviewFeedback_ApproveStyleSummaryOnly(t *testing.T) {
	r := ReviewResult{
		Verdict: ReviewRequestChanges,
		Summary: "Looks good but needs tests.",
	}
	got := formatReviewFeedback(r)
	if !stringContains(got, "Looks good but needs tests.") {
		t.Fatalf("expected summary in feedback, got:\n%s", got)
	}
}

func TestFormatReviewFeedback_FallbackWhenEmpty(t *testing.T) {
	got := formatReviewFeedback(ReviewResult{Verdict: ReviewRequestChanges})
	if got == "" {
		t.Fatal("expected non-empty fallback feedback")
	}
}

// stubLLM is a tiny llm.Client fake used to drive ReviewCard / verifyCard
// without hitting the network.
type stubLLM struct {
	resp string
	err  error
}

func (s stubLLM) ChatCompletion(_ context.Context, _ llm.CompletionRequest) (string, error) {
	return s.resp, s.err
}

func (s stubLLM) ChatCompletionFull(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{Content: s.resp}, s.err
}

func TestReviewCard_ParsesApproveJSON(t *testing.T) {
	stub := stubLLM{resp: `{"verdict":"APPROVE","summary":"clean","suggestions":[]}`}
	got, err := ReviewCard(context.Background(), stub, "test-model", Card{Title: "t"}, "diff --git a b\n+x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Verdict != ReviewApprove {
		t.Fatalf("verdict = %q, want APPROVE", got.Verdict)
	}
	if got.Summary != "clean" {
		t.Fatalf("summary = %q, want %q", got.Summary, "clean")
	}
	if len(got.Suggestions) != 0 {
		t.Fatalf("suggestions = %#v, want empty", got.Suggestions)
	}
}

func TestReviewCard_ParsesRequestChangesWithSuggestions(t *testing.T) {
	stub := stubLLM{resp: "```json\n{\"verdict\":\"REQUEST_CHANGES\",\"summary\":\"missing tests\",\"suggestions\":[\"add a test\",\"\",\"  fix typo  \"]}\n```"}
	got, err := ReviewCard(context.Background(), stub, "m", Card{Title: "t"}, "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Verdict != ReviewRequestChanges {
		t.Fatalf("verdict = %q, want REQUEST_CHANGES", got.Verdict)
	}
	want := []string{"add a test", "fix typo"}
	if len(got.Suggestions) != len(want) {
		t.Fatalf("suggestions = %#v, want %#v", got.Suggestions, want)
	}
	for i, w := range want {
		if got.Suggestions[i] != w {
			t.Fatalf("suggestions = %#v, want %#v", got.Suggestions, want)
		}
	}
}

func TestReviewCard_EmptyDiffReturnsRequestChanges(t *testing.T) {
	got, err := ReviewCard(context.Background(), stubLLM{}, "m", Card{Title: "t"}, "  \n\t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Verdict != ReviewRequestChanges {
		t.Fatalf("verdict = %q, want REQUEST_CHANGES", got.Verdict)
	}
	if len(got.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion when diff is empty")
	}
}

func TestReviewCard_MissingModel(t *testing.T) {
	_, err := ReviewCard(context.Background(), stubLLM{resp: "{}"}, "", Card{Title: "t"}, "diff")
	if err == nil {
		t.Fatal("expected error when model is empty")
	}
}

func TestReviewCard_NilClient(t *testing.T) {
	_, err := ReviewCard(context.Background(), nil, "m", Card{Title: "t"}, "diff")
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
}

func TestReviewCard_LLMError(t *testing.T) {
	want := errors.New("boom")
	_, err := ReviewCard(context.Background(), stubLLM{err: want}, "m", Card{Title: "t"}, "diff")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped llm error, got %v", err)
	}
}

func TestVerifyAgentWork_NoCriteriaReturnsNil(t *testing.T) {
	got, err := VerifyAgentWork(context.Background(), stubLLM{}, "m", nil, "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil verdicts when no criteria, got %#v", got)
	}
}

func TestVerifyAgentWork_EmptyDiffFailsAll(t *testing.T) {
	got, err := VerifyAgentWork(context.Background(), stubLLM{}, "m", []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d verdicts, want 2", len(got))
	}
	for _, v := range got {
		if v.Met {
			t.Errorf("expected all verdicts unmet, got %#v", v)
		}
	}
}

func TestVerifyAgentWork_ParsesLLMVerdicts(t *testing.T) {
	stub := stubLLM{resp: `{"verdicts":[{"criterion":"a","met":true,"reason":"ok"},{"criterion":"b","met":false,"reason":"nope"}],"summary":"1/2"}`}
	got, err := VerifyAgentWork(context.Background(), stub, "m", []string{"a", "b"}, "diff --git a b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d verdicts, want 2", len(got))
	}
	if !got[0].Met || got[1].Met {
		t.Fatalf("unexpected verdict states: %#v", got)
	}
}

func TestVerifyAgentWork_NilClient(t *testing.T) {
	_, err := VerifyAgentWork(context.Background(), nil, "m", []string{"a"}, "diff")
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
}
