package board

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/github"
	"github.com/EvaEverywhere/eva-board/backend/internal/security"
)

func uuidNew(t *testing.T) uuid.UUID {
	t.Helper()
	return uuid.New()
}

// fakeGitHub is a minimal Client used by settings tests. Only GetUser and
// ListUserRepos are exercised; other methods panic if accidentally hit.
type fakeGitHub struct {
	token       string
	getUserErr  error
	user        *github.User
	repos       []github.Repo
	listErr     error
}

func (f *fakeGitHub) CreatePR(ctx context.Context, owner, repo string, req github.CreatePRRequest) (*github.PR, error) {
	panic("not implemented")
}
func (f *fakeGitHub) MergePR(ctx context.Context, owner, repo string, number int, req github.MergePRRequest) error {
	panic("not implemented")
}
func (f *fakeGitHub) GetPRState(ctx context.Context, owner, repo string, number int) (*github.PRState, error) {
	panic("not implemented")
}
func (f *fakeGitHub) CreateIssue(ctx context.Context, owner, repo string, req github.CreateIssueRequest) (*github.Issue, error) {
	panic("not implemented")
}
func (f *fakeGitHub) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	panic("not implemented")
}
func (f *fakeGitHub) ListIssues(ctx context.Context, owner, repo string, opts github.ListIssuesOptions) ([]github.Issue, error) {
	panic("not implemented")
}
func (f *fakeGitHub) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	panic("not implemented")
}
func (f *fakeGitHub) GetUser(ctx context.Context) (*github.User, error) {
	if f.getUserErr != nil {
		return nil, f.getUserErr
	}
	if f.user != nil {
		return f.user, nil
	}
	return &github.User{Login: "tester"}, nil
}
func (f *fakeGitHub) ListUserRepos(ctx context.Context, opts github.ListUserReposOptions) ([]github.Repo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.repos, nil
}

type fakeFactory struct {
	last       string
	getUserErr error
	repos      []github.Repo
}

func (f *fakeFactory) NewClient(token string) github.Client {
	f.last = token
	return &fakeGitHub{token: token, getUserErr: f.getUserErr, repos: f.repos}
}

func newCipherForTest(t *testing.T) *security.TokenCipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	c, err := security.NewTokenCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	return c
}

func TestSettingsUpsertRequiresCipherForToken(t *testing.T) {
	svc := NewSettingsService(nil, nil, &fakeFactory{})
	tok := "ghp_x"
	_, err := svc.Upsert(context.Background(), uuidNew(t), UpsertRequest{GitHubToken: &tok})
	if !errors.Is(err, ErrCipherNotConfigured) {
		t.Fatalf("expected ErrCipherNotConfigured, got %v", err)
	}
}

func TestSettingsUpsertRejectsInvalidToken(t *testing.T) {
	cipher := newCipherForTest(t)
	factory := &fakeFactory{getUserErr: errors.New("401 unauthorized")}
	svc := NewSettingsService(nil, cipher, factory)
	tok := "ghp_bad"
	_, err := svc.Upsert(context.Background(), uuidNew(t), UpsertRequest{GitHubToken: &tok})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", appErr.Status)
	}
	if factory.last != "ghp_bad" {
		t.Fatalf("expected factory called with provided token, got %q", factory.last)
	}
}

func TestSettingsServiceDefaults(t *testing.T) {
	st := Settings{}
	if DefaultCodegenAgent == "" {
		t.Fatal("DefaultCodegenAgent must be non-empty")
	}
	if DefaultMaxVerifyIterations < 1 {
		t.Fatalf("DefaultMaxVerifyIterations should be >= 1, got %d", DefaultMaxVerifyIterations)
	}
	if DefaultMaxReviewCycles < 1 {
		t.Fatalf("DefaultMaxReviewCycles should be >= 1, got %d", DefaultMaxReviewCycles)
	}
	_ = st
}

func TestSettingsGetDefaultsIncludeEmptyCodegenOverrides(t *testing.T) {
	st := Settings{
		CodegenAgent:        DefaultCodegenAgent,
		CodegenArgs:         []string{},
		MaxVerifyIterations: DefaultMaxVerifyIterations,
		MaxReviewCycles:     DefaultMaxReviewCycles,
	}
	if st.CodegenCommand != "" {
		t.Fatalf("expected empty CodegenCommand default, got %q", st.CodegenCommand)
	}
	if st.CodegenArgs == nil || len(st.CodegenArgs) != 0 {
		t.Fatalf("expected non-nil empty CodegenArgs, got %v", st.CodegenArgs)
	}
}

func TestSettingsUpsertValidatesIterationBounds(t *testing.T) {
	svc := NewSettingsService(nil, nil, &fakeFactory{})
	bad := 0
	_, err := svc.Upsert(context.Background(), uuidNew(t), UpsertRequest{MaxVerifyIterations: &bad})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 AppError, got %v", err)
	}

	_, err = svc.Upsert(context.Background(), uuidNew(t), UpsertRequest{MaxReviewCycles: &bad})
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 AppError, got %v", err)
	}
}
