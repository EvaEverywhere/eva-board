package board

// HTTP-level smoke tests for ReposHandler. The validation paths that
// don't touch the database (missing fields, invalid path, repo_id
// parsing) run without a live Postgres; the happy-path Add/Remove
// flows defer to the integration tests in repos_test.go.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/httputil"
)

func newReposTestApp(t *testing.T, h *ReposHandler, userID uuid.UUID) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})
	h.Register(app)
	return app
}

func TestReposHandler_AddRejectsMissingFields(t *testing.T) {
	userID := uuid.New()
	h := NewReposHandler(nil, nil, nil, nil)
	app := newReposTestApp(t, h, userID)

	body, _ := json.Marshal(map[string]any{"owner": "", "name": "x", "repo_path": "/tmp/x"})
	req := httptest.NewRequest(http.MethodPost, "/board/repos/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing owner, got %d", resp.StatusCode)
	}
}

func TestReposHandler_AddRejectsRelativePath(t *testing.T) {
	userID := uuid.New()
	h := NewReposHandler(nil, nil, nil, nil)
	app := newReposTestApp(t, h, userID)

	body, _ := json.Marshal(map[string]any{"owner": "acme", "name": "widgets", "repo_path": "relative/path"})
	req := httptest.NewRequest(http.MethodPost, "/board/repos/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for relative path, got %d", resp.StatusCode)
	}
}

func TestReposHandler_DeleteRejectsBadID(t *testing.T) {
	userID := uuid.New()
	h := NewReposHandler(nil, nil, nil, nil)
	app := newReposTestApp(t, h, userID)

	req := httptest.NewRequest(http.MethodDelete, "/board/repos/not-a-uuid", nil)
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad uuid, got %d", resp.StatusCode)
	}
}
