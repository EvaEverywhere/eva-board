package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// interfaceCheck is a compile-time assertion that *HTTPClient satisfies Client.
var _ Client = (*HTTPClient)(nil)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*HTTPClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(Options{
		Token:   "test-token",
		BaseURL: srv.URL,
	})
	return c, srv
}

func TestSetHeadersAndAuth(t *testing.T) {
	var gotAuth, gotAccept, gotUA, gotCT, gotAPI string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		gotAPI = r.Header.Get("X-GitHub-Api-Version")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(PR{Number: 1})
	})
	_, err := c.CreatePR(context.Background(), "owner", "repo", CreatePRRequest{
		Head: "feature", Base: "main", Title: "t",
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA == "" {
		t.Errorf("User-Agent must be set")
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotAPI != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", gotAPI)
	}
}

func TestCreatePR(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/repos/own/rep/pulls" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body CreatePRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Head != "feat" || body.Base != "main" || body.Title != "T" {
			t.Errorf("unexpected payload: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number": 42, "html_url": "https://example.com/pr/42", "state": "open"}`)
	})
	pr, err := c.CreatePR(context.Background(), "own", "rep", CreatePRRequest{Head: "feat", Base: "main", Title: "T"})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if pr.Number != 42 || pr.HTMLURL != "https://example.com/pr/42" {
		t.Errorf("unexpected PR: %+v", pr)
	}
}

func TestCreatePRValidation(t *testing.T) {
	c := New(Options{Token: "x"})
	if _, err := c.CreatePR(context.Background(), "", "rep", CreatePRRequest{Head: "h", Base: "b", Title: "t"}); err == nil {
		t.Error("expected error for missing owner")
	}
	if _, err := c.CreatePR(context.Background(), "o", "r", CreatePRRequest{Base: "b", Title: "t"}); err == nil {
		t.Error("expected error for missing head")
	}
}

func TestMergePR(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/repos/o/r/pulls/7/merge" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body MergePRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.MergeMethod != MergeMethodSquash {
			t.Errorf("merge_method = %q, want squash", body.MergeMethod)
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.MergePR(context.Background(), "o", "r", 7, MergePRRequest{MergeMethod: MergeMethodSquash}); err != nil {
		t.Fatalf("MergePR: %v", err)
	}
}

func TestMergePRDefaultMethod(t *testing.T) {
	var gotMethod MergeMethod
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body MergePRRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMethod = body.MergeMethod
		w.WriteHeader(http.StatusOK)
	})
	if err := c.MergePR(context.Background(), "o", "r", 1, MergePRRequest{}); err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if gotMethod != MergeMethodMerge {
		t.Errorf("default merge method = %q, want %q", gotMethod, MergeMethodMerge)
	}
}

func TestMergePRInvalidMethod(t *testing.T) {
	c := New(Options{Token: "x"})
	err := c.MergePR(context.Background(), "o", "r", 1, MergePRRequest{MergeMethod: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "invalid merge method") {
		t.Errorf("expected invalid merge method error, got %v", err)
	}
}

func TestGetPRState(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/repos/o/r/pulls/3" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"number": 3, "state": "closed", "merged": true}`)
	})
	st, err := c.GetPRState(context.Background(), "o", "r", 3)
	if err != nil {
		t.Fatalf("GetPRState: %v", err)
	}
	if st.Number != 3 || st.State != "closed" || !st.Merged {
		t.Errorf("unexpected state: %+v", st)
	}
}

func TestCreateIssue(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body CreateIssueRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Title != "Bug" || len(body.Labels) != 1 || body.Labels[0] != "bug" {
			t.Errorf("unexpected payload: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number": 9, "html_url": "https://example.com/i/9", "title": "Bug"}`)
	})
	is, err := c.CreateIssue(context.Background(), "o", "r", CreateIssueRequest{Title: "Bug", Body: "B", Labels: []string{"bug"}})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if is.Number != 9 || is.Title != "Bug" {
		t.Errorf("unexpected issue: %+v", is)
	}
}

func TestAddIssueComment(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/12/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["body"] != "hello" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusCreated)
	})
	if err := c.AddIssueComment(context.Background(), "o", "r", 12, "hello"); err != nil {
		t.Fatalf("AddIssueComment: %v", err)
	}
}

func TestAddIssueCommentValidation(t *testing.T) {
	c := New(Options{Token: "x"})
	if err := c.AddIssueComment(context.Background(), "o", "r", 12, "  "); err == nil {
		t.Error("expected error for empty body")
	}
	if err := c.AddIssueComment(context.Background(), "o", "r", 0, "hi"); err == nil {
		t.Error("expected error for invalid number")
	}
}

func TestListIssuesExcludesPRs(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("state") != "open" {
			t.Errorf("state = %s", q.Get("state"))
		}
		if q.Get("per_page") != "50" {
			t.Errorf("per_page = %s", q.Get("per_page"))
		}
		if q.Get("labels") != "bug,p1" {
			t.Errorf("labels = %s", q.Get("labels"))
		}
		_, _ = io.WriteString(w, `[
            {"number": 1, "title": "issue"},
            {"number": 2, "title": "pr", "pull_request": {"html_url": "x"}}
        ]`)
	})
	issues, err := c.ListIssues(context.Background(), "o", "r", ListIssuesOptions{
		PerPage:    50,
		Labels:     []string{"bug", "p1"},
		ExcludePRs: true,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Errorf("unexpected issues: %+v", issues)
	}
}

func TestHTTPErrorOnNon2xx(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"Validation Failed"}`)
	})
	_, err := c.GetPRState(context.Background(), "o", "r", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if herr.StatusCode != 422 {
		t.Errorf("status = %d", herr.StatusCode)
	}
}

func TestVerifySignature(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := SignaturePrefix + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature(secret, body, sig); err != nil {
		t.Errorf("VerifySignature: %v", err)
	}

	if err := VerifySignature(secret, body, SignaturePrefix+"deadbeef"); err == nil {
		t.Error("expected mismatch error")
	}
	if err := VerifySignature(secret, body, "sha1=abc"); err == nil {
		t.Error("expected prefix error")
	}
	if err := VerifySignature("", body, sig); err == nil {
		t.Error("expected missing secret error")
	}
	if err := VerifySignature(secret, body, ""); err == nil {
		t.Error("expected missing signature error")
	}
}

func TestParseEvent(t *testing.T) {
	secret := "s"
	body := []byte(`{"action":"opened","number":7}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := SignaturePrefix + hex.EncodeToString(mac.Sum(nil))

	ev, err := ParseEvent(secret, body, "pull_request", "delivery-1", sig)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if ev.Type != "pull_request" || ev.DeliveryID != "delivery-1" {
		t.Errorf("unexpected event: %+v", ev)
	}
	if ev.Payload["action"] != "opened" {
		t.Errorf("payload action = %v", ev.Payload["action"])
	}

	if _, err := ParseEvent(secret, body, "", "d", sig); err == nil {
		t.Error("expected missing event type error")
	}
}
