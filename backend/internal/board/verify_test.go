package board

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseAcceptanceCriteria(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty description",
			in:   "",
			want: []string{},
		},
		{
			name: "no checkboxes",
			in:   "Just a paragraph with no checklist.\n",
			want: []string{},
		},
		{
			name: "single unchecked",
			in:   "- [ ] Implement the thing\n",
			want: []string{"Implement the thing"},
		},
		{
			name: "single checked",
			in:   "- [x] Already done\n",
			want: []string{"Already done"},
		},
		{
			name: "checked uppercase X",
			in:   "- [X] Mixed case checkbox\n",
			want: []string{"Mixed case checkbox"},
		},
		{
			name: "mixed bullets and states",
			in: "Description first.\n\n" +
				"- [ ] Do A\n" +
				"* [x] Do B\n" +
				"- [ ] Do C with **bold**\n" +
				"Some prose between.\n" +
				"- [X] Do D\n",
			want: []string{"Do A", "Do B", "Do C with **bold**", "Do D"},
		},
		{
			name: "ignores blank-text checkboxes",
			in:   "- [ ] \n- [x]   \n- [ ] real one\n",
			want: []string{"real one"},
		},
		{
			name: "ignores indented checkboxes (sub-items handled via document order, top-level only)",
			in:   "  - [ ] indented\n- [ ] top-level\n",
			want: []string{"top-level"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseAcceptanceCriteria(tc.in)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseAcceptanceCriteriaDetailed_PreservesCheckedState(t *testing.T) {
	in := "- [ ] one\n- [x] two\n- [X] three\n"
	got := ParseAcceptanceCriteriaDetailed(in)
	want := []AcceptanceCriterion{
		{Text: "one", Checked: false},
		{Text: "two", Checked: true},
		{Text: "three", Checked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestMakeAllFail_AssignsReasonToEveryCriterion(t *testing.T) {
	criteria := []string{"a", "b", "c"}
	verdicts := makeAllFail(criteria, "no diff")
	if len(verdicts) != 3 {
		t.Fatalf("expected 3 verdicts, got %d", len(verdicts))
	}
	for i, v := range verdicts {
		if v.Met {
			t.Errorf("verdict %d met=true, want false", i)
		}
		if v.Reason != "no diff" {
			t.Errorf("verdict %d reason=%q, want %q", i, v.Reason, "no diff")
		}
		if v.Criterion != criteria[i] {
			t.Errorf("verdict %d criterion=%q, want %q", i, v.Criterion, criteria[i])
		}
	}
}

func TestSummarizeVerdicts(t *testing.T) {
	cases := []struct {
		name string
		in   []CriterionResult
		want string
	}{
		{name: "empty", in: nil, want: "No criteria were evaluated."},
		{name: "all pass", in: []CriterionResult{{Met: true}, {Met: true}}, want: "2 of 2 acceptance criteria met."},
		{name: "mixed", in: []CriterionResult{{Met: true}, {Met: false}, {Met: true}}, want: "2 of 3 acceptance criteria met."},
		{name: "all fail", in: []CriterionResult{{Met: false}, {Met: false}}, want: "0 of 2 acceptance criteria met."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarizeVerdicts(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatFailedCriteriaFeedback_OnlyIncludesFailures(t *testing.T) {
	verdicts := []CriterionResult{
		{Criterion: "passes", Met: true, Reason: "ok"},
		{Criterion: "missing test", Met: false, Reason: "no _test.go file"},
		{Criterion: "missing handler", Met: false, Reason: "handler not wired"},
	}
	got := formatFailedCriteriaFeedback(verdicts)
	if contains := "passes"; stringContains(got, contains) {
		t.Errorf("feedback should not mention passing criterion, got:\n%s", got)
	}
	for _, want := range []string{"missing test", "no _test.go file", "missing handler", "handler not wired"} {
		if !stringContains(got, want) {
			t.Errorf("feedback missing %q, got:\n%s", want, got)
		}
	}
}

func TestVerifyAgentWork_NoCriteriaReturnsNil(t *testing.T) {
	got, err := VerifyAgentWork(context.Background(), &fakeCodegen{}, nil, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil verdicts when no criteria, got %#v", got)
	}
}

func TestVerifyAgentWork_NilAgent(t *testing.T) {
	_, err := VerifyAgentWork(context.Background(), nil, []string{"a"}, "/tmp")
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestVerifyAgentWork_ParsesAgentVerdicts(t *testing.T) {
	fc := &fakeCodegen{
		reviewerOutputs: []string{
			`{"results":[{"criterion":"a","met":true,"reason":"ok"},{"criterion":"b","met":false,"reason":"nope"}],"summary":"1/2"}`,
		},
	}
	got, err := VerifyAgentWork(context.Background(), fc, []string{"a", "b"}, "/tmp")
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

func TestVerifyAgentWork_AgentError(t *testing.T) {
	want := errors.New("boom")
	fc := &fakeCodegen{runErr: want}
	_, err := VerifyAgentWork(context.Background(), fc, []string{"a"}, "/tmp")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped agent error, got %v", err)
	}
}

func stringContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (needle == "" || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
