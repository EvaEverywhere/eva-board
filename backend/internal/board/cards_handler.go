// Package board — HTTP handler for card CRUD + agent lifecycle.
//
// CardsHandler exposes the per-card REST surface the mobile/web client
// hits and is also the integration point between an HTTP request and the
// autonomous agent loop. Column transitions to `develop` start the
// agent; manual moves to `review` stop it; everything else is a plain
// metadata update.
//
// AgentManager resolution: the handler delegates to an AgentRegistry
// that caches managers per (user, repo), keyed by (userID, repoID)
// plus a signature derived from the user's settings/credentials.
// Multi-repo support means one user can drive several boards in
// parallel, each with its own manager. Caching is required so that
// StopAgent and SubmitFeedback from a follow-up HTTP request reach
// the SAME manager instance that owns the running goroutine — without
// it, those operations target an empty in-memory state and silently
// no-op.
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

// AgentLifecycleFactory builds an AgentLifecycle for a given user.
// Tests inject a recording factory; production goes through the
// AgentRegistry on the handler.
type AgentLifecycleFactory func(ctx context.Context, userID, repoID uuid.UUID) (AgentLifecycle, error)

// CardsHandler exposes board card CRUD, move, and agent-lifecycle
// routes.
type CardsHandler struct {
	cards    cardStore
	settings *SettingsService
	repos    *ReposService
	registry *AgentRegistry
	broker   *Broker

	// agentFactory is set by tests via SetAgentFactory to substitute
	// the registry-backed lifecycle resolution. nil means use the
	// registry path.
	agentFactory AgentLifecycleFactory
}

// NewCardsHandler builds a CardsHandler. broker, registry and repos
// may be nil in test binaries that don't exercise the corresponding
// surfaces; agent routes surface a 503 when registry is nil and
// list/create surface a 400 when repos is nil and no ?repo_id is
// supplied.
func NewCardsHandler(
	cards cardStore,
	settings *SettingsService,
	repos *ReposService,
	registry *AgentRegistry,
	broker *Broker,
) *CardsHandler {
	return &CardsHandler{
		cards:    cards,
		settings: settings,
		repos:    repos,
		registry: registry,
		broker:   broker,
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
	RepoID         string         `json:"repo_id,omitempty"`
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
	view := cardView{
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
	if c.RepoID != uuid.Nil {
		view.RepoID = c.RepoID.String()
	}
	return view
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
	repoID, err := h.resolveRepoID(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	column := strings.TrimSpace(c.Query("column"))
	cards, err := h.cards.List(c.UserContext(), userID, repoID, column)
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
	repoID, err := h.resolveRepoID(c.UserContext(), c, userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	var body createCardBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
	}
	card, err := h.cards.Create(c.UserContext(), userID, repoID, CreateRequest{
		Title:       body.Title,
		Description: body.Description,
	})
	if err != nil {
		return apperrors.Handle(c, mapCardError(err))
	}
	return c.Status(http.StatusCreated).JSON(toCardView(card))
}

// resolveRepoID picks the repo for list/create. Explicit ?repo_id
// wins; absent that we fall back to the user's default repo. Returns
// 400 when no repo can be resolved (no default and no explicit id),
// because a card must be scoped to some board.
func (h *CardsHandler) resolveRepoID(ctx context.Context, c *fiber.Ctx, userID uuid.UUID) (uuid.UUID, error) {
	if raw := strings.TrimSpace(c.Query("repo_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, apperrors.New(http.StatusBadRequest, "invalid repo_id")
		}
		// Validate ownership when we have a repos service; without
		// it we trust the caller (matches the test wiring).
		if h.repos != nil {
			if _, err := h.repos.Get(ctx, userID, id); err != nil {
				return uuid.Nil, apperrors.New(http.StatusBadRequest, "repo_id not found for user")
			}
		}
		return id, nil
	}
	if h.repos == nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "repo_id is required")
	}
	repo, err := h.repos.GetDefault(ctx, userID)
	if err != nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "no default board repo configured for user")
	}
	return repo.ID, nil
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
			if err := h.stopAgent(c.UserContext(), userID, cardID); err != nil {
				return apperrors.Handle(c, err)
			}
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
	if err := h.stopAgent(c.UserContext(), userID, cardID); err != nil {
		return apperrors.Handle(c, err)
	}
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
	repoID, err := h.repoForCard(c.UserContext(), userID, cardID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	lc, err := h.resolveLifecycle(c.UserContext(), userID, repoID)
	if err != nil {
		return apperrors.Handle(c, err)
	}
	_ = lc.SubmitFeedback(cardID, feedback)
	return c.JSON(fiber.Map{"status": "queued"})
}

// SetAgentFactory replaces the registry-backed lifecycle resolution
// with a custom factory. Intended for tests; production code leaves
// this nil and goes through the AgentRegistry.
func (h *CardsHandler) SetAgentFactory(f AgentLifecycleFactory) {
	h.agentFactory = f
}

// resolveLifecycle returns either the test-injected factory's
// lifecycle or the registry-cached AgentManager for (userID, repoID).
// repoID is required: agents are now keyed per board so we never
// silently fall back to the default repo here.
func (h *CardsHandler) resolveLifecycle(ctx context.Context, userID, repoID uuid.UUID) (AgentLifecycle, error) {
	if h.agentFactory != nil {
		return h.agentFactory(ctx, userID, repoID)
	}
	if h.registry == nil {
		return nil, apperrors.New(http.StatusServiceUnavailable, "board agent is not configured on this server")
	}
	if repoID == uuid.Nil {
		return nil, apperrors.New(http.StatusBadRequest, "card has no repo")
	}
	return h.registry.For(ctx, userID, repoID)
}

// repoForCard returns the repo a card belongs to, falling back to the
// user's default repo when the card has no repo_id (legacy data).
// Used by the agent lifecycle and move handlers to pick the right
// AgentManager.
func (h *CardsHandler) repoForCard(ctx context.Context, userID, cardID uuid.UUID) (uuid.UUID, error) {
	card, err := h.cards.Get(ctx, userID, cardID)
	if err != nil {
		return uuid.Nil, mapCardError(err)
	}
	if card.RepoID != uuid.Nil {
		return card.RepoID, nil
	}
	if h.repos == nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "card has no repo and no repos service configured")
	}
	repo, err := h.repos.GetDefault(ctx, userID)
	if err != nil {
		return uuid.Nil, apperrors.New(http.StatusBadRequest, "card has no repo and user has no default")
	}
	return repo.ID, nil
}

// startAgentForCard resolves the per-(user, repo) manager from the
// registry and starts the loop. StartAgent is idempotent inside the
// manager (no-op if a run is already active for the card), so we
// don't gate here.
func (h *CardsHandler) startAgentForCard(ctx context.Context, userID, cardID uuid.UUID) error {
	repoID, err := h.repoForCard(ctx, userID, cardID)
	if err != nil {
		return err
	}
	lc, err := h.resolveLifecycle(ctx, userID, repoID)
	if err != nil {
		return err
	}
	if err := lc.StartAgent(ctx, cardID); err != nil {
		return apperrors.New(http.StatusInternalServerError, "failed to start agent: "+err.Error())
	}
	return nil
}

// stopAgent cancels a running agent for cardID. The lookup uses the
// card's own repo_id so we hit the same manager instance StartAgent
// used.
func (h *CardsHandler) stopAgent(ctx context.Context, userID, cardID uuid.UUID) error {
	repoID, err := h.repoForCard(ctx, userID, cardID)
	if err != nil {
		return err
	}
	lc, err := h.resolveLifecycle(ctx, userID, repoID)
	if err != nil {
		return err
	}
	if err := lc.StopAgent(cardID); err != nil {
		return apperrors.New(http.StatusInternalServerError, "failed to stop agent: "+err.Error())
	}
	return nil
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
	// Resolve the repo for the card so we look up the correct local
	// checkout when the user has multiple boards. Falls back to the
	// user's default repo for legacy cards with no repo_id.
	repoPath := ""
	base := "main"
	if card.RepoID != uuid.Nil && h.repos != nil {
		repo, err := h.repos.Get(c.UserContext(), userID, card.RepoID)
		if err == nil {
			repoPath = repo.RepoPath
			if repo.DefaultBranch != "" {
				base = repo.DefaultBranch
			}
		}
	}
	if repoPath == "" && h.settings != nil {
		// Backwards compat — Settings still carries the legacy
		// single-repo path until the next wave drops it.
		st, err := h.settings.Get(c.UserContext(), userID)
		if err != nil {
			return apperrors.Handle(c, err)
		}
		repoPath = strings.TrimSpace(st.RepoPath)
	}
	if repoPath == "" {
		return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "board settings incomplete: repo_path is required"))
	}

	branch := *card.WorktreeBranch
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", base+"..."+branch)
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
