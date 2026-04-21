// Package board — HTTP handler for the per-user repo catalog.
//
// The handler validates new repos against GitHub before persisting (we
// reject 404 / 401 right at the API edge so the UI gets a usable error
// instead of failing later inside the agent loop), and evicts the
// AgentRegistry cache for the user whenever the catalog changes so the
// next agent operation sees the new shape.
package board

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// ReposHandler exposes the /api/board/repos REST surface.
type ReposHandler struct {
	repos    *ReposService
	gh       github.ClientFactory
	settings *SettingsService
	registry *AgentRegistry
}

// NewReposHandler builds the handler. registry may be nil in test
// binaries that don't wire the agent stack; in production it is
// required so a repo change evicts the cached AgentManager(s) for the
// user.
func NewReposHandler(repos *ReposService, gh github.ClientFactory, settings *SettingsService, registry *AgentRegistry) *ReposHandler {
	return &ReposHandler{repos: repos, gh: gh, settings: settings, registry: registry}
}

// Register mounts the repo routes onto r. The caller is responsible
// for placing r behind authentication middleware.
func (h *ReposHandler) Register(r fiber.Router) {
	g := r.Group("/board/repos")
	g.Get("/", h.list)
	g.Post("/", h.add)
	g.Delete("/:id", h.remove)
	g.Post("/:id/default", h.setDefault)
}

type addRepoBody struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	RepoPath      string `json:"repo_path"`
	DefaultBranch string `json:"default_branch,omitempty"`
	SetDefault    bool   `json:"set_default,omitempty"`
}

func (h *ReposHandler) list(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repos, err := h.repos.List(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"repos": repos})
}

func (h *ReposHandler) add(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body addRepoBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}

	owner := strings.TrimSpace(body.Owner)
	name := strings.TrimSpace(body.Name)
	repoPath := strings.TrimSpace(body.RepoPath)
	if owner == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "owner is required"))
	}
	if name == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "name is required"))
	}
	if repoPath == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "repo_path is required"))
	}
	// We only check that repo_path looks absolute; existence is
	// validated lazily inside the agent loop because it depends on the
	// host filesystem (not present in test envs / web).
	if !filepath.IsAbs(repoPath) {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "repo_path must be an absolute path"))
	}

	// Validate against GitHub before writing. Without a configured
	// token we can't talk to the API, so return 400 — the user must
	// save a token via /board/settings first.
	if h.gh == nil || h.settings == nil {
		return apperrors.Handle(c, apperrors.New(http.StatusServiceUnavailable, "github is not configured on this server"))
	}
	token, err := h.settings.GitHubToken(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, mapSettingsError(err))
	}
	ghRepo, err := h.gh.NewClient(token).GetRepo(c.UserContext(), owner, name)
	if err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "github could not find repo: "+err.Error()))
	}
	branch := strings.TrimSpace(body.DefaultBranch)
	if branch == "" {
		branch = strings.TrimSpace(ghRepo.DefaultBranch)
	}

	repo, err := h.repos.Add(c.UserContext(), userID, AddRepoRequest{
		Owner:         owner,
		Name:          name,
		RepoPath:      repoPath,
		DefaultBranch: branch,
		SetDefault:    body.SetDefault,
	})
	if err != nil {
		return apperrors.Handle(c, mapRepoError(err))
	}
	if h.registry != nil {
		h.registry.Forget(userID)
	}
	return c.Status(http.StatusCreated).JSON(repo)
}

func (h *ReposHandler) remove(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repoID, err := parseRepoID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if err := h.repos.Remove(c.UserContext(), userID, repoID); err != nil {
		return apperrors.Handle(c, mapRepoError(err))
	}
	if h.registry != nil {
		h.registry.Forget(userID)
	}
	return c.SendStatus(http.StatusNoContent)
}

func (h *ReposHandler) setDefault(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repoID, err := parseRepoID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if err := h.repos.SetDefault(c.UserContext(), userID, repoID); err != nil {
		return apperrors.Handle(c, mapRepoError(err))
	}
	if h.registry != nil {
		h.registry.Forget(userID)
	}
	repo, err := h.repos.Get(c.UserContext(), userID, repoID)
	if err != nil {
		return apperrors.Handle(c, mapRepoError(err))
	}
	return c.JSON(repo)
}

func parseRepoID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "invalid repo id")
	}
	return id, nil
}

func mapRepoError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrRepoNotFound):
		return apperrors.New(http.StatusNotFound, "repo not found")
	case errors.Is(err, ErrRepoConflict):
		return apperrors.New(http.StatusConflict, "repo already connected")
	case errors.Is(err, ErrRepoOwnerRequired):
		return apperrors.New(http.StatusBadRequest, "owner is required")
	case errors.Is(err, ErrRepoNameRequired):
		return apperrors.New(http.StatusBadRequest, "name is required")
	case errors.Is(err, ErrRepoPathRequired):
		return apperrors.New(http.StatusBadRequest, "repo_path is required")
	default:
		return err
	}
}
