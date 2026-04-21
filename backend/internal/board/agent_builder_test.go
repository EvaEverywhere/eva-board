package board

// TestManagerSignatureIncludesRepo verifies that two distinct repos
// produce distinct manager signatures so the AgentRegistry treats them
// as separate cache entries. Without the repo dimension the registry
// would collapse all of a user's boards onto a single AgentManager,
// regressing the multi-repo isolation guarantee the data-model PR is
// adding.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
)

func TestManagerSignature_IncludesRepoIdentity(t *testing.T) {
	repoA := &Repo{
		ID:            uuid.New(),
		Owner:         "acme",
		Name:          "alpha",
		RepoPath:      "/tmp/alpha",
		DefaultBranch: "main",
	}
	repoB := &Repo{
		ID:            uuid.New(),
		Owner:         "acme",
		Name:          "beta",
		RepoPath:      "/tmp/beta",
		DefaultBranch: "main",
	}
	st := Settings{MaxVerifyIterations: 3, MaxReviewCycles: 5}
	cfg := codegen.Config{Type: "claude-code"}

	if managerSignature(repoA, st, cfg) == managerSignature(repoB, st, cfg) {
		t.Fatalf("expected distinct signatures for different repos")
	}
}

func TestManagerSignature_StableForSameInputs(t *testing.T) {
	repo := &Repo{
		ID:            uuid.New(),
		Owner:         "acme",
		Name:          "x",
		RepoPath:      "/tmp/x",
		DefaultBranch: "main",
	}
	st := Settings{MaxVerifyIterations: 3, MaxReviewCycles: 5}
	cfg := codegen.Config{Type: "claude-code", Args: []string{"--a", "--b"}}

	if managerSignature(repo, st, cfg) != managerSignature(repo, st, cfg) {
		t.Fatalf("expected stable signature for identical inputs")
	}
}
