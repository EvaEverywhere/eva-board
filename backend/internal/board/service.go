package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCardNotFound    = errors.New("card not found")
	ErrInvalidColumn   = errors.New("invalid column")
	ErrInvalidStatus   = errors.New("invalid agent status")
	ErrTitleRequired   = errors.New("title is required")
	ErrInvalidPosition = errors.New("invalid position")
)

type Service struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

const cardSelect = `
	id::text, user_id::text, title, description, column_name, position,
	agent_status, worktree_branch, pr_number, pr_url, review_status,
	metadata, created_at, updated_at
`

func (s *Service) Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (*Card, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrTitleRequired
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var nextPos int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(position) + 1, 0)
		FROM board_cards
		WHERE user_id = $1 AND column_name = $2
	`, userID, ColumnBacklog).Scan(&nextPos); err != nil {
		return nil, fmt.Errorf("compute next position: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO board_cards (user_id, title, description, column_name, position)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+cardSelect+`
	`, userID, title, strings.TrimSpace(req.Description), ColumnBacklog, nextPos)

	card, err := scanCardRow(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}
	return card, nil
}

// GetByID fetches a card without enforcing user ownership. Intended for
// system-internal callers like the autonomous agent loop, which is keyed on
// card ID and runs outside of any HTTP request.
func (s *Service) GetByID(ctx context.Context, cardID uuid.UUID) (*Card, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+cardSelect+`
		FROM board_cards
		WHERE id = $1
	`, cardID)
	card, err := scanCardRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCardNotFound
	}
	return card, err
}

func (s *Service) Get(ctx context.Context, userID, cardID uuid.UUID) (*Card, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+cardSelect+`
		FROM board_cards
		WHERE id = $1 AND user_id = $2
	`, cardID, userID)
	card, err := scanCardRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCardNotFound
	}
	return card, err
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, column string) ([]Card, error) {
	var rows pgx.Rows
	var err error
	if column == "" {
		rows, err = s.db.Query(ctx, `
			SELECT `+cardSelect+`
			FROM board_cards
			WHERE user_id = $1
			ORDER BY column_name, position, created_at
		`, userID)
	} else {
		if !IsValidColumn(column) {
			return nil, ErrInvalidColumn
		}
		rows, err = s.db.Query(ctx, `
			SELECT `+cardSelect+`
			FROM board_cards
			WHERE user_id = $1 AND column_name = $2
			ORDER BY position, created_at
		`, userID, column)
	}
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	defer rows.Close()

	out := []Card{}
	for rows.Next() {
		card, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, userID, cardID uuid.UUID, req UpdateRequest) (*Card, error) {
	current, err := s.Get(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			return nil, ErrTitleRequired
		}
		current.Title = t
	}
	if req.Description != nil {
		current.Description = strings.TrimSpace(*req.Description)
	}
	if req.Metadata != nil {
		current.Metadata = req.Metadata
	}

	metaJSON, err := marshalMetadata(current.Metadata)
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRow(ctx, `
		UPDATE board_cards
		SET title = $3, description = $4, metadata = $5::jsonb, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+cardSelect+`
	`, cardID, userID, current.Title, current.Description, string(metaJSON))

	updated, err := scanCardRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCardNotFound
	}
	return updated, err
}

func (s *Service) Move(ctx context.Context, userID, cardID uuid.UUID, toColumn string, toPosition int) (*Card, error) {
	if !IsValidColumn(toColumn) {
		return nil, ErrInvalidColumn
	}
	if toPosition < 0 {
		return nil, ErrInvalidPosition
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var fromColumn string
	var fromPosition int
	if err := tx.QueryRow(ctx, `
		SELECT column_name, position FROM board_cards
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, cardID, userID).Scan(&fromColumn, &fromPosition); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCardNotFound
		}
		return nil, fmt.Errorf("lock card: %w", err)
	}

	if fromColumn == toColumn {
		if fromPosition == toPosition {
			row := tx.QueryRow(ctx, `SELECT `+cardSelect+` FROM board_cards WHERE id = $1`, cardID)
			card, err := scanCardRow(row)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return card, nil
		}

		var maxPos int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(position), 0) FROM board_cards
			WHERE user_id = $1 AND column_name = $2 AND id <> $3
		`, userID, toColumn, cardID).Scan(&maxPos); err != nil {
			return nil, fmt.Errorf("max pos: %w", err)
		}
		clampedPos := toPosition
		if clampedPos > maxPos+1 {
			clampedPos = maxPos + 1
		}

		if clampedPos < fromPosition {
			if _, err := tx.Exec(ctx, `
				UPDATE board_cards
				SET position = position + 1, updated_at = now()
				WHERE user_id = $1 AND column_name = $2
				  AND id <> $3
				  AND position >= $4 AND position < $5
			`, userID, toColumn, cardID, clampedPos, fromPosition); err != nil {
				return nil, fmt.Errorf("shift down siblings: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE board_cards
				SET position = position - 1, updated_at = now()
				WHERE user_id = $1 AND column_name = $2
				  AND id <> $3
				  AND position > $4 AND position <= $5
			`, userID, toColumn, cardID, fromPosition, clampedPos); err != nil {
				return nil, fmt.Errorf("shift up siblings: %w", err)
			}
		}

		row := tx.QueryRow(ctx, `
			UPDATE board_cards
			SET position = $3, updated_at = now()
			WHERE id = $1 AND user_id = $2
			RETURNING `+cardSelect+`
		`, cardID, userID, clampedPos)
		card, err := scanCardRow(row)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit move: %w", err)
		}
		return card, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE board_cards
		SET position = position - 1, updated_at = now()
		WHERE user_id = $1 AND column_name = $2 AND position > $3
	`, userID, fromColumn, fromPosition); err != nil {
		return nil, fmt.Errorf("close source gap: %w", err)
	}

	var destMax int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(position) + 1, 0) FROM board_cards
		WHERE user_id = $1 AND column_name = $2
	`, userID, toColumn).Scan(&destMax); err != nil {
		return nil, fmt.Errorf("dest max: %w", err)
	}
	clampedPos := toPosition
	if clampedPos > destMax {
		clampedPos = destMax
	}

	if _, err := tx.Exec(ctx, `
		UPDATE board_cards
		SET position = position + 1, updated_at = now()
		WHERE user_id = $1 AND column_name = $2 AND position >= $3
	`, userID, toColumn, clampedPos); err != nil {
		return nil, fmt.Errorf("open dest slot: %w", err)
	}

	row := tx.QueryRow(ctx, `
		UPDATE board_cards
		SET column_name = $3, position = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+cardSelect+`
	`, cardID, userID, toColumn, clampedPos)
	card, err := scanCardRow(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit move: %w", err)
	}
	return card, nil
}

func (s *Service) Delete(ctx context.Context, userID, cardID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var column string
	var position int
	if err := tx.QueryRow(ctx, `
		SELECT column_name, position FROM board_cards
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, cardID, userID).Scan(&column, &position); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCardNotFound
		}
		return fmt.Errorf("lock card: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM board_cards WHERE id = $1 AND user_id = $2`, cardID, userID); err != nil {
		return fmt.Errorf("delete card: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE board_cards
		SET position = position - 1, updated_at = now()
		WHERE user_id = $1 AND column_name = $2 AND position > $3
	`, userID, column, position); err != nil {
		return fmt.Errorf("close gap: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *Service) SetAgentStatus(ctx context.Context, cardID uuid.UUID, status string) error {
	if !IsValidAgentStatus(status) {
		return ErrInvalidStatus
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE board_cards SET agent_status = $2, updated_at = now() WHERE id = $1
	`, cardID, status)
	if err != nil {
		return fmt.Errorf("set agent status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) SetWorktreeBranch(ctx context.Context, cardID uuid.UUID, branch string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE board_cards SET worktree_branch = $2, updated_at = now() WHERE id = $1
	`, cardID, branch)
	if err != nil {
		return fmt.Errorf("set worktree branch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) SetPR(ctx context.Context, cardID uuid.UUID, number int, url string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE board_cards SET pr_number = $2, pr_url = $3, updated_at = now() WHERE id = $1
	`, cardID, number, url)
	if err != nil {
		return fmt.Errorf("set pr: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) SetReviewStatus(ctx context.Context, cardID uuid.UUID, status string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE board_cards SET review_status = $2, updated_at = now() WHERE id = $1
	`, cardID, status)
	if err != nil {
		return fmt.Errorf("set review status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) SetMetadata(ctx context.Context, cardID uuid.UUID, key string, value any) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("metadata key is required")
	}
	patch, err := json.Marshal(map[string]any{key: value})
	if err != nil {
		return fmt.Errorf("marshal metadata patch: %w", err)
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE board_cards
		SET metadata = metadata || $2::jsonb, updated_at = now()
		WHERE id = $1
	`, cardID, string(patch))
	if err != nil {
		return fmt.Errorf("set metadata: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCardRow(r rowScanner) (*Card, error) {
	var (
		c          Card
		idStr      string
		userIDStr  string
		rawMeta    []byte
	)
	if err := r.Scan(
		&idStr,
		&userIDStr,
		&c.Title,
		&c.Description,
		&c.Column,
		&c.Position,
		&c.AgentStatus,
		&c.WorktreeBranch,
		&c.PRNumber,
		&c.PRURL,
		&c.ReviewStatus,
		&rawMeta,
		&c.CreatedAt,
		&c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse card id: %w", err)
	}
	uid, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	c.ID = id
	c.UserID = uid
	if len(rawMeta) > 0 {
		meta := map[string]any{}
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
		c.Metadata = meta
	} else {
		c.Metadata = map[string]any{}
	}
	return &c, nil
}

func marshalMetadata(metadata map[string]any) ([]byte, error) {
	if len(metadata) == 0 {
		return []byte(`{}`), nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	return raw, nil
}
