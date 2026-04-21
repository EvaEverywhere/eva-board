CREATE TABLE IF NOT EXISTS board_repos (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner           TEXT NOT NULL,
    name            TEXT NOT NULL,
    repo_path       TEXT NOT NULL,
    default_branch  TEXT NOT NULL DEFAULT 'main',
    is_default      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, owner, name)
);

CREATE UNIQUE INDEX IF NOT EXISTS board_repos_one_default_per_user
    ON board_repos (user_id) WHERE is_default = true;

CREATE INDEX IF NOT EXISTS board_repos_user_id_idx ON board_repos (user_id);

-- Backfill: every existing board_settings row with a configured repo
-- becomes a board_repos row, marked default. Empty repos (settings with
-- no repo info) are skipped.
INSERT INTO board_repos (user_id, owner, name, repo_path, is_default)
SELECT user_id, github_owner, github_repo, repo_path, true
FROM board_settings
WHERE github_owner IS NOT NULL
  AND github_owner <> ''
  AND github_repo IS NOT NULL
  AND github_repo <> ''
  AND repo_path IS NOT NULL
  AND repo_path <> '';

-- Add repo_id to cards. Nullable initially so the backfill can populate
-- it; we tighten to NOT NULL in the next migration step.
ALTER TABLE board_cards ADD COLUMN IF NOT EXISTS repo_id UUID;

-- Backfill existing cards to point at the user's default repo. Cards
-- whose user has no repo (no settings or empty) are left with NULL
-- repo_id; they will be unreachable from any board view until the user
-- adds a repo. We do not delete them so the user does not lose data.
UPDATE board_cards c
SET repo_id = r.id
FROM board_repos r
WHERE r.user_id = c.user_id AND r.is_default = true
  AND c.repo_id IS NULL;

CREATE INDEX IF NOT EXISTS board_cards_user_repo_column_idx
    ON board_cards (user_id, repo_id, column_name, position);

ALTER TABLE board_cards
    ADD CONSTRAINT board_cards_repo_id_fkey
    FOREIGN KEY (repo_id) REFERENCES board_repos(id) ON DELETE CASCADE;
