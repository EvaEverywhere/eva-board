package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/EvaEverywhere/eva-board/backend/internal/codegen"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/llm"
	"github.com/google/uuid"
)

// fakeCardStore is an in-memory cardStore for tests. Only the methods
// the agent loop and HTTP handlers actually call need to be wired up;
// the rest panic so accidental wiring is loud.
type fakeCardStore struct {
	mu    sync.Mutex
	cards map[uuid.UUID]*Card

	// Recordings of mutations the tests typically assert on.
	statusHistory  []string
	worktreeBranch string
	pr             *fakePR
	reviewStatus   string
	moves          []fakeMove

	// getByPRNumber lets tests pre-seed the lookup result without
	// having to populate the by-PR-number index manually.
	getByPRNumber func(int) (*Card, error)
}

type fakePR struct {
	Number int
	URL    string
}

type fakeMove struct {
	UserID   uuid.UUID
	CardID   uuid.UUID
	Column   string
	Position int
}

func newFakeCardStore() *fakeCardStore {
	return &fakeCardStore{cards: map[uuid.UUID]*Card{}}
}

func (f *fakeCardStore) seed(card Card) *Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := card
	f.cards[c.ID] = &c
	return &c
}

func (f *fakeCardStore) Snapshot(id uuid.UUID) *Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cards[id]; ok {
		copy := *c
		return &copy
	}
	return nil
}

func (f *fakeCardStore) Statuses() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.statusHistory))
	copy(out, f.statusHistory)
	return out
}

func (f *fakeCardStore) Moves() []fakeMove {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeMove, len(f.moves))
	copy(out, f.moves)
	return out
}

func (f *fakeCardStore) Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (*Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &Card{
		ID:          uuid.New(),
		UserID:      userID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Column:      ColumnBacklog,
		AgentStatus: AgentStatusIdle,
		Metadata:    map[string]any{},
	}
	if c.Title == "" {
		return nil, ErrTitleRequired
	}
	f.cards[c.ID] = c
	copy := *c
	return &copy, nil
}

func (f *fakeCardStore) Get(ctx context.Context, userID, cardID uuid.UUID) (*Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok || c.UserID != userID {
		return nil, ErrCardNotFound
	}
	copy := *c
	return &copy, nil
}

func (f *fakeCardStore) GetByID(ctx context.Context, cardID uuid.UUID) (*Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return nil, ErrCardNotFound
	}
	copy := *c
	return &copy, nil
}

func (f *fakeCardStore) GetByPRNumber(ctx context.Context, prNumber int) (*Card, error) {
	if f.getByPRNumber != nil {
		return f.getByPRNumber(prNumber)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cards {
		if c.PRNumber != nil && *c.PRNumber == prNumber {
			copy := *c
			return &copy, nil
		}
	}
	return nil, ErrCardNotFound
}

func (f *fakeCardStore) List(ctx context.Context, userID uuid.UUID, column string) ([]Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Card{}
	for _, c := range f.cards {
		if c.UserID != userID {
			continue
		}
		if column != "" && c.Column != column {
			continue
		}
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeCardStore) Update(ctx context.Context, userID, cardID uuid.UUID, req UpdateRequest) (*Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok || c.UserID != userID {
		return nil, ErrCardNotFound
	}
	if req.Title != nil {
		c.Title = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		c.Description = *req.Description
	}
	if req.Metadata != nil {
		c.Metadata = req.Metadata
	}
	copy := *c
	return &copy, nil
}

func (f *fakeCardStore) Move(ctx context.Context, userID, cardID uuid.UUID, toColumn string, toPosition int) (*Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok || c.UserID != userID {
		return nil, ErrCardNotFound
	}
	if !IsValidColumn(toColumn) {
		return nil, ErrInvalidColumn
	}
	c.Column = toColumn
	c.Position = toPosition
	f.moves = append(f.moves, fakeMove{UserID: userID, CardID: cardID, Column: toColumn, Position: toPosition})
	copy := *c
	return &copy, nil
}

func (f *fakeCardStore) Delete(ctx context.Context, userID, cardID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok || c.UserID != userID {
		return ErrCardNotFound
	}
	delete(f.cards, c.ID)
	return nil
}

func (f *fakeCardStore) SetAgentStatus(ctx context.Context, cardID uuid.UUID, status string) error {
	if !IsValidAgentStatus(status) {
		return ErrInvalidStatus
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return ErrCardNotFound
	}
	c.AgentStatus = status
	f.statusHistory = append(f.statusHistory, status)
	return nil
}

func (f *fakeCardStore) SetWorktreeBranch(ctx context.Context, cardID uuid.UUID, branch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return ErrCardNotFound
	}
	c.WorktreeBranch = &branch
	f.worktreeBranch = branch
	return nil
}

func (f *fakeCardStore) SetPR(ctx context.Context, cardID uuid.UUID, number int, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return ErrCardNotFound
	}
	c.PRNumber = &number
	c.PRURL = &url
	f.pr = &fakePR{Number: number, URL: url}
	return nil
}

func (f *fakeCardStore) SetReviewStatus(ctx context.Context, cardID uuid.UUID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return ErrCardNotFound
	}
	c.ReviewStatus = &status
	f.reviewStatus = status
	return nil
}

func (f *fakeCardStore) SetMetadata(ctx context.Context, cardID uuid.UUID, key string, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cards[cardID]
	if !ok {
		return ErrCardNotFound
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata[key] = value
	return nil
}

// fakeCodegen is a codegen.Agent that records prompts and produces
// configurable outputs. By default each Run is a no-op success; callers
// override touchFile / runErr to drive specific scenarios.
type fakeCodegen struct {
	mu        sync.Mutex
	calls     int
	prompts   []string
	workDirs  []string
	touchFile func(workDir string, call int) error
	runErr    error

	// blockUntil, when non-nil, makes Run wait until either the
	// channel is closed or the context is cancelled. If ctx wins, Run
	// returns ctx.Err() so the caller's isCancelled() path fires.
	blockUntil <-chan struct{}
	// started is signalled the first time Run begins so tests can
	// race-free wait for the loop to enter codegen before cancelling.
	started chan struct{}
}

func (f *fakeCodegen) Name() string { return "fake-codegen" }

func (f *fakeCodegen) Run(ctx context.Context, prompt, workDir string, _ ...codegen.RunOption) (codegen.Result, error) {
	f.mu.Lock()
	f.calls++
	f.prompts = append(f.prompts, prompt)
	f.workDirs = append(f.workDirs, workDir)
	call := f.calls
	touch := f.touchFile
	runErr := f.runErr
	block := f.blockUntil
	startedCh := f.started
	f.mu.Unlock()

	if startedCh != nil {
		select {
		case <-startedCh: // already closed
		default:
			close(startedCh)
		}
	}

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return codegen.Result{ExitCode: -1, Output: "cancelled"}, ctx.Err()
		}
	}

	if runErr != nil {
		return codegen.Result{ExitCode: 1, Output: "fake codegen failure"}, runErr
	}
	if touch != nil {
		if err := touch(workDir, call); err != nil {
			return codegen.Result{ExitCode: 1, Output: err.Error()}, err
		}
	}
	return codegen.Result{ExitCode: 0, Output: "ok", Duration: time.Millisecond}, nil
}

func (f *fakeCodegen) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeLLM dispenses canned ChatCompletion responses. Tests load the
// responses queue with the JSON bodies the verify/review parsers expect
// in the order they will be requested.
type fakeLLM struct {
	mu        sync.Mutex
	responses []string
	calls     []llm.CompletionRequest
	err       error
}

func (f *fakeLLM) ChatCompletion(ctx context.Context, req llm.CompletionRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.err != nil {
		return "", f.err
	}
	if len(f.responses) == 0 {
		return "", fmt.Errorf("fakeLLM: no responses queued (call #%d)", len(f.calls))
	}
	out := f.responses[0]
	f.responses = f.responses[1:]
	return out, nil
}

func (f *fakeLLM) ChatCompletionFull(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	content, err := f.ChatCompletion(ctx, req)
	if err != nil {
		return llm.CompletionResponse{}, err
	}
	return llm.CompletionResponse{Content: content}, nil
}

// fakeAgentGitHub extends the read-only fakeGitHub with a recording
// CreatePR implementation that the agent loop hits when opening a PR.
// We keep settings-test fakeGitHub unchanged by composing here.
type fakeAgentGitHub struct {
	createCalls   int
	createErr     error
	createReq     github.CreatePRRequest
	createOwner   string
	createRepo    string
	prNumber      int
	prHTMLURL     string
}

func (f *fakeAgentGitHub) CreatePR(ctx context.Context, owner, repo string, req github.CreatePRRequest) (*github.PR, error) {
	f.createCalls++
	f.createOwner = owner
	f.createRepo = repo
	f.createReq = req
	if f.createErr != nil {
		return nil, f.createErr
	}
	num := f.prNumber
	if num == 0 {
		num = 101
	}
	url := f.prHTMLURL
	if url == "" {
		url = "https://example.com/pr/" + fmt.Sprintf("%d", num)
	}
	return &github.PR{Number: num, HTMLURL: url}, nil
}

func (f *fakeAgentGitHub) MergePR(ctx context.Context, owner, repo string, number int, req github.MergePRRequest) error {
	return errors.New("MergePR not implemented in fake")
}
func (f *fakeAgentGitHub) GetPRState(ctx context.Context, owner, repo string, number int) (*github.PRState, error) {
	return nil, errors.New("GetPRState not implemented in fake")
}
func (f *fakeAgentGitHub) CreateIssue(ctx context.Context, owner, repo string, req github.CreateIssueRequest) (*github.Issue, error) {
	return nil, errors.New("CreateIssue not implemented in fake")
}
func (f *fakeAgentGitHub) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	return errors.New("AddIssueComment not implemented in fake")
}
func (f *fakeAgentGitHub) ListIssues(ctx context.Context, owner, repo string, opts github.ListIssuesOptions) ([]github.Issue, error) {
	return nil, errors.New("ListIssues not implemented in fake")
}
func (f *fakeAgentGitHub) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	return errors.New("CloseIssue not implemented in fake")
}
func (f *fakeAgentGitHub) GetUser(ctx context.Context) (*github.User, error) {
	return &github.User{Login: "agent"}, nil
}
func (f *fakeAgentGitHub) ListUserRepos(ctx context.Context, opts github.ListUserReposOptions) ([]github.Repo, error) {
	return nil, nil
}
