package github

import "time"

// CreatePRRequest is the payload for creating a pull request.
type CreatePRRequest struct {
	Head  string `json:"head"`
	Base  string `json:"base"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Draft bool   `json:"draft,omitempty"`
}

// MergeMethod is one of "merge", "squash", or "rebase".
type MergeMethod string

const (
	MergeMethodMerge  MergeMethod = "merge"
	MergeMethodSquash MergeMethod = "squash"
	MergeMethodRebase MergeMethod = "rebase"
)

// MergePRRequest is the payload for merging a pull request.
type MergePRRequest struct {
	CommitTitle   string      `json:"commit_title,omitempty"`
	CommitMessage string      `json:"commit_message,omitempty"`
	SHA           string      `json:"sha,omitempty"`
	MergeMethod   MergeMethod `json:"merge_method,omitempty"`
}

// PR is a created pull request.
type PR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
}

// PRState describes the current state of a pull request.
type PRState struct {
	Number int    `json:"number"`
	State  string `json:"state"` // "open" or "closed"
	Merged bool   `json:"merged"`
}

// CreateIssueRequest is the payload for creating an issue.
type CreateIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// Issue is a GitHub issue.
type Issue struct {
	Number    int       `json:"number"`
	HTMLURL   string    `json:"html_url"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    []Label   `json:"labels"`
	// PullRequest is set by GitHub when this issue is actually a pull request.
	PullRequest *struct {
		HTMLURL string `json:"html_url"`
	} `json:"pull_request,omitempty"`
}

// Label is a GitHub label.
type Label struct {
	Name string `json:"name"`
}

// ListIssuesOptions controls the ListIssues query.
type ListIssuesOptions struct {
	State      string // "open", "closed", "all"; default "open"
	PerPage    int    // default 30, max 100
	Page       int    // default 1
	ExcludePRs bool   // GitHub returns PRs in /issues; set to filter them out
	Labels     []string
	Sort       string // "created", "updated", "comments"
	Direction  string // "asc", "desc"
}
