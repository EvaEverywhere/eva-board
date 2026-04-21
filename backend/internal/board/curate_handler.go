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
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
)

// CurateHandler exposes triage, spring-clean, and combined curate
// routes.
type CurateHandler struct {
	cards     *Service
	settings  *SettingsService
	llm       llm.Client
	ghFactory github.ClientFactory
	llmModel  string
}

// NewCurateHandler builds a CurateHandler.
func NewCurateHandler(
	cards *Service,
	settings *SettingsService,
	llmClient llm.Client,
	ghFactory github.ClientFactory,
	llmModel string,
) *CurateHandler {
	return &CurateHandler{
		cards:     cards,
		settings:  settings,
		llm:       llmClient,
		ghFactory: ghFactory,
		llmModel:  llmModel,
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
	svc, err := h.buildTriageService(c.UserContext(), userID)
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
	svc, err := h.buildTriageService(c.UserContext(), userID)
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
	svc, err := h.buildSpringCleanService(c.UserContext(), userID)
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
	svc, err := h.buildSpringCleanService(c.UserContext(), userID)
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
	triage, err := h.buildTriageService(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cleanup, err := h.buildSpringCleanService(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	res, err := NewCurateService(triage, cleanup).Run(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(res)
}

func (h *CurateHandler) buildTriageService(ctx context.Context, userID uuid.UUID) (*TriageService, error) {
	if h.settings == nil || h.llm == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "triage is not configured on this server")
	}
	st, err := h.settings.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	cfg := TriageConfig{
		Model:     h.llmModel,
		RepoOwner: st.GitHubOwner,
		RepoName:  st.GitHubRepo,
	}
	if h.ghFactory != nil && st.GitHubOwner != "" && st.GitHubRepo != "" {
		token, err := h.settings.GitHubToken(ctx, userID)
		if err == nil && token != "" {
			cfg.GitHub = h.ghFactory.NewClient(token)
		}
	}
	return NewTriageService(h.cards, h.llm, cfg), nil
}

func (h *CurateHandler) buildSpringCleanService(ctx context.Context, userID uuid.UUID) (*SpringCleanService, error) {
	if h.settings == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "spring clean is not configured on this server")
	}
	st, err := h.settings.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	cfg := SpringCleanConfig{
		RepoOwner:    st.GitHubOwner,
		RepoName:     st.GitHubRepo,
		RepoPath:     st.RepoPath,
		BranchPrefix: "eva-board/",
	}
	var ghClient github.Client
	if h.ghFactory != nil && st.GitHubOwner != "" && st.GitHubRepo != "" {
		token, err := h.settings.GitHubToken(ctx, userID)
		if err == nil && token != "" {
			ghClient = h.ghFactory.NewClient(token)
		}
	}
	return NewSpringCleanService(ghClient, cfg), nil
}
