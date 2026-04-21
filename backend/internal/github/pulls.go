package github

import (
	"context"
	"fmt"
	"strings"
)

// CreatePR creates a new pull request.
func (c *HTTPClient) CreatePR(ctx context.Context, owner, repo string, req CreatePRRequest) (*PR, error) {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Head) == "" || strings.TrimSpace(req.Base) == "" || strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("head, base, and title are required")
	}
	var pr PR
	if err := c.do(ctx, "POST", prefix+"/pulls", req, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// MergePR merges a pull request using the requested merge method (defaults to "merge").
func (c *HTTPClient) MergePR(ctx context.Context, owner, repo string, number int, req MergePRRequest) error {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return err
	}
	if number <= 0 {
		return fmt.Errorf("pr number must be positive")
	}
	if req.MergeMethod == "" {
		req.MergeMethod = MergeMethodMerge
	}
	switch req.MergeMethod {
	case MergeMethodMerge, MergeMethodSquash, MergeMethodRebase:
	default:
		return fmt.Errorf("invalid merge method: %q", req.MergeMethod)
	}
	return c.do(ctx, "PUT", fmt.Sprintf("%s/pulls/%d/merge", prefix, number), req, nil)
}

// GetPRState fetches the current state (open/closed) and merged flag of a PR.
func (c *HTTPClient) GetPRState(ctx context.Context, owner, repo string, number int) (*PRState, error) {
	prefix, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if number <= 0 {
		return nil, fmt.Errorf("pr number must be positive")
	}
	var state PRState
	if err := c.do(ctx, "GET", fmt.Sprintf("%s/pulls/%d", prefix, number), nil, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
