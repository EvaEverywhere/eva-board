// Package board — spring clean flow.
//
// SpringCleanService audits a repository and proposes destructive
// cleanup actions for orphan branches, stale worktrees, and dead GitHub
// issues. AuditRepo is read-only. ApplyActions performs the
// user-approved destructive subset.
package board

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// CleanupActionType enumerates spring-clean destructive actions.
type CleanupActionType string

const (
	CleanupDeleteBranch   CleanupActionType = "delete_branch"
	CleanupRemoveWorktree CleanupActionType = "remove_worktree"
	CleanupCloseIssue     CleanupActionType = "close_issue"
)

// CleanupAction is a destructive action proposed by spring clean. The
// caller must surface these to the user and only pass approved actions
// to ApplyActions.
type CleanupAction struct {
	Type   CleanupActionType `json:"type"`
	Target string            `json:"target"` // branch name, worktree path, or "owner/repo#123"
	Reason string            `json:"reason"`
}

// SpringCleanConfig configures a SpringCleanService.
type SpringCleanConfig struct {
	// RepoOwner / RepoName identify the GitHub repo whose dead issues
	// may be proposed for closure.
	RepoOwner string
	RepoName  string
	// RepoPath is the local clone whose branches and worktrees are
	// audited. Required for branch / worktree audits.
	RepoPath string
	// BranchPrefix limits branch deletion proposals to refs that begin
	// with this prefix (e.g. "eva-board/"). Empty disables branch audit.
	BranchPrefix string
	// StaleAfter is the threshold above which worktrees are proposed for
	// removal. Defaults to 14 days when zero.
	StaleAfter time.Duration
	// DefaultBranch is the protected branch (default "main") that is
	// never proposed for deletion.
	DefaultBranch string
	// IssueStaleAfter is the threshold above which open GitHub issues
	// labelled "wontfix"/"stale" are proposed for closure. Defaults to
	// 90 days when zero.
	IssueStaleAfter time.Duration
}

// SpringCleanService inspects branches, worktrees, and GitHub issues and
// proposes cleanup actions.
type SpringCleanService struct {
	gh  github.Client
	cfg SpringCleanConfig
}

// NewSpringCleanService constructs a SpringCleanService.
func NewSpringCleanService(gh github.Client, cfg SpringCleanConfig) *SpringCleanService {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 14 * 24 * time.Hour
	}
	if cfg.IssueStaleAfter <= 0 {
		cfg.IssueStaleAfter = 90 * 24 * time.Hour
	}
	if strings.TrimSpace(cfg.DefaultBranch) == "" {
		cfg.DefaultBranch = "main"
	}
	return &SpringCleanService{gh: gh, cfg: cfg}
}

// AuditRepo runs read-only inspection and returns proposed cleanup
// actions. The caller is responsible for surfacing these to the user.
func (s *SpringCleanService) AuditRepo(ctx context.Context) ([]CleanupAction, error) {
	if s == nil {
		return nil, fmt.Errorf("spring clean service not configured")
	}
	out := []CleanupAction{}

	if strings.TrimSpace(s.cfg.RepoPath) != "" && strings.TrimSpace(s.cfg.BranchPrefix) != "" {
		branches, err := listOrphanBranches(ctx, s.cfg.RepoPath, s.cfg.BranchPrefix, s.cfg.DefaultBranch)
		if err != nil {
			return nil, fmt.Errorf("orphan branch audit: %w", err)
		}
		for _, b := range branches {
			out = append(out, CleanupAction{
				Type:   CleanupDeleteBranch,
				Target: b,
				Reason: fmt.Sprintf("merged into %s and prefixed with %q", s.cfg.DefaultBranch, s.cfg.BranchPrefix),
			})
		}
	}

	if strings.TrimSpace(s.cfg.RepoPath) != "" {
		stale, err := listStaleWorktrees(ctx, s.cfg.RepoPath, s.cfg.StaleAfter)
		if err != nil {
			return nil, fmt.Errorf("stale worktree audit: %w", err)
		}
		for _, w := range stale {
			out = append(out, CleanupAction{
				Type:   CleanupRemoveWorktree,
				Target: w.Path,
				Reason: fmt.Sprintf("worktree idle for %s (clean)", w.Age.Round(time.Hour)),
			})
		}
	}

	if s.gh != nil && strings.TrimSpace(s.cfg.RepoOwner) != "" && strings.TrimSpace(s.cfg.RepoName) != "" {
		dead, err := listDeadIssues(ctx, s.gh, s.cfg.RepoOwner, s.cfg.RepoName, s.cfg.IssueStaleAfter)
		if err != nil {
			return nil, fmt.Errorf("dead issue audit: %w", err)
		}
		for _, iss := range dead {
			out = append(out, CleanupAction{
				Type:   CleanupCloseIssue,
				Target: fmt.Sprintf("%s/%s#%d", s.cfg.RepoOwner, s.cfg.RepoName, iss.Number),
				Reason: iss.Reason,
			})
		}
	}

	return out, nil
}

// ApplyActions applies user-approved cleanup actions. The caller MUST
// filter to the approved subset before calling.
func (s *SpringCleanService) ApplyActions(ctx context.Context, actions []CleanupAction) error {
	if s == nil {
		return fmt.Errorf("spring clean service not configured")
	}
	for _, a := range actions {
		switch a.Type {
		case CleanupDeleteBranch:
			if isProtectedBranch(a.Target, s.cfg.DefaultBranch) {
				return fmt.Errorf("refusing to delete protected branch %q", a.Target)
			}
			if strings.TrimSpace(s.cfg.BranchPrefix) != "" && !strings.HasPrefix(a.Target, s.cfg.BranchPrefix) {
				return fmt.Errorf("branch %q does not match configured prefix %q", a.Target, s.cfg.BranchPrefix)
			}
			if err := deleteLocalBranch(ctx, s.cfg.RepoPath, a.Target); err != nil {
				return fmt.Errorf("delete branch %q: %w", a.Target, err)
			}
		case CleanupRemoveWorktree:
			if err := removeWorktree(ctx, s.cfg.RepoPath, a.Target); err != nil {
				return fmt.Errorf("remove worktree %q: %w", a.Target, err)
			}
		case CleanupCloseIssue:
			if s.gh == nil {
				return fmt.Errorf("close_issue requires a github client")
			}
			owner, repo, number, err := parseIssueTarget(a.Target)
			if err != nil {
				return fmt.Errorf("close issue %q: %w", a.Target, err)
			}
			if err := s.gh.CloseIssue(ctx, owner, repo, number); err != nil {
				return fmt.Errorf("close issue %s: %w", a.Target, err)
			}
		default:
			return fmt.Errorf("unknown cleanup action type %q", a.Type)
		}
	}
	return nil
}

func isProtectedBranch(name, defaultBranch string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return true
	}
	if b := strings.TrimSpace(defaultBranch); b != "" && strings.EqualFold(n, b) {
		return true
	}
	switch strings.ToLower(n) {
	case "main", "master", "develop", "development", "head":
		return true
	}
	return false
}

func parseIssueTarget(target string) (owner, repo string, number int, err error) {
	target = strings.TrimSpace(target)
	hash := strings.LastIndex(target, "#")
	if hash <= 0 || hash == len(target)-1 {
		return "", "", 0, fmt.Errorf("expected owner/repo#NUMBER, got %q", target)
	}
	repoFull := target[:hash]
	numStr := target[hash+1:]
	slash := strings.Index(repoFull, "/")
	if slash <= 0 || slash == len(repoFull)-1 {
		return "", "", 0, fmt.Errorf("expected owner/repo#NUMBER, got %q", target)
	}
	if _, err := fmt.Sscanf(numStr, "%d", &number); err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("invalid issue number in %q", target)
	}
	return repoFull[:slash], repoFull[slash+1:], number, nil
}

// listOrphanBranches returns branches matching prefix that are merged
// into defaultBranch (so safe to delete).
func listOrphanBranches(ctx context.Context, repoPath, prefix, defaultBranch string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--merged", defaultBranch, "--format=%(refname:short)").Output()
	if err != nil {
		return nil, fmt.Errorf("git branch --merged: %w", err)
	}
	var orphans []string
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if name == "" {
			continue
		}
		if isProtectedBranch(name, defaultBranch) {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		orphans = append(orphans, name)
	}
	return orphans, nil
}

type staleWorktree struct {
	Path string
	Age  time.Duration
}

// listStaleWorktrees returns worktrees whose latest commit is older than
// staleAfter and which have no uncommitted changes.
func listStaleWorktrees(ctx context.Context, mainRepoPath string, staleAfter time.Duration) ([]staleWorktree, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", mainRepoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	mainClean := filepath.Clean(strings.TrimSpace(mainRepoPath))
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			p := filepath.Clean(strings.TrimSpace(strings.TrimPrefix(line, "worktree ")))
			if p == "" || p == mainClean {
				continue
			}
			paths = append(paths, p)
		}
	}

	var stale []staleWorktree
	for _, p := range paths {
		// Skip dirty worktrees — never propose destructive cleanup
		// when uncommitted work could be lost.
		statusOut, err := exec.CommandContext(ctx, "git", "-C", p, "status", "--porcelain").Output()
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(statusOut)) != "" {
			continue
		}
		commitOut, err := exec.CommandContext(ctx, "git", "-C", p, "log", "-1", "--format=%ct").Output()
		if err != nil {
			continue
		}
		var ts int64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(commitOut)), "%d", &ts); err != nil {
			continue
		}
		age := time.Since(time.Unix(ts, 0))
		if age >= staleAfter {
			stale = append(stale, staleWorktree{Path: p, Age: age})
		}
	}
	return stale, nil
}

func deleteLocalBranch(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "-d", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		// fall back to -D for already-merged-but-not-on-current-branch cases
		cmd2 := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "-D", branch)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("%s: %w (initial: %s)", strings.TrimSpace(string(out2)), err2, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func removeWorktree(ctx context.Context, mainRepoPath, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", mainRepoPath, "worktree", "remove", worktreePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = exec.CommandContext(ctx, "git", "-C", mainRepoPath, "worktree", "prune").Run()
	return nil
}

type deadIssue struct {
	Number int
	Reason string
}

// listDeadIssues finds open issues that look abandoned: labelled "stale"
// or "wontfix", or unupdated for longer than staleAfter.
func listDeadIssues(ctx context.Context, gh github.Client, owner, repo string, staleAfter time.Duration) ([]deadIssue, error) {
	issues, err := gh.ListIssues(ctx, owner, repo, github.ListIssuesOptions{
		State:      "open",
		PerPage:    100,
		ExcludePRs: true,
		Sort:       "updated",
		Direction:  "asc",
	})
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-staleAfter)
	var out []deadIssue
	for _, iss := range issues {
		if reason, dead := classifyDeadIssue(iss, cutoff); dead {
			out = append(out, deadIssue{Number: iss.Number, Reason: reason})
		}
	}
	return out, nil
}

func classifyDeadIssue(iss github.Issue, cutoff time.Time) (string, bool) {
	for _, label := range iss.Labels {
		switch strings.ToLower(strings.TrimSpace(label.Name)) {
		case "wontfix", "won't fix", "stale", "obsolete", "duplicate":
			return fmt.Sprintf("labelled %q", label.Name), true
		}
	}
	if !iss.UpdatedAt.IsZero() && iss.UpdatedAt.Before(cutoff) {
		return fmt.Sprintf("no activity since %s", iss.UpdatedAt.Format("2006-01-02")), true
	}
	return "", false
}
