// Package board — GitHub webhook receiver.
//
// WebhookHandler accepts GitHub webhook deliveries (currently
// pull_request events), verifies the X-Hub-Signature-256 HMAC against
// the configured shared secret, and applies card column transitions
// driven by PR state changes:
//
//   - merged → done
//   - closed unmerged → review
//
// The endpoint MUST NOT require auth — GitHub authenticates via HMAC
// only. Routing should mount this outside the auth middleware group.
//
// Card → user mapping is implicit: GetByPRNumber finds the card; the
// card carries its own user_id, so we move within that user's
// per-user position space.
package board

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
)

// WebhookHandler dispatches GitHub webhook deliveries.
type WebhookHandler struct {
	cards  *Service
	broker *Broker
	secret string
}

// NewWebhookHandler builds a WebhookHandler. broker may be nil; secret
// must be set or every request is rejected (treated as
// misconfiguration, not implicit trust).
func NewWebhookHandler(cards *Service, broker *Broker, secret string) *WebhookHandler {
	return &WebhookHandler{cards: cards, broker: broker, secret: secret}
}

// Register mounts the webhook route on r. The caller MUST place this
// route OUTSIDE any auth middleware group.
func (h *WebhookHandler) Register(r fiber.Router) {
	r.Post("/webhooks/github", h.receive)
}

func (h *WebhookHandler) receive(c *fiber.Ctx) error {
	body := c.Body()
	signature := c.Get("X-Hub-Signature-256")
	eventType := c.Get("X-GitHub-Event")
	deliveryID := c.Get("X-GitHub-Delivery")

	event, err := github.ParseEvent(h.secret, body, eventType, deliveryID, signature)
	if err != nil {
		return apperrors.Handle(c, apperrors.New(http.StatusUnauthorized, "invalid webhook: "+err.Error()))
	}

	// Always 200 quickly. Heavy lookups + DB writes happen in a
	// background goroutine using a fresh context so the request
	// returns immediately and GitHub does not retry on client-side
	// slowness.
	go h.dispatch(event)

	return c.SendStatus(http.StatusOK)
}

func (h *WebhookHandler) dispatch(event *github.Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[board-webhook] panic handling event %s/%s: %v", event.Type, event.DeliveryID, r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch event.Type {
	case "pull_request":
		h.handlePullRequest(ctx, event)
	default:
		// Silently ignore event types we don't handle. GitHub will
		// happily deliver many; no need to log them all.
	}
}

func (h *WebhookHandler) handlePullRequest(ctx context.Context, event *github.Event) {
	action, _ := event.Payload["action"].(string)
	prRaw, _ := event.Payload["pull_request"].(map[string]any)
	if prRaw == nil {
		return
	}
	prNumberFloat, ok := prRaw["number"].(float64)
	if !ok {
		return
	}
	prNumber := int(prNumberFloat)
	merged, _ := prRaw["merged"].(bool)

	switch action {
	case "closed":
		card, err := h.cards.GetByPRNumber(ctx, prNumber)
		if err != nil {
			if !errors.Is(err, ErrCardNotFound) {
				log.Printf("[board-webhook] lookup card by PR %d: %v", prNumber, err)
			}
			return
		}
		target := ColumnReview
		if merged {
			target = ColumnDone
		}
		if card.Column == target {
			return
		}
		updated, err := h.cards.Move(ctx, card.UserID, card.ID, target, 0)
		if err != nil {
			log.Printf("[board-webhook] move card %s to %s: %v", card.ID, target, err)
			return
		}
		if h.broker != nil {
			h.broker.Publish(Event{
				Type:   EventCardMoved,
				UserID: card.UserID.String(),
				CardID: card.ID.String(),
				Data: map[string]any{
					"from_column": card.Column,
					"to_column":   updated.Column,
					"position":    updated.Position,
					"trigger":     "github_webhook",
					"pr_number":   prNumber,
					"pr_merged":   merged,
				},
			})
		}
	case "opened":
		// Best-effort pre-link: when an `eva-board/<short-id>` branch
		// opens a PR we may already have a card waiting, but the
		// branch alone does not identify the card uniquely without
		// extra metadata, so we just log for now. Future work: parse
		// short id from head.ref and persist PR info.
		head, _ := prRaw["head"].(map[string]any)
		ref, _ := head["ref"].(string)
		if !strings.HasPrefix(ref, "eva-board/") {
			return
		}
		log.Printf("[board-webhook] eva-board PR opened: #%d head=%s", prNumber, ref)
	}
}
