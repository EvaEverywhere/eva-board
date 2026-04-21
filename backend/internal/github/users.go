package github

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// User describes the authenticated user (subset of GitHub's /user payload).
type User struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Repo is a repository the authenticated user can access. Only the fields
// the board UI's repo picker needs are decoded.
type Repo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// ListUserReposOptions controls the GET /user/repos query.
type ListUserReposOptions struct {
	Affiliation string // e.g. "owner,collaborator,organization_member"
	Visibility  string // "all", "public", "private"
	Sort        string // "created", "updated", "pushed", "full_name"
	Direction   string // "asc", "desc"
	PerPage     int    // default 30, max 100
	Page        int    // default 1
}

// GetUser calls GET /user, returning the authenticated user. Useful as a
// "ping" to validate a personal access token.
func (c *HTTPClient) GetUser(ctx context.Context) (*User, error) {
	var u User
	if err := c.do(ctx, "GET", "/user", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUserRepos calls GET /user/repos. The board settings handler uses
// this to populate the repo picker after a token has been saved.
func (c *HTTPClient) ListUserRepos(ctx context.Context, opts ListUserReposOptions) ([]Repo, error) {
	q := url.Values{}
	if a := strings.TrimSpace(opts.Affiliation); a != "" {
		q.Set("affiliation", a)
	}
	if v := strings.TrimSpace(opts.Visibility); v != "" {
		q.Set("visibility", v)
	}
	sort := strings.TrimSpace(opts.Sort)
	if sort == "" {
		sort = "updated"
	}
	q.Set("sort", sort)
	if d := strings.TrimSpace(opts.Direction); d != "" {
		q.Set("direction", d)
	}

	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 100
	}
	if perPage > 100 {
		perPage = 100
	}
	q.Set("per_page", strconv.Itoa(perPage))
	if opts.Page > 0 {
		q.Set("page", strconv.Itoa(opts.Page))
	}

	var repos []Repo
	if err := c.do(ctx, "GET", "/user/repos?"+q.Encode(), nil, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}
