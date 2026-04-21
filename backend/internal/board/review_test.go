package board

import (
	"context"
	"errors"
	"testing"
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

func TestReviewCard_ParsesApproveJSON(t *testing.T) {
	fc := &fakeCodegen{reviewerOutputs: []string{
		`{"verdict":"APPROVE","summary":"clean","suggestions":[]}`,
	}}
	got, err := ReviewCard(context.Background(), fc, Card{Title: "t"}, "/tmp")
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
	fenced := "```json\n{\"verdict\":\"REQUEST_CHANGES\",\"summary\":\"missing tests\",\"suggestions\":[\"add a test\",\"\",\"  fix typo  \"]}\n```"
	fc := &fakeCodegen{reviewerOutputs: []string{fenced}}
	got, err := ReviewCard(context.Background(), fc, Card{Title: "t"}, "/tmp")
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

func TestReviewCard_NilAgent(t *testing.T) {
	_, err := ReviewCard(context.Background(), nil, Card{Title: "t"}, "/tmp")
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestReviewCard_AgentError(t *testing.T) {
	want := errors.New("boom")
	fc := &fakeCodegen{runErr: want}
	_, err := ReviewCard(context.Background(), fc, Card{Title: "t"}, "/tmp")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped agent error, got %v", err)
	}
}
