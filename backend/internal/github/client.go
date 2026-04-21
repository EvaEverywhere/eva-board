// Package github provides a focused HTTP client for the GitHub REST API,
// covering the operations the board agent needs: pull requests, issues,
// and webhook signature verification.
package github

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

// DefaultBaseURL is the default GitHub REST API base URL.
const DefaultBaseURL = "https://api.github.com"

// Client is the public surface of the GitHub client. It is defined as an
// interface so callers can substitute fakes in tests.
type Client interface {
	CreatePR(ctx context.Context, owner, repo string, req CreatePRRequest) (*PR, error)
	MergePR(ctx context.Context, owner, repo string, number int, req MergePRRequest) error
	GetPRState(ctx context.Context, owner, repo string, number int) (*PRState, error)
	CreateIssue(ctx context.Context, owner, repo string, req CreateIssueRequest) (*Issue, error)
	AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOptions) ([]Issue, error)
}

// Options configure a new client.
type Options struct {
	// Token is the GitHub personal access token (or installation token).
	// Required for any authenticated calls.
	Token string
	// BaseURL overrides DefaultBaseURL. Useful for tests or GitHub Enterprise.
	BaseURL string
	// HTTPClient overrides the default *http.Client.
	HTTPClient *http.Client
	// UserAgent sets the User-Agent header. GitHub requires one.
	UserAgent string
}

// HTTPClient is the concrete implementation of Client.
type HTTPClient struct {
	token     string
	baseURL   string
	userAgent string
	http      *http.Client
}

// New constructs a new HTTPClient.
func New(opts Options) *HTTPClient {
	base := strings.TrimSpace(opts.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = "eva-board-github-client"
	}

	return &HTTPClient{
		token:     strings.TrimSpace(opts.Token),
		baseURL:   base,
		userAgent: ua,
		http:      hc,
	}
}

// HTTPError is returned when GitHub responds with a non-2xx status.
type HTTPError struct {
	StatusCode int
	Body       string
	Method     string
	URL        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github api %s %s: status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// do executes a JSON request against the GitHub API. If body is non-nil it is
// JSON-encoded; if out is non-nil the response body is decoded into it.
func (c *HTTPClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(errBody),
			Method:     method,
			URL:        fullURL,
		}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode github response: %w", err)
		}
	}
	return nil
}

func (c *HTTPClient) setHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)
}

// repoPath builds a "/repos/{owner}/{repo}" prefix, trimming surrounding space.
func repoPath(owner, repo string) (string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return "", fmt.Errorf("owner and repo are required")
	}
	return "/repos/" + owner + "/" + repo, nil
}
