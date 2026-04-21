// Package llm provides a minimal client for OpenAI-compatible chat completion
// APIs (primarily OpenRouter). It is intentionally free of cross-cutting
// concerns such as metering, user attribution, or model selection policy so it
// can be embedded in any service that needs raw LLM access.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is used when no base URL is configured.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// Client is the contract for a chat-completion capable LLM client. Callers
// should depend on this interface rather than the concrete type so tests can
// swap in a fake.
type Client interface {
	ChatCompletion(ctx context.Context, req CompletionRequest) (string, error)
	ChatCompletionFull(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// HTTPClient is an OpenAI-compatible chat completions client.
type HTTPClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewClient constructs an HTTPClient. If baseURL is empty, DefaultBaseURL is
// used.
func NewClient(apiKey, baseURL string) *HTTPClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &HTTPClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// ChatCompletion returns just the assistant content string for the given
// request, discarding token usage information.
func (c *HTTPClient) ChatCompletion(ctx context.Context, req CompletionRequest) (string, error) {
	resp, err := c.ChatCompletionFull(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// ChatCompletionFull issues the chat completion request and returns the parsed
// content alongside reported token usage.
func (c *HTTPClient) ChatCompletionFull(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshal llm request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("create llm request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("read llm response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("llm returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return CompletionResponse{}, fmt.Errorf("unmarshal llm response: %w", err)
	}
	if len(result.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("llm returned no choices")
	}

	return CompletionResponse{
		Content:      strings.TrimSpace(result.Choices[0].Message.Content),
		PromptTokens: result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}, nil
}

// CleanJSON strips markdown JSON fences (```json ... ``` or ``` ... ```) from
// LLM output so the caller can json.Unmarshal it directly.
func CleanJSON(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "```json") {
		s = s[7:]
	} else if strings.HasPrefix(lower, "```") {
		s = s[3:]
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}
