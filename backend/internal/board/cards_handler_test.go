package board

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
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
	h.SetAgentFactory(func(ctx context.Context, _, _ uuid.UUID) (AgentLifecycle, error) {
		return lc, nil
	})
	h.Register(app)
	return app
}

func makeBacklogCard(store *fakeCardStore, userID uuid.UUID) *Card {
	c := Card{
		ID:          uuid.New(),
		UserID:      userID,
		RepoID:      uuid.New(),
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
		RepoID:      uuid.New(),
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

// TestCardsHandler_StopHitsSameManagerInstance is the regression test
// that motivated AgentRegistry: a Move-into-develop request used to
// build one manager, kick off a goroutine inside it, and discard the
// reference; a follow-up agent/stop request would build a *different*
// manager and call StopAgent on an empty map. With the registry both
// requests resolve to the same lifecycle and the stop actually fires.
func TestCardsHandler_StopHitsSameManagerInstance(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	card := makeBacklogCard(store, userID)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})

	lc := &recordingLifecycle{}
	h := NewCardsHandler(store, nil, nil, nil, nil)
	h.SetAgentFactory(func(_ context.Context, _, _ uuid.UUID) (AgentLifecycle, error) {
		return lc, nil
	})
	h.Register(app)

	if resp := postMove(t, app, card.ID, ColumnDevelop); resp.StatusCode != http.StatusOK {
		t.Fatalf("move→develop: expected 200, got %d", resp.StatusCode)
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/board/cards/"+card.ID.String()+"/agent/stop", nil)
	stopResp, err := app.Test(stopReq, 5_000)
	if err != nil {
		t.Fatalf("agent/stop: %v", err)
	}
	if stopResp.StatusCode != http.StatusOK {
		t.Fatalf("agent/stop: expected 200, got %d", stopResp.StatusCode)
	}

	if got := lc.Starts(); len(got) != 1 || got[0] != card.ID {
		t.Fatalf("expected one StartAgent for %s, got %v", card.ID, got)
	}
	if got := lc.Stops(); len(got) != 1 || got[0] != card.ID {
		t.Fatalf("expected one StopAgent for %s, got %v", card.ID, got)
	}
}

// TestResolveCodegenAgent_PerUserOverride asserts that ResolveCodegenAgent
// layers per-user board_settings on top of the server-level CODEGEN_*
// defaults: a non-empty CodegenCommand from the user replaces the env
// fallback, the user's CodegenAgent type wins, and unset fields fall
// back to the defaults. This used to live on CardsHandler before the
// AgentRegistry refactor moved manager construction out of the handler.
func TestResolveCodegenAgent_PerUserOverride(t *testing.T) {
	defaults := codegen.Config{
		Type:           "claude-code",
		Command:        "default-cli",
		Args:           []string{"--default"},
		Timeout:        5 * time.Minute,
		MaxOutputBytes: 1024,
	}

	cases := []struct {
		name        string
		settings    Settings
		wantType    string
		wantCommand string
		wantArgs    []string
	}{
		{
			name: "user_overrides_command_and_args",
			settings: Settings{
				CodegenAgent:   "generic",
				CodegenCommand: "aider",
				CodegenArgs:    []string{"--yes", "--no-stream"},
			},
			wantType:    "generic",
			wantCommand: "aider",
			wantArgs:    []string{"--yes", "--no-stream"},
		},
		{
			name:        "empty_user_falls_back_to_defaults",
			settings:    Settings{CodegenArgs: []string{}},
			wantType:    "claude-code",
			wantCommand: "default-cli",
			wantArgs:    []string{"--default"},
		},
		{
			name: "user_agent_only_keeps_default_command",
			settings: Settings{
				CodegenAgent: "generic",
				CodegenArgs:  []string{},
			},
			wantType:    "generic",
			wantCommand: "default-cli",
			wantArgs:    []string{"--default"},
		},
	}

	shared := nopAgent{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var built codegen.Config
			factory := func(cfg codegen.Config) (codegen.Agent, error) {
				built = cfg
				return nopAgent{}, nil
			}
			_, resolved, err := ResolveCodegenAgent(tc.settings, defaults, shared, factory)
			if err != nil {
				t.Fatalf("ResolveCodegenAgent: %v", err)
			}
			if resolved.Type != tc.wantType {
				t.Fatalf("resolved.Type = %q, want %q", resolved.Type, tc.wantType)
			}
			if resolved.Command != tc.wantCommand {
				t.Fatalf("resolved.Command = %q, want %q", resolved.Command, tc.wantCommand)
			}
			if !equalStrings(resolved.Args, tc.wantArgs) {
				t.Fatalf("resolved.Args = %v, want %v", resolved.Args, tc.wantArgs)
			}
			// When the resolved config matches the defaults exactly the
			// shared agent is reused and the factory is never called.
			if codegenConfigEqual(resolved, defaults) {
				if built.Type != "" {
					t.Fatalf("factory unexpectedly invoked when resolved == defaults")
				}
			} else {
				if built.Type != tc.wantType {
					t.Fatalf("factory built type = %q, want %q", built.Type, tc.wantType)
				}
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nopAgent is a no-op codegen.Agent for tests that only need the
// resolver path.
type nopAgent struct{}

func (nopAgent) Name() string { return "nop" }
func (nopAgent) Run(_ context.Context, _ string, _ string, _ ...codegen.RunOption) (codegen.Result, error) {
	return codegen.Result{}, nil
}
