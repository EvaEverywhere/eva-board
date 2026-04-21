package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CreateIssue creates a new issue in the given repository.
func (c *HTTPClient) CreateIssue(ctx context.Context, owner, repo string, req CreateIssueRequest) (*Issue, error) {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("issue title is required")
	}
	var issue Issue
	if err := c.do(ctx, "POST", prefix+"/issues", req, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// AddIssueComment posts a comment on the given issue or pull request number.
func (c *HTTPClient) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return err
	}
	if number <= 0 {
		return fmt.Errorf("issue number must be positive")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is required")
	}
	payload := map[string]string{"body": body}
	return c.do(ctx, "POST", fmt.Sprintf("%s/issues/%d/comments", prefix, number), payload, nil)
}

// ListIssues lists issues in a repository. When opts.ExcludePRs is true, the
// pull-requests that GitHub returns from this endpoint are filtered out.
func (c *HTTPClient) ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOptions) ([]Issue, error) {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	state := strings.TrimSpace(opts.State)
	if state == "" {
		state = "open"
	}
	q.Set("state", state)

	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 30
	}
	if perPage > 100 {
		perPage = 100
	}
	q.Set("per_page", strconv.Itoa(perPage))

	if opts.Page > 0 {
		q.Set("page", strconv.Itoa(opts.Page))
	}
	if len(opts.Labels) > 0 {
		q.Set("labels", strings.Join(opts.Labels, ","))
	}
	if s := strings.TrimSpace(opts.Sort); s != "" {
		q.Set("sort", s)
	}
	if d := strings.TrimSpace(opts.Direction); d != "" {
		q.Set("direction", d)
	}

	var issues []Issue
	if err := c.do(ctx, "GET", prefix+"/issues?"+q.Encode(), nil, &issues); err != nil {
		return nil, err
	}

	if !opts.ExcludePRs {
		return issues, nil
	}
	filtered := issues[:0]
	for _, i := range issues {
		if i.PullRequest != nil {
			continue
		}
		filtered = append(filtered, i)
	}
	return filtered, nil
}
