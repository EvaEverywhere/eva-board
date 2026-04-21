package board

import (
	"context"

	"github.com/google/uuid"
)

// cardStore is the subset of *Service that the agent loop and HTTP
// handlers need. Keeping it as an unexported interface lets tests
// substitute an in-memory fake without spinning up Postgres while the
// production code path still uses *Service unchanged. *Service
// satisfies cardStore by virtue of implementing every method below.
type cardStore interface {
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (*Card, error)
	Get(ctx context.Context, userID, cardID uuid.UUID) (*Card, error)
	GetByID(ctx context.Context, cardID uuid.UUID) (*Card, error)
	GetByPRNumber(ctx context.Context, prNumber int) (*Card, error)
	List(ctx context.Context, userID uuid.UUID, column string) ([]Card, error)
	Update(ctx context.Context, userID, cardID uuid.UUID, req UpdateRequest) (*Card, error)
	Move(ctx context.Context, userID, cardID uuid.UUID, toColumn string, toPosition int) (*Card, error)
	Delete(ctx context.Context, userID, cardID uuid.UUID) error
	SetAgentStatus(ctx context.Context, cardID uuid.UUID, status string) error
	SetWorktreeBranch(ctx context.Context, cardID uuid.UUID, branch string) error
	SetPR(ctx context.Context, cardID uuid.UUID, number int, url string) error
	SetReviewStatus(ctx context.Context, cardID uuid.UUID, status string) error
	SetMetadata(ctx context.Context, cardID uuid.UUID, key string, value any) error
}

// Compile-time assertion that *Service still satisfies cardStore. If a
// new method is added to cardStore (or a Service method is removed),
// the build breaks here rather than at the first failing test.
var _ cardStore = (*Service)(nil)
