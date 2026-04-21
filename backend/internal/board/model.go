package board

import (
	"time"

	"github.com/google/uuid"
)

const (
	ColumnBacklog = "backlog"
	ColumnDevelop = "develop"
	ColumnReview  = "review"
	ColumnPR      = "pr"
	ColumnDone    = "done"
)

var Columns = []string{ColumnBacklog, ColumnDevelop, ColumnReview, ColumnPR, ColumnDone}

const (
	AgentStatusIdle      = "idle"
	AgentStatusRunning   = "running"
	AgentStatusVerifying = "verifying"
	AgentStatusReviewing = "reviewing"
	AgentStatusFailed    = "failed"
	AgentStatusSucceeded = "succeeded"
)

var AgentStatuses = []string{
	AgentStatusIdle,
	AgentStatusRunning,
	AgentStatusVerifying,
	AgentStatusReviewing,
	AgentStatusFailed,
	AgentStatusSucceeded,
}

type Card struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	RepoID         uuid.UUID
	Title          string
	Description    string
	Column         string
	Position       int
	AgentStatus    string
	WorktreeBranch *string
	PRNumber       *int
	PRURL          *string
	ReviewStatus   *string
	Metadata       map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateRequest struct {
	Title       string
	Description string
}

type UpdateRequest struct {
	Title       *string
	Description *string
	Metadata    map[string]any
}

func IsValidColumn(c string) bool {
	for _, col := range Columns {
		if col == c {
			return true
		}
	}
	return false
}

func IsValidAgentStatus(s string) bool {
	for _, st := range AgentStatuses {
		if st == s {
			return true
		}
	}
	return false
}
