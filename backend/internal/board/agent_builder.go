package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/teslashibe/codegen-go"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// AgentBuilderDeps groups the singleton dependencies the production
// ManagerBuilder needs. CodegenDefaults captures the server-level
// CODEGEN_* env values; per-user settings (Settings.CodegenAgent /
// CodegenCommand / CodegenArgs) override these when non-empty.
// SharedCodegen is the agent built from the defaults at startup; it is
// reused when the user has not changed any codegen field, avoiding a
// fresh process per request for the common case.
type AgentBuilderDeps struct {
	Cards           cardStore
	Repos           *ReposService
	Settings        *SettingsService
	GitHubClient    github.ClientFactory
	CodegenDefaults codegen.Config
	SharedCodegen   codegen.Agent
	// CodegenFactory builds a fresh codegen.Agent when per-user
	// overrides differ from the defaults. Defaults to codegen.NewAgent
	// when nil.
	CodegenFactory func(codegen.Config) (codegen.Agent, error)
}

// NewProductionManagerBuilder returns a ManagerBuilder closure suitable
// for the AgentRegistry. It loads the user's settings, decrypts the
// GitHub token, resolves the codegen agent (server defaults overlaid
// with per-user overrides), constructs an AgentManager, and returns a
// settings signature for cache invalidation.
//
// The signature is a sha256 of repo + retry caps + resolved codegen
// fields. It does not need to be cryptographic — it is purely a change
// detector. The GitHub token is intentionally NOT included so that
// rotating a token does not invalidate a running agent.
func NewProductionManagerBuilder(deps AgentBuilderDeps) ManagerBuilder {
	if deps.CodegenFactory == nil {
		deps.CodegenFactory = codegen.NewAgent
	}
	return func(ctx context.Context, userID, repoID uuid.UUID) (*AgentManager, string, error) {
		if deps.Settings == nil || deps.GitHubClient == nil || deps.Cards == nil || deps.Repos == nil {
			return nil, "", apperrors.New(http.StatusServiceUnavailable, "board agent is not configured on this server")
		}
		repo, err := deps.Repos.Get(ctx, userID, repoID)
		if err != nil {
			return nil, "", apperrors.New(http.StatusBadRequest, "board repo not found for user")
		}
		if repo.Owner == "" || repo.Name == "" || repo.RepoPath == "" {
			return nil, "", apperrors.New(http.StatusBadRequest, "board repo incomplete: owner, name, and repo_path are required")
		}
		st, err := deps.Settings.Get(ctx, userID)
		if err != nil {
			return nil, "", err
		}
		token, err := deps.Settings.GitHubToken(ctx, userID)
		if err != nil {
			return nil, "", mapSettingsError(err)
		}
		code, codeCfg, err := ResolveCodegenAgent(st, deps.CodegenDefaults, deps.SharedCodegen, deps.CodegenFactory)
		if err != nil {
			return nil, "", apperrors.New(http.StatusBadRequest, "invalid codegen configuration: "+err.Error())
		}
		gh := deps.GitHubClient.NewClient(token)
		baseBranch := repo.DefaultBranch
		if baseBranch == "" {
			baseBranch = "main"
		}
		cfg := AgentConfig{
			RepoOwner:           repo.Owner,
			RepoName:            repo.Name,
			RepoPath:            repo.RepoPath,
			BaseBranch:          baseBranch,
			MaxVerifyIterations: st.MaxVerifyIterations,
			MaxReviewCycles:     st.MaxReviewCycles,
			GitHubToken:         token,
		}
		mgr := NewAgentManager(deps.Cards, code, gh, cfg)
		sig := managerSignature(repo, st, codeCfg)
		return mgr, sig, nil
	}
}

// ResolveCodegenAgent layers per-user overrides onto the server
// defaults and returns both the agent and the resolved codegen.Config
// (so the caller can fold the config into a cache signature). Per-user
// values win when non-empty; otherwise the server-level CODEGEN_*
// defaults apply. If neither side has set anything new (the resolved
// Config matches the server defaults exactly), shared is reused to
// avoid spinning up a new agent per request.
func ResolveCodegenAgent(
	st Settings,
	defaults codegen.Config,
	shared codegen.Agent,
	factory func(codegen.Config) (codegen.Agent, error),
) (codegen.Agent, codegen.Config, error) {
	cfg := defaults
	if v := strings.TrimSpace(st.CodegenAgent); v != "" {
		cfg.Type = v
	}
	if v := strings.TrimSpace(st.CodegenCommand); v != "" {
		cfg.Command = v
	}
	if len(st.CodegenArgs) > 0 {
		cfg.Args = append([]string(nil), st.CodegenArgs...)
	}
	if codegenConfigEqual(cfg, defaults) && shared != nil {
		return shared, cfg, nil
	}
	if factory == nil {
		factory = codegen.NewAgent
	}
	agent, err := factory(cfg)
	return agent, cfg, err
}

func codegenConfigEqual(a, b codegen.Config) bool {
	if a.Type != b.Type || a.Model != b.Model || a.Command != b.Command {
		return false
	}
	if a.Timeout != b.Timeout || a.MaxOutputBytes != b.MaxOutputBytes {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

// managerSignature returns a stable hash of the inputs that affect
// AgentManager construction. The GitHub token is intentionally NOT
// included so a token rotation does not invalidate a running agent.
func managerSignature(repo *Repo, st Settings, code codegen.Config) string {
	parts := []string{
		repo.ID.String(),
		repo.Owner,
		repo.Name,
		repo.RepoPath,
		repo.DefaultBranch,
		strconv.Itoa(st.MaxVerifyIterations),
		strconv.Itoa(st.MaxReviewCycles),
		code.Type,
		code.Model,
		code.Command,
		strings.Join(code.Args, ","),
		code.Timeout.String(),
		strconv.Itoa(code.MaxOutputBytes),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
