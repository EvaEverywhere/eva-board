package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletionFull_RequestAndResponse(t *testing.T) {
	var captured CompletionRequest
	var authHeader, contentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		authHeader = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "  hello world  "}}],
			"usage": {"prompt_tokens": 11, "completion_tokens": 3}
		}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL)
	resp, err := c.ChatCompletionFull(context.Background(), CompletionRequest{
		Model:       "openai/gpt-4o-mini",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		MaxTokens:   42,
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("ChatCompletionFull error: %v", err)
	}

	if resp.Content != "hello world" {
		t.Errorf("content not trimmed, got %q", resp.Content)
	}
	if resp.PromptTokens != 11 || resp.OutputTokens != 3 {
		t.Errorf("usage not parsed: %+v", resp)
	}
	if authHeader != "Bearer test-key" {
		t.Errorf("missing/incorrect auth header: %q", authHeader)
	}
	if contentType != "application/json" {
		t.Errorf("missing/incorrect content-type: %q", contentType)
	}
	if captured.Model != "openai/gpt-4o-mini" {
		t.Errorf("model not forwarded, got %q", captured.Model)
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Content != "hi" {
		t.Errorf("messages not forwarded: %+v", captured.Messages)
	}
	if captured.MaxTokens != 42 || captured.Temperature != 0.5 {
		t.Errorf("params not forwarded: %+v", captured)
	}
}

func TestChatCompletion_ReturnsContentOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient("k", srv.URL)
	got, err := c.ChatCompletion(context.Background(), CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want ok", got)
	}
}

func TestChatCompletion_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := NewClient("k", srv.URL)
	_, err := c.ChatCompletion(context.Background(), CompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error missing status/body: %v", err)
	}
}

func TestChatCompletion_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := NewClient("k", srv.URL)
	_, err := c.ChatCompletion(context.Background(), CompletionRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected no-choices error, got %v", err)
	}
}

func TestNewClient_DefaultBaseURL(t *testing.T) {
	c := NewClient("k", "")
	if c.baseURL != DefaultBaseURL {
		t.Errorf("default base URL not applied: %q", c.baseURL)
	}
	c2 := NewClient("k", "https://example.com/v1/")
	if c2.baseURL != "https://example.com/v1" {
		t.Errorf("trailing slash not trimmed: %q", c2.baseURL)
	}
}

func TestCleanJSON(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"```\n{\"a\":1}\n```":     `{"a":1}`,
		"   {\"a\":1}   ":         `{"a":1}`,
		"```JSON\n{}\n```":        `{}`,
	}
	for in, want := range cases {
		if got := CleanJSON(in); got != want {
			t.Errorf("CleanJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

// Ensure HTTPClient satisfies the Client interface.
var _ Client = (*HTTPClient)(nil)
