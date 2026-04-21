package board

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/httputil"
)

// SettingsHandler exposes per-user board settings over HTTP.
type SettingsHandler struct {
	svc      *SettingsService
	registry *AgentRegistry
}

// NewSettingsHandler builds a SettingsHandler. registry may be nil in
// test binaries that don't construct the agent stack; in production
// it is required so a settings change can evict the cached
// AgentManager and force a rebuild on the next agent operation.
func NewSettingsHandler(svc *SettingsService, registry *AgentRegistry) *SettingsHandler {
	return &SettingsHandler{svc: svc, registry: registry}
}

// Register mounts the settings routes onto r. The caller is responsible
// for placing r behind authentication middleware.
func (h *SettingsHandler) Register(r fiber.Router) {
	g := r.Group("/board/settings")
	g.Get("/", h.get)
	g.Put("/", h.upsert)
	g.Get("/repos", h.listRepos)
}

type upsertSettingsBody struct {
	GitHubToken         *string `json:"github_token,omitempty"`
	GitHubOwner         *string `json:"github_owner,omitempty"`
	GitHubRepo          *string `json:"github_repo,omitempty"`
	RepoPath            *string `json:"repo_path,omitempty"`
	CodegenAgent        *string   `json:"codegen_agent,omitempty"`
	CodegenCommand      *string   `json:"codegen_command,omitempty"`
	CodegenArgs         *[]string `json:"codegen_args,omitempty"`
	MaxVerifyIterations *int      `json:"max_verify_iterations,omitempty"`
	MaxReviewCycles     *int      `json:"max_review_cycles,omitempty"`
}

type repoView struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

func (h *SettingsHandler) get(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	st, err := h.svc.Get(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(st)
}

func (h *SettingsHandler) upsert(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}

	var body upsertSettingsBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}

	st, err := h.svc.Upsert(c.UserContext(), userID, UpsertRequest{
		GitHubToken:         body.GitHubToken,
		GitHubOwner:         body.GitHubOwner,
		GitHubRepo:          body.GitHubRepo,
		RepoPath:            body.RepoPath,
		CodegenAgent:        body.CodegenAgent,
		CodegenCommand:      body.CodegenCommand,
		CodegenArgs:         body.CodegenArgs,
		MaxVerifyIterations: body.MaxVerifyIterations,
		MaxReviewCycles:     body.MaxReviewCycles,
	})
	if err != nil {
		return apperrors.Handle(c, mapSettingsError(err))
	}
	if h.registry != nil {
		h.registry.Forget(userID)
	}
	return c.JSON(st)
}

func (h *SettingsHandler) listRepos(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repos, err := h.svc.ListRepos(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, mapSettingsError(err))
	}
	out := make([]repoView, 0, len(repos))
	for _, r := range repos {
		out = append(out, repoView{
			Owner:         r.Owner.Login,
			Name:          r.Name,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
		})
	}
	return c.JSON(fiber.Map{"repos": out})
}

func currentUserUUID(c *fiber.Ctx) (uuid.UUID, error) {
	raw, err := httputil.CurrentUserID(c)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperrors.ErrUnauthorized
	}
	return id, nil
}

func mapSettingsError(err error) error {
	switch {
	case errors.Is(err, ErrNoGitHubToken):
		return apperrors.New(http.StatusBadRequest, "no github token configured for user")
	case errors.Is(err, ErrCipherNotConfigured):
		return apperrors.New(http.StatusServiceUnavailable, "token encryption is not configured on the server")
	default:
		return err
	}
}
