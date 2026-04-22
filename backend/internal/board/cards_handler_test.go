package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/teslashibe/codegen-go"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
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
	h := NewCardsHandler(store, nil, nil, nil, nil, nil, nil)
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
	h := NewCardsHandler(store, nil, nil, nil, nil, nil, nil)
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

// TestCardsHandler_DraftCard_NoServiceReturns503 asserts the route
// surfaces a 503 (not a 500 panic) when the server was built without a
// draft service. The mobile client uses the 503 as a signal to fall
// back to raw "Create".
func TestCardsHandler_DraftCard_NoServiceReturns503(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	app := newCardsTestApp(t, store, userID, &recordingLifecycle{})

	body, _ := json.Marshal(map[string]string{"title": "idea", "description": "body"})
	// resolveRepoID requires ?repo_id when repos is nil; we pass one
	// so the request reaches the draft handler rather than stopping
	// at the repo resolver.
	url := "/board/cards/draft?repo_id=" + uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

// TestCardsHandler_DraftCard_HappyPath wires a real DraftService
// backed by a fake repo locator and a fake codegen agent and asserts
// the route decodes the draft and returns it verbatim.
func TestCardsHandler_DraftCard_HappyPath(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	repo := newDraftTestRepo()
	canned := `{
  "title": "Fix pagination on /items",
  "description": "Pagination regressed when we added cursor support.",
  "acceptance_criteria": [
    "GET /items?cursor=<token> returns the next page",
    "response includes next_cursor when more data exists"
  ],
  "reasoning": "Cursor logic is in items_handler.go; tests cover only the limit path."
}`
	fc := &fakeCodegen{reviewerOutputs: []string{canned}}
	draftSvc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: repo},
		agent: fc,
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})
	h := NewCardsHandler(store, nil, nil, nil, nil, draftSvc, nil)
	h.Register(app)

	body, _ := json.Marshal(map[string]string{"title": "pagination is broken", "description": ""})
	url := "/board/cards/draft?repo_id=" + uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got CardDraft
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Title != "Fix pagination on /items" {
		t.Errorf("title = %q, want %q", got.Title, "Fix pagination on /items")
	}
	if len(got.AcceptanceCriteria) != 2 {
		t.Fatalf("acceptance_criteria len = %d, want 2", len(got.AcceptanceCriteria))
	}
	if got.Reasoning == "" {
		t.Error("reasoning should be populated")
	}
}

// TestCardsHandler_DraftCard_EmptyTitle asserts the route validates
// the title before calling the (expensive) agent.
func TestCardsHandler_DraftCard_EmptyTitle(t *testing.T) {
	store := newFakeCardStore()
	userID := uuid.New()
	fc := &fakeCodegen{}
	draftSvc := &DraftService{
		repos: &fakeDraftRepoLocator{repo: newDraftTestRepo()},
		agent: fc,
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})
	h := NewCardsHandler(store, nil, nil, nil, nil, draftSvc, nil)
	h.Register(app)

	body, _ := json.Marshal(map[string]string{"title": "   ", "description": ""})
	url := "/board/cards/draft?repo_id=" + uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if fc.Calls() != 0 {
		t.Fatalf("agent should not have run for empty title, got %d calls", fc.Calls())
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

// recordingIssueCreator captures every IssueCreator call so the
// create-card tests can assert exactly what was pushed to GitHub
// without spinning up a settings + repos + ghFactory stack. The zero
// value behaves like a successful push returning issue 100; tests can
// override either issue or err to drive the alternative paths.
type recordingIssueCreator struct {
	mu     sync.Mutex
	calls  int
	cards  []uuid.UUID
	repoID uuid.UUID
	userID uuid.UUID

	issue *github.Issue
	err   error
}

func (r *recordingIssueCreator) create(_ context.Context, userID, repoID uuid.UUID, card *Card) (*github.Issue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.userID = userID
	r.repoID = repoID
	r.cards = append(r.cards, card.ID)
	if r.err != nil {
		return nil, r.err
	}
	if r.issue != nil {
		return r.issue, nil
	}
	return &github.Issue{Number: 100, HTMLURL: "https://github.com/o/r/issues/100"}, nil
}

func (r *recordingIssueCreator) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// newCreateCardApp wires a CardsHandler with the supplied store and
// (optionally) issue creator. The test middleware injects userID into
// locals so the handler reaches the create path without going through
// the real auth middleware. repoID is required by resolveRepoID and
// is supplied as ?repo_id by the request helpers below.
func newCreateCardApp(t *testing.T, store cardStore, issuer IssueCreator) *fiber.App {
	t.Helper()
	app := fiber.New()
	userID := uuid.New()
	app.Use(func(c *fiber.Ctx) error {
		httputil.SetUserID(c, userID.String())
		return c.Next()
	})
	h := NewCardsHandler(store, nil, nil, nil, nil, nil, nil)
	if issuer != nil {
		h.SetIssueCreator(issuer)
	}
	h.Register(app)
	return app
}

func postCreateCard(t *testing.T, app *fiber.App, body createCardBody) (*http.Response, *cardView) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "/board/cards?repo_id=" + uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return resp, nil
	}
	var view cardView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp, &view
}

// TestCardsHandler_Create_DefaultPushesIssue covers the happy path:
// the request omits `create_issue` so the handler defaults to true,
// invokes the issue creator, persists the returned number+url, and
// includes both fields on the 201 response so the client doesn't have
// to refetch.
func TestCardsHandler_Create_DefaultPushesIssue(t *testing.T) {
	store := newFakeCardStore()
	issuer := &recordingIssueCreator{
		issue: &github.Issue{Number: 77, HTMLURL: "https://github.com/o/r/issues/77"},
	}
	app := newCreateCardApp(t, store, issuer.create)

	resp, view := postCreateCard(t, app, createCardBody{Title: "ship it", Description: "details"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if issuer.Calls() != 1 {
		t.Fatalf("issue creator should have fired once, got %d calls", issuer.Calls())
	}
	if view.GitHubIssueNumber == nil || *view.GitHubIssueNumber != 77 {
		t.Fatalf("github_issue_number wrong on response: %+v", view.GitHubIssueNumber)
	}
	if view.GitHubIssueURL == nil || *view.GitHubIssueURL != "https://github.com/o/r/issues/77" {
		t.Fatalf("github_issue_url wrong on response: %+v", view.GitHubIssueURL)
	}

	cardID, err := uuid.Parse(view.ID)
	if err != nil {
		t.Fatalf("parse view id: %v", err)
	}
	stored := store.Snapshot(cardID)
	if stored == nil {
		t.Fatalf("card not in store after create")
	}
	if stored.GitHubIssueNumber == nil || *stored.GitHubIssueNumber != 77 {
		t.Fatalf("issue number not persisted on stored card: %+v", stored.GitHubIssueNumber)
	}
}

// TestCardsHandler_Create_OptOut asserts that an explicit
// create_issue=false bypasses the issue creator entirely. This is the
// "I'm scribbling notes, don't spam my GitHub" path the modal exposes
// via a checkbox.
func TestCardsHandler_Create_OptOut(t *testing.T) {
	store := newFakeCardStore()
	issuer := &recordingIssueCreator{}
	app := newCreateCardApp(t, store, issuer.create)

	optOut := false
	resp, view := postCreateCard(t, app, createCardBody{Title: "draft", CreateIssue: &optOut})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if issuer.Calls() != 0 {
		t.Fatalf("issue creator should not fire when opted out, got %d calls", issuer.Calls())
	}
	if view.GitHubIssueNumber != nil || view.GitHubIssueURL != nil {
		t.Fatalf("issue fields should be absent on opt-out, got %+v / %+v", view.GitHubIssueNumber, view.GitHubIssueURL)
	}
}

// TestCardsHandler_Create_PartialSuccess covers the "GitHub failed
// but the card is already saved" path. The user's typing should never
// be lost because the GH API blipped — return 201 with the bare card
// and let the user retry from the UI later.
func TestCardsHandler_Create_PartialSuccess(t *testing.T) {
	store := newFakeCardStore()
	issuer := &recordingIssueCreator{err: errors.New("github 502")}
	app := newCreateCardApp(t, store, issuer.create)

	resp, view := postCreateCard(t, app, createCardBody{Title: "still saved"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("partial success must still 201, got %d", resp.StatusCode)
	}
	if issuer.Calls() != 1 {
		t.Fatalf("issue creator should have been attempted, got %d calls", issuer.Calls())
	}
	if view.GitHubIssueNumber != nil || view.GitHubIssueURL != nil {
		t.Fatalf("issue fields must be absent on partial success, got %+v / %+v", view.GitHubIssueNumber, view.GitHubIssueURL)
	}
	if view.Title != "still saved" {
		t.Fatalf("card title not persisted on partial success: %+v", view.Title)
	}
}

// TestCardsHandler_Create_NoCreatorWired covers the dev/test path
// where ghFactory + settings + repos aren't all wired (e.g. a
// freshly-cloned dev box with no GitHub token configured). The card
// must still be created successfully — the issue push is optional
// infrastructure.
func TestCardsHandler_Create_NoCreatorWired(t *testing.T) {
	store := newFakeCardStore()
	app := newCreateCardApp(t, store, nil)

	resp, view := postCreateCard(t, app, createCardBody{Title: "no gh"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if view.GitHubIssueNumber != nil || view.GitHubIssueURL != nil {
		t.Fatalf("issue fields should be absent without a creator, got %+v / %+v", view.GitHubIssueNumber, view.GitHubIssueURL)
	}
}

// TestBuildPRBody_PrependsClosesWhenIssueLinked is the prompts.go
// regression test for the "Closes #N" hand-off. Without this, the
// agent's PR opens but the issue stays stuck open after merge,
// silently leaking backlog items.
func TestBuildPRBody_PrependsClosesWhenIssueLinked(t *testing.T) {
	issue := 123
	card := Card{Title: "Add foo", Description: "do the thing", GitHubIssueNumber: &issue}
	body := buildPRBody(card, nil, ReviewResult{})
	if !startsWith(body, "Closes #123\n\n") {
		t.Fatalf("PR body should start with Closes line, got prefix:\n%q", firstN(body, 80))
	}

	// No issue → no closes line. Other PRs (e.g. drive-by fixes the
	// agent runs without an originating issue) must not get a stray
	// Closes #0 prepended.
	bare := buildPRBody(Card{Title: "Add foo"}, nil, ReviewResult{})
	if startsWith(bare, "Closes ") {
		t.Fatalf("PR body without issue should not have Closes line: %q", firstN(bare, 80))
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
