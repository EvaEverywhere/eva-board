CREATE TABLE IF NOT EXISTS board_cards (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    column_name     TEXT NOT NULL DEFAULT 'backlog' CHECK (column_name IN ('backlog', 'develop', 'review', 'pr', 'done')),
    position        INTEGER NOT NULL DEFAULT 0,
    agent_status    TEXT NOT NULL DEFAULT 'idle' CHECK (agent_status IN ('idle', 'running', 'verifying', 'reviewing', 'failed', 'succeeded')),
    worktree_branch TEXT,
    pr_number       INTEGER,
    pr_url          TEXT,
    review_status   TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_board_cards_user_column ON board_cards(user_id, column_name, position);
CREATE INDEX IF NOT EXISTS idx_board_cards_pr_number ON board_cards(pr_number) WHERE pr_number IS NOT NULL;
