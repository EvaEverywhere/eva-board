package board

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/httputil"
)

// keepAliveInterval controls how often a comment line is emitted to keep the
// connection alive through proxies.
const keepAliveInterval = 15 * time.Second

// EventsHandler exposes SSE streaming for board events.
type EventsHandler struct {
	broker *Broker
}

// NewEventsHandler wires the broker into a Fiber-friendly handler.
func NewEventsHandler(broker *Broker) *EventsHandler {
	return &EventsHandler{broker: broker}
}

// Stream serves a single Server-Sent Events connection for the authenticated user.
//
// Headers:
//   - Content-Type: text/event-stream
//   - Cache-Control: no-cache
//   - Connection: keep-alive
//
// Reads Last-Event-ID header for resume.
// Sends a comment ":\n\n" every 15s to keep the connection alive through proxies.
func (h *EventsHandler) Stream(c *fiber.Ctx) error {
	userID, err := httputil.CurrentUserID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}

	lastEventID := c.Get("Last-Event-ID")

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	events, cleanup := h.broker.Subscribe(userID, lastEventID)

	ctx := c.Context()
	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cleanup()

		ticker := time.NewTicker(keepAliveInterval)
		defer ticker.Stop()

		done := ctx.Done()

		for {
			select {
			case <-done:
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if err := writeEvent(w, ev); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-ticker.C:
				if _, err := w.WriteString(":\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

func writeEvent(w *bufio.Writer, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, payload); err != nil {
		return err
	}
	return nil
}
