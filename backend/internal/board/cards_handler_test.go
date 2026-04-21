package board

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/httputil"
)

// recordingLifecycle is an AgentLifecycle that just records the calls
// it receives. Tests inject it via SetAgentFactory to assert column
// transitions trigger the right lifecycle hook.
type recordingLifecycle struct {
	mu        sync.Mutex
	startCalls []uuid.UUID
	stopCalls  []uuid.UUID
	feedback   []string
	startErr   error
}

func (r *recordingLifecycle) StartAgent(_ context.Context, cardID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalls = append(r.startCalls, cardID)
	return r.startErr
}

func (r *recordingLifecycle) StopAgent(cardID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopCalls = append(r.stopCalls, cardID)
	return nil
}

func (r *recordingLifecycle) SubmitFeedback(_ uuid.UUID, fb string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.feedback = append(r.feedback, fb)
	return nil
}

func (r *recordingLifecycle) Starts() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, len(r.startCalls))
	copy(out, r.startCalls)
	return out
}
func (r *recordingLifecycle) Stops() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, len(r.stopCalls))
	copy(out, r.stopCalls)
	return out
}

// newCardsTestApp builds a Fiber app that injects userID into locals
// (mimicking the auth middleware) and registers the CardsHandler with
// the supplied lifecycle factory.
func newCardsTestApp(t *testing.T, store cardStore, userID uuid.UUID, lc AgentLifecycle) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})
	h := NewCardsHandler(store, nil, nil, nil, nil)
	h.SetAgentFactory(func(ctx context.Context, _ uuid.UUID) (AgentLifecycle, error) {
		return lc, nil
	})
	h.Register(app)
	return app
}

func makeBacklogCard(store *fakeCardStore, userID uuid.UUID) *Card {
	c := Card{
		ID:          uuid.New(),
		UserID:      userID,
		Title:       "Some work",
		Column:      ColumnBacklog,
		AgentStatus: AgentStatusIdle,
		Metadata:    map[string]any{},
	}
	return store.seed(c)
}

func makeDevelopCard(store *fakeCardStore, userID uuid.UUID) *Card {
	c := Card{
		ID:          uuid.New(),
		UserID:      userID,
		Title:       "Active work",
		Column:      ColumnDevelop,
		AgentStatus: AgentStatusRunning,
		Metadata:    map[string]any{},
	}
	return store.seed(c)
}

func postMove(t *testing.T, app *fiber.App, cardID uuid.UUID, toCol string) *http.Response {
	t.Helper()
	body, err := json.Marshal(moveCardBody{ToColumn: toCol, ToPosition: 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/board/cards/"+cardID.String()+"/move", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func TestCardsHandler_MoveToDevelop_StartsAgent(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeBacklogCard(store, userID)
	lc := &recordingLifecycle{}
	app := newCardsTestApp(t, store, userID, lc)

	resp := postMove(t, app, card.ID, ColumnDevelop)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	starts := lc.Starts()
	if len(starts) != 1 {
		t.Fatalf("expected 1 StartAgent call, got %d", len(starts))
	}
	if starts[0] != card.ID {
		t.Fatalf("StartAgent received wrong card id: %s vs %s", starts[0], card.ID)
	}
	if got := lc.Stops(); len(got) != 0 {
		t.Fatalf("StopAgent should not be called on backlog→develop, got %d", len(got))
	}

	final := store.Snapshot(card.ID)
	if final.Column != ColumnDevelop {
		t.Fatalf("card column = %s, want develop", final.Column)
	}
}

func TestCardsHandler_MoveDevelopToReview_StopsAgent(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeDevelopCard(store, userID)
	lc := &recordingLifecycle{}
	app := newCardsTestApp(t, store, userID, lc)

	resp := postMove(t, app, card.ID, ColumnReview)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	stops := lc.Stops()
	if len(stops) != 1 {
		t.Fatalf("expected 1 StopAgent call, got %d", len(stops))
	}
	if stops[0] != card.ID {
		t.Fatalf("StopAgent received wrong card id: %s vs %s", stops[0], card.ID)
	}
	if got := lc.Starts(); len(got) != 0 {
		t.Fatalf("StartAgent should not fire on develop→review, got %d", len(got))
	}
}

func TestCardsHandler_MoveDevelopToDevelop_DoesNotRestartAgent(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeDevelopCard(store, userID)
	lc := &recordingLifecycle{}
	app := newCardsTestApp(t, store, userID, lc)

	// A move within the same column (re-ordering) must not trigger
	// the agent — re-running on every drag would be a regression.
	resp := postMove(t, app, card.ID, ColumnDevelop)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := lc.Starts(); len(got) != 0 {
		t.Fatalf("StartAgent fired on develop→develop reorder: %d calls", len(got))
	}
}

func TestCardsHandler_MoveToOtherColumn_NoLifecycleHook(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeBacklogCard(store, userID)
	lc := &recordingLifecycle{}
	app := newCardsTestApp(t, store, userID, lc)

	resp := postMove(t, app, card.ID, ColumnDone)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := lc.Starts(); len(got) != 0 {
		t.Fatalf("backlog→done must not start agent, got %d starts", len(got))
	}
	if got := lc.Stops(); len(got) != 0 {
		t.Fatalf("backlog→done must not stop agent, got %d stops", len(got))
	}
}
