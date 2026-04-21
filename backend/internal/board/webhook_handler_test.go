package board

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const testWebhookSecret = "test-webhook-secret"

func signWebhookBody(t *testing.T, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newWebhookTestApp(t *testing.T, store *fakeCardStore) *fiber.App {
	t.Helper()
	app := fiber.New()
	h := NewWebhookHandler(store, nil, testWebhookSecret)
	h.SetDispatchSyncForTest(true)
	h.Register(app)
	return app
}

func pullRequestBody(t *testing.T, action string, prNumber int, merged bool) []byte {
	t.Helper()
	payload := map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"number": prNumber,
			"merged": merged,
			"head":   map[string]any{"ref": "eva-board/abc123"},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func sendWebhook(t *testing.T, app *fiber.App, body []byte, signature string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-test")
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

// seedCardWithPR seeds a card sitting in the develop column linked to a
// PR number so the webhook lookup hits.
func seedCardWithPR(store *fakeCardStore, prNumber int) *Card {
	c := Card{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		Title:       "An open PR",
		Column:      ColumnDevelop,
		AgentStatus: AgentStatusReviewing,
		Metadata:    map[string]any{},
	}
	c.PRNumber = &prNumber
	url := "https://example.com/pr/" + uuid.NewString()
	c.PRURL = &url
	return store.seed(c)
}

func TestWebhook_PRClosedMerged_MovesCardToDone(t *testing.T) {
	store := newFakeCardStore()
	card := seedCardWithPR(store, 42)
	app := newWebhookTestApp(t, store)

	body := pullRequestBody(t, "closed", 42, true)
	resp := sendWebhook(t, app, body, signWebhookBody(t, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	moves := store.Moves()
	if len(moves) != 1 {
		t.Fatalf("expected exactly 1 Move call, got %d (moves=%+v)", len(moves), moves)
	}
	if moves[0].Column != ColumnDone {
		t.Fatalf("expected move to %s, got %s", ColumnDone, moves[0].Column)
	}
	if moves[0].CardID != card.ID {
		t.Fatalf("Move targeted wrong card: got %s want %s", moves[0].CardID, card.ID)
	}
}

func TestWebhook_PRClosedUnmerged_MovesCardToReview(t *testing.T) {
	store := newFakeCardStore()
	seedCardWithPR(store, 99)
	app := newWebhookTestApp(t, store)

	body := pullRequestBody(t, "closed", 99, false)
	resp := sendWebhook(t, app, body, signWebhookBody(t, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	moves := store.Moves()
	if len(moves) != 1 {
		t.Fatalf("expected exactly 1 Move call, got %d", len(moves))
	}
	if moves[0].Column != ColumnReview {
		t.Fatalf("unmerged close should park the card in %s, got %s", ColumnReview, moves[0].Column)
	}
}

func TestWebhook_InvalidSignature_Returns401(t *testing.T) {
	store := newFakeCardStore()
	seedCardWithPR(store, 7)
	app := newWebhookTestApp(t, store)

	body := pullRequestBody(t, "closed", 7, true)
	resp := sendWebhook(t, app, body, "sha256=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad signature, got %d", resp.StatusCode)
	}
	if got := store.Moves(); len(got) != 0 {
		t.Fatalf("no Move calls expected on rejected webhook, got %d", len(got))
	}
}

func TestWebhook_MissingSignature_Returns401(t *testing.T) {
	store := newFakeCardStore()
	app := newWebhookTestApp(t, store)
	body := pullRequestBody(t, "closed", 1, true)
	resp := sendWebhook(t, app, body, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when signature header is missing, got %d", resp.StatusCode)
	}
}

func TestWebhook_PRClosed_NoMatchingCard_NoOp(t *testing.T) {
	store := newFakeCardStore()
	app := newWebhookTestApp(t, store)

	body := pullRequestBody(t, "closed", 12345, true)
	resp := sendWebhook(t, app, body, signWebhookBody(t, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("missing card should still 200 (best-effort), got %d", resp.StatusCode)
	}
	if got := store.Moves(); len(got) != 0 {
		t.Fatalf("no Move calls expected when card lookup misses, got %d", len(got))
	}
}
