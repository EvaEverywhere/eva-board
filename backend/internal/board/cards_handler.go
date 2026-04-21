// Package board — HTTP handler for card CRUD + agent lifecycle.
//
// CardsHandler exposes the per-card REST surface the mobile/web client
// hits and is also the integration point between an HTTP request and the
// autonomous agent loop. Column transitions to `develop` start the
// agent; manual moves to `review` stop it; everything else is a plain
// metadata update.
//
// AgentManager construction strategy (v1): the handler builds a fresh
// AgentManager per request from the requesting user's settings. This is
// intentionally simple — settings (RepoPath, GitHubOwner, GitHubRepo,
// retry caps, GitHub PAT) are user-scoped and live in the
// SettingsService, so building per-request keeps the wiring obvious and
// avoids stale-token bugs. The cost is one DB read + token decrypt per
// agent-touching request, which is fine at v1 scale. Future work: cache
// per-user managers and invalidate on settings change.
package board

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
)

// AgentLifecycle is the slice of AgentManager that CardsHandler relies
// on to start, stop, and feed the autonomous loop. Defining it as an
// interface lets tests substitute a recording fake without spinning up
// a real manager (which needs git on PATH, settings rows, etc.).
// *AgentManager satisfies it directly.
type AgentLifecycle interface {
	StartAgent(ctx context.Context, cardID uuid.UUID) error
	StopAgent(cardID uuid.UUID) error
	SubmitFeedback(cardID uuid.UUID, feedback string) error
}

// AgentLifecycleFactory builds an AgentLifecycle for a given user. The
// production factory hits the per-user settings row to produce a fresh
// manager. Tests inject a recording factory.
type AgentLifecycleFactory func(ctx context.Context, userID uuid.UUID) (AgentLifecycle, error)

// CardsHandler exposes board card CRUD, move, and agent-lifecycle
// routes.
type CardsHandler struct {
	cards     cardStore
	settings  *SettingsService
	code      codegen.Agent
	llm       llm.Client
	ghFactory github.ClientFactory
	broker    *Broker
	llmModel  string

	// agentFactory is set by tests via SetAgentFactory to substitute
	// the real settings-driven AgentManager builder. nil means use the
	// default buildAgentManager path.
	agentFactory AgentLifecycleFactory
}

// NewCardsHandler builds a CardsHandler. broker may be nil (events are
// best-effort), but the other deps must be non-nil for any agent route
// to function. The handler does not pre-validate them so the package is
// importable in builds that don't construct the full board stack
// (e.g. test binaries).
func NewCardsHandler(
	cards cardStore,
	settings *SettingsService,
	code codegen.Agent,
	llmClient llm.Client,
	ghFactory github.ClientFactory,
	broker *Broker,
	llmModel string,
) *CardsHandler {
	return &CardsHandler{
		cards:     cards,
		settings:  settings,
		code:      code,
		llm:       llmClient,
		ghFactory: ghFactory,
		broker:    broker,
		llmModel:  llmModel,
	}
}

// Register mounts the card routes onto r. The caller is responsible for
// placing r behind authentication middleware.
func (h *CardsHandler) Register(r fiber.Router) {
	g := r.Group("/board/cards")
	g.Get("/", h.list)
	g.Post("/", h.create)
	g.Get("/:id", h.get)
	g.Put("/:id", h.update)
	g.Delete("/:id", h.delete)
	g.Post("/:id/move", h.move)
	g.Post("/:id/agent/start", h.agentStart)
	g.Post("/:id/agent/stop", h.agentStop)
	g.Post("/:id/agent/feedback", h.agentFeedback)
	g.Get("/:id/diff", h.diff)
}

// cardView is the JSON shape returned to the client. We don't add JSON
// tags to the domain Card struct because it doubles as the internal
// shared model and we want the wire shape to be controlled here.
type cardView struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Column         string         `json:"column"`
	Position       int            `json:"position"`
	AgentStatus    string         `json:"agent_status"`
	WorktreeBranch *string        `json:"worktree_branch,omitempty"`
	PRNumber       *int           `json:"pr_number,omitempty"`
	PRURL          *string        `json:"pr_url,omitempty"`
	ReviewStatus   *string        `json:"review_status,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func toCardView(c *Card) cardView {
	if c == nil {
		return cardView{}
	}
	return cardView{
		ID:             c.ID.String(),
		UserID:         c.UserID.String(),
		Title:          c.Title,
		Description:    c.Description,
		Column:         c.Column,
		Position:       c.Position,
		AgentStatus:    c.AgentStatus,
		WorktreeBranch: c.WorktreeBranch,
		PRNumber:       c.PRNumber,
		PRURL:          c.PRURL,
		ReviewStatus:   c.ReviewStatus,
		Metadata:       c.Metadata,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
}

func toCardViews(cs []Card) []cardView {
	out := make([]cardView, 0, len(cs))
	for i := range cs {
		out = append(out, toCardView(&cs[i]))
	}
	return out
}

type createCardBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type updateCardBody struct {
	Title       *string        `json:"title,omitempty"`
	Description *string        `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type moveCardBody struct {
	ToColumn   string `json:"to_column"`
	ToPosition int    `json:"to_position"`
}

type feedbackBody struct {
	Feedback string `json:"feedback"`
}

func (h *CardsHandler) list(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	column := strings.TrimSpace(c.Query("column"))
	cards, err := h.cards.List(c.UserContext(), userID, column)
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.JSON(fiber.Map{"cards": toCardViews(cards)})
}

func (h *CardsHandler) create(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body createCardBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	card, err := h.cards.Create(c.UserContext(), userID, CreateRequest{
		Title:       body.Title,
		Description: body.Description,
	})
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.Status(http.StatusCreated).JSON(toCardView(card))
}

func (h *CardsHandler) get(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	card, err := h.cards.Get(c.UserContext(), userID, cardID)
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.JSON(toCardView(card))
}

func (h *CardsHandler) update(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body updateCardBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	card, err := h.cards.Update(c.UserContext(), userID, cardID, UpdateRequest{
		Title:       body.Title,
		Description: body.Description,
		Metadata:    body.Metadata,
	})
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.JSON(toCardView(card))
}

func (h *CardsHandler) delete(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if err := h.cards.Delete(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.SendStatus(http.StatusNoContent)
}

// move applies the column transition and triggers agent lifecycle
// hooks: moving INTO `develop` starts the agent; moving INTO `review`
// stops it. Other moves only persist the column/position change. We
// load the previous column before the move so agent triggers fire only
// on real transitions (re-ordering inside develop should not restart
// the loop).
func (h *CardsHandler) move(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body moveCardBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}

	prev, err := h.cards.Get(c.UserContext(), userID, cardID)
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	fromColumn := prev.Column

	updated, err := h.cards.Move(c.UserContext(), userID, cardID, body.ToColumn, body.ToPosition)
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}

	if h.broker != nil {
		h.broker.Publish(Event{
			Type:   EventCardMoved,
			UserID: userID.String(),
			CardID: cardID.String(),
			Data: map[string]any{
				"from_column": fromColumn,
				"to_column":   updated.Column,
				"position":    updated.Position,
			},
		})
	}

	switch updated.Column {
	case ColumnDevelop:
		if fromColumn != ColumnDevelop {
			if err := h.startAgentForCard(c.UserContext(), userID, cardID); err != nil {
				return apperrors.Handle(c, err)
			}
		}
	case ColumnReview:
		if fromColumn == ColumnDevelop {
			h.stopAgentBestEffort(userID, cardID)
		}
	}

	return c.JSON(toCardView(updated))
}

func (h *CardsHandler) agentStart(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if _, err := h.cards.Get(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	if err := h.startAgentForCard(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, err)
	}
	return c.JSON(fiber.Map{"status": "started"})
}

func (h *CardsHandler) agentStop(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if _, err := h.cards.Get(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	h.stopAgentBestEffort(userID, cardID)
	return c.JSON(fiber.Map{"status": "stopped"})
}

func (h *CardsHandler) agentFeedback(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if _, err := h.cards.Get(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	var body feedbackBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	feedback := strings.TrimSpace(body.Feedback)
	if feedback == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "feedback is required"))
	}
	lc, err := h.resolveLifecycle(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	_ = lc.SubmitFeedback(cardID, feedback)
	return c.JSON(fiber.Map{"status": "queued"})
}

// SetAgentFactory replaces the default settings-driven AgentManager
// builder with a custom factory. Intended for tests; production code
// leaves this nil and goes through buildAgentManager.
func (h *CardsHandler) SetAgentFactory(f AgentLifecycleFactory) {
	h.agentFactory = f
}

// resolveLifecycle returns either the test-injected factory's
// lifecycle or a freshly built AgentManager from settings.
func (h *CardsHandler) resolveLifecycle(ctx context.Context, userID uuid.UUID) (AgentLifecycle, error) {
	if h.agentFactory != nil {
		return h.agentFactory(ctx, userID)
	}
	return h.buildAgentManager(ctx, userID)
}

// startAgentForCard builds a per-user AgentManager and starts the loop.
// StartAgent is itself idempotent in the manager (no-op if a run is
// already active for the card), so we don't gate here.
func (h *CardsHandler) startAgentForCard(ctx context.Context, userID, cardID uuid.UUID) error {
	lc, err := h.resolveLifecycle(ctx, userID)
	if err != nil {
		return err
	}
	if err := lc.StartAgent(ctx, cardID); err != nil {
		return apperrors.New(http.StatusInternalServerError, "failed to start agent: "+err.Error())
	}
	return nil
}

// stopAgentBestEffort cancels a running agent if one exists. Failures
// are logged-by-omission (the manager itself never returns errors from
// StopAgent today). We still build the manager because runs are tracked
// inside it; in v1 each request constructs a fresh manager so there is
// no shared run state to stop. This means HTTP-driven stops are mostly
// symbolic until we cache managers per user — documented as a known
// limitation; see package comment.
func (h *CardsHandler) stopAgentBestEffort(userID, cardID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lc, err := h.resolveLifecycle(ctx, userID)
	if err != nil {
		return
	}
	_ = lc.StopAgent(cardID)
}

// buildAgentManager constructs a fresh AgentManager from the user's
// stored settings. Returns a 400 AppError if settings are incomplete.
func (h *CardsHandler) buildAgentManager(ctx context.Context, userID uuid.UUID) (*AgentManager, error) {
	if h.settings == nil || h.ghFactory == nil || h.code == nil || h.llm == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "board agent is not configured on this server")
	}
	st, err := h.settings.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if st.GitHubOwner == "" || st.GitHubRepo == "" || st.RepoPath == "" {
		return nil, apperrors.New(http.StatusBadRequest, "board settings incomplete: github_owner, github_repo, and repo_path are required")
	}
	token, err := h.settings.GitHubToken(ctx, userID)
	if err != nil {
		return nil, mapSettingsError(err)
	}
	gh := h.ghFactory.NewClient(token)
	cfg := AgentConfig{
		RepoOwner:           st.GitHubOwner,
		RepoName:             st.GitHubRepo,
		RepoPath:             st.RepoPath,
		MaxVerifyIterations:  st.MaxVerifyIterations,
		MaxReviewCycles:      st.MaxReviewCycles,
		LLMModel:             h.llmModel,
		GitHubToken:          token,
	}
	return NewAgentManager(h.cards, h.code, gh, h.llm, cfg), nil
}

// diff returns the git diff for the card's worktree branch against the
// repository's main branch. The diff is computed with
// `git -C <repoPath> diff main...<branch>`, which shows the changes on
// the branch since it diverged from main. Returns an empty string if no
// branch has been created yet (e.g. agent has not run).
func (h *CardsHandler) diff(c *fiber.Ctx) error {
	userID, err := currentUserUUID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	cardID, err := parseCardID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	card, err := h.cards.Get(c.UserContext(), userID, cardID)
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	if card.WorktreeBranch == nil || strings.TrimSpace(*card.WorktreeBranch) == "" {
		return c.JSON(fiber.Map{"diff": "", "branch": nil, "base": "main"})
	}
	if h.settings == nil {
		return apperrors.Handle(c, apperrors.New(http.StatusServiceUnavailable, "board settings not configured"))
	}
	st, err := h.settings.Get(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	if strings.TrimSpace(st.RepoPath) == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "board settings incomplete: repo_path is required"))
	}

	branch := *card.WorktreeBranch
	base := "main"
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", st.RepoPath, "diff", base+"..."+branch)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return c.JSON(fiber.Map{
			"diff":   "",
			"branch": branch,
			"base":   base,
			"error":  strings.TrimSpace(string(out)),
		})
	}
	return c.JSON(fiber.Map{
		"diff":   string(out),
		"branch": branch,
		"base":   base,
	})
}

func parseCardID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "invalid card id")
	}
	return id, nil
}

func mapCardError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrCardNotFound):
		return apperrors.New(http.StatusNotFound, "card not found")
	case errors.Is(err, ErrInvalidColumn):
		return apperrors.New(http.StatusBadRequest, "invalid column")
	case errors.Is(err, ErrInvalidStatus):
		return apperrors.New(http.StatusBadRequest, "invalid agent status")
	case errors.Is(err, ErrTitleRequired):
		return apperrors.New(http.StatusBadRequest, "title is required")
	case errors.Is(err, ErrInvalidPosition):
		return apperrors.New(http.StatusBadRequest, "invalid position")
	default:
		return err
	}
}
