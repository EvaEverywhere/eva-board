// Package board — HTTP handler for triage, spring-clean, and combined
// curate flows.
//
// Like CardsHandler, the per-user TriageService / SpringCleanService /
// CurateService are constructed per request from the user's settings.
// This keeps the user's GitHub token scoped to one request and avoids
// stale-config bugs when settings change.
package board

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/teslashibe/codegen-go"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// CurateHandler exposes triage, spring-clean, and combined curate
// routes.
type CurateHandler struct {
	cards     cardStore
	settings  *SettingsService
	repos     *ReposService
	agent     codegen.Agent
	ghFactory github.ClientFactory
}

// NewCurateHandler builds a CurateHandler. The handler resolves the
// target repo per-request: ?repo_id wins, otherwise it falls back to
// the user's default repo so the legacy single-repo UI still works.
func NewCurateHandler(
	cards cardStore,
	settings *SettingsService,
	repos *ReposService,
	agent codegen.Agent,
	ghFactory github.ClientFactory,
) *CurateHandler {
	return &CurateHandler{
		cards:     cards,
		settings:  settings,
		repos:     repos,
		agent:     agent,
		ghFactory: ghFactory,
	}
}

// Register mounts the curate routes onto r. The caller is responsible
// for placing r behind authentication middleware.
func (h *CurateHandler) Register(r fiber.Router) {
	g := r.Group("/board")
	g.Post("/triage", h.analyzeTriage)
	g.Post("/triage/apply", h.applyTriage)
	g.Post("/springclean", h.analyzeSpringClean)
	g.Post("/springclean/apply", h.applySpringClean)
	g.Post("/curate", h.curate)
}

type applyTriageBody struct {
	Proposals []TriageProposal `json:"proposals"`
}

type applySpringCleanBody struct {
	Actions []CleanupAction `json:"actions"`
}

func (h *CurateHandler) analyzeTriage(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repo, err := h.resolveRepoFromQuery(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	svc, err := h.buildTriageService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	proposals, err := svc.AnalyzeBacklog(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"proposals": proposals})
}

func (h *CurateHandler) applyTriage(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body applyTriageBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	repo, err := h.resolveRepoFromQuery(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	svc, err := h.buildTriageService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if err := svc.ApplyProposals(c.UserContext(), userID, body.Proposals); err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"applied": len(body.Proposals)})
}

func (h *CurateHandler) analyzeSpringClean(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repo, err := h.resolveRepoFromQuery(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	svc, err := h.buildSpringCleanService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	actions, err := svc.AuditRepo(c.UserContext())
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"actions": actions})
}

func (h *CurateHandler) applySpringClean(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body applySpringCleanBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	repo, err := h.resolveRepoFromQuery(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	svc, err := h.buildSpringCleanService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if err := svc.ApplyActions(c.UserContext(), body.Actions); err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"applied": len(body.Actions)})
}

func (h *CurateHandler) curate(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	repo, err := h.resolveRepoFromQuery(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	triage, err := h.buildTriageService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cleanup, err := h.buildSpringCleanService(c.UserContext(), userID, repo)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	res, err := NewCurateService(triage, cleanup).Run(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(res)
}

// resolveRepoFromQuery picks the repo for a curate-family call.
// Mirrors CardsHandler.resolveRepoID: explicit ?repo_id wins (after
// ownership validation); otherwise we fall back to the user's default
// repo so the legacy UI keeps working. The H1 / M4 audit fix means
// callers that have repos but no default get a clearer error here too.
func (h *CurateHandler) resolveRepoFromQuery(ctx context.Context, c *fiber.Ctx, userID uuid.UUID) (*Repo, error) {
	if h.repos == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "board repos service is not configured on this server")
	}
	if raw := strings.TrimSpace(c.Query("repo_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, apperrors.New(http.StatusBadRequest, "invalid repo_id")
		}
		repo, err := h.repos.Get(ctx, userID, id)
		if err != nil {
			return nil, apperrors.New(http.StatusBadRequest, "repo_id not found for user")
		}
		return repo, nil
	}
	repo, err := h.repos.GetDefault(ctx, userID)
	if err == nil {
		return repo, nil
	}
	if errors.Is(err, ErrRepoNotFound) {
		existing, listErr := h.repos.List(ctx, userID)
		if listErr == nil && len(existing) == 0 {
			return nil, apperrors.New(http.StatusBadRequest, "no repositories connected — add one in Settings → Manage repos")
		}
		return nil, apperrors.New(http.StatusBadRequest, "no default repository selected — pick one in Repos screen, or pass ?repo_id=<id>")
	}
	return nil, err
}

func (h *CurateHandler) buildTriageService(ctx context.Context, userID uuid.UUID, repo *Repo) (*TriageService, error) {
	if h.settings == nil || h.agent == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "triage is not configured on this server")
	}
	if repo == nil {
		return nil, apperrors.New(http.StatusBadRequest, "triage requires a repo")
	}
	cfg := TriageConfig{
		WorkDir:   repo.RepoPath,
		RepoOwner: repo.Owner,
		RepoName:  repo.Name,
		RepoID:    repo.ID,
	}
	if h.ghFactory != nil {
		token, err := h.settings.GitHubToken(ctx, userID)
		if err == nil && token != "" {
			cfg.GitHub = h.ghFactory.NewClient(token)
		}
	}
	return NewTriageService(h.cards, h.agent, cfg), nil
}

func (h *CurateHandler) buildSpringCleanService(ctx context.Context, userID uuid.UUID, repo *Repo) (*SpringCleanService, error) {
	if h.settings == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "spring clean is not configured on this server")
	}
	if repo == nil {
		return nil, apperrors.New(http.StatusBadRequest, "spring clean requires a repo")
	}
	cfg := SpringCleanConfig{
		RepoOwner:    repo.Owner,
		RepoName:     repo.Name,
		RepoPath:     repo.RepoPath,
		BranchPrefix: "eva-board/",
	}
	var ghClient github.Client
	if h.ghFactory != nil {
		token, err := h.settings.GitHubToken(ctx, userID)
		if err == nil && token != "" {
			ghClient = h.ghFactory.NewClient(token)
		}
	}
	return NewSpringCleanService(ghClient, cfg), nil
}
