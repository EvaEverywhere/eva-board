package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	magiclink "github.com/teslashibe/magiclink-auth-go"
	"github.com/teslashibe/magiclink-auth-go/fiberadapter"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/auth"
	"github.com/EvaEverywhere/eva-board/backend/internal/board"
	"github.com/EvaEverywhere/eva-board/backend/internal/bootstrap"
	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	githubclient "github.com/EvaEverywhere/eva-board/backend/internal/github"
)

func main() {
	ctx := context.Background()

	core, err := bootstrap.Init(ctx)
	if err != nil {
		log.Fatalf("bootstrap init: %v", err)
	}
	defer core.Pool.Close()

	authSvc := auth.NewService(core.Pool)
	magicSvc, err := newMagicLinkService(core.Cfg, core.Pool, authSvc)
	if err != nil {
		log.Fatalf("magiclink init: %v", err)
	}

	authMW := auth.NewMiddleware(magicSvc, authSvc)
	authHandler := auth.NewHandler(authSvc)

	app := fiber.New(fiber.Config{
		AppName: "Eva Board API",
	})
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: core.Cfg.CORSAllowedOrigins,
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	registerPublicRoutes(app)

	app.Post("/auth/magic-link", fiberadapter.SendHandler(magicSvc))
	app.Post("/auth/verify", fiberadapter.VerifyCodeHandler(magicSvc))
	app.Get("/auth/verify", fiberadapter.VerifyLinkHandler(magicSvc))
	app.Post("/auth/login", devLoginHandler(magicSvc, authSvc))

	boardBroker := board.NewBroker()
	boardEventsHandler := board.NewEventsHandler(boardBroker)

	ghFactory := &githubclient.HTTPClientFactory{
		BaseURL:   core.Cfg.GitHubAPIBaseURL,
		UserAgent: "eva-board-server",
	}
	cardsSvc := board.New(core.Pool)
	settingsSvc := board.NewSettingsService(core.Pool, core.Cipher, ghFactory)
	reposSvc := board.NewReposService(core.Pool)

	codegenDefaults := codegen.Config{
		Type:           core.Cfg.CodegenAgent,
		Model:          core.Cfg.CodegenModel,
		Timeout:        core.Cfg.CodegenTimeout,
		MaxOutputBytes: core.Cfg.CodegenMaxOutputBytes,
		Command:        core.Cfg.CodegenCommand,
		Args:           core.Cfg.CodegenArgs,
	}
	codegenAgent, err := codegen.NewAgent(codegenDefaults)
	if err != nil {
		log.Fatalf("codegen init: %v", err)
	}

	// One AgentRegistry per process: it caches *board.AgentManager
	// instances per (user, repo) so Stop/Feedback from a follow-up HTTP
	// request reach the same goroutine StartAgent kicked off, even
	// when a user has multiple boards running in parallel.
	agentRegistry := board.NewAgentRegistry(board.NewProductionManagerBuilder(board.AgentBuilderDeps{
		Cards:           cardsSvc,
		Repos:           reposSvc,
		Settings:        settingsSvc,
		GitHubClient:    ghFactory,
		CodegenDefaults: codegenDefaults,
		SharedCodegen:   codegenAgent,
	}))

	settingsHandler := board.NewSettingsHandler(settingsSvc, agentRegistry)
	reposHandler := board.NewReposHandler(reposSvc, ghFactory, settingsSvc, agentRegistry)
	// DraftService reuses the shared codegen agent so the draft
	// prompt runs in the user's repo worktree and benefits from any
	// user-level CODEGEN_* overrides already folded into the shared
	// instance at startup.
	draftSvc := board.NewDraftService(reposSvc, codegenAgent)
	cardsHandler := board.NewCardsHandler(
		cardsSvc, settingsSvc, reposSvc, agentRegistry, boardBroker, draftSvc, ghFactory,
	)
	curateHandler := board.NewCurateHandler(
		cardsSvc, settingsSvc, reposSvc, codegenAgent, ghFactory,
	)
	webhookHandler := board.NewWebhookHandler(cardsSvc, boardBroker, core.Cfg.GitHubWebhookSecret)

	// Webhook routes intentionally live OUTSIDE the auth group —
	// GitHub authenticates via X-Hub-Signature-256 HMAC.
	webhookHandler.Register(app)

	api := app.Group("/api", authMW.RequireAuth())
	api.Get("/me", authHandler.GetMe)
	api.Get("/board/events", boardEventsHandler.Stream)
	settingsHandler.Register(api)
	reposHandler.Register(api)
	cardsHandler.Register(api)
	curateHandler.Register(api)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listen(":" + core.Cfg.Port)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case listenErr := <-errCh:
		if listenErr != nil {
			log.Fatalf("server listen error: %v", listenErr)
		}
	case sig := <-sigCh:
		log.Printf("shutdown signal received: %s", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Fatalf("shutdown failed: %v", err)
		}
	}
}

func devLoginHandler(magicSvc *magiclink.Service, authSvc *auth.Service) fiber.Handler {
	type request struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}

	return func(c *fiber.Ctx) error {
		var req request
		if err := c.BodyParser(&req); err != nil {
			return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "invalid request body"))
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		if email == "" {
			return apperrors.Handle(c, apperrors.New(http.StatusBadRequest, "email is required"))
		}

		name := strings.TrimSpace(req.Name)
		identityKey := fmt.Sprintf("dev|%s", email)
		user, err := authSvc.UpsertIdentity(c.UserContext(), identityKey, email, name)
		if err != nil {
			return apperrors.Handle(c, err)
		}

		token, err := magicSvc.IssueToken(magiclink.Claims{
			Subject:     identityKey,
			Email:       email,
			DisplayName: user.Name,
		})
		if err != nil {
			return apperrors.Handle(c, err)
		}

		return c.JSON(magiclink.AuthResult{
			JWT:         token,
			UserID:      user.ID,
			Email:       user.Email,
			DisplayName: user.Name,
		})
	}
}
