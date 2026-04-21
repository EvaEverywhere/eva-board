CREATE TABLE IF NOT EXISTS board_settings (
    user_id                UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    github_token_encrypted TEXT,
    github_owner           TEXT,
    github_repo            TEXT,
    repo_path              TEXT,
    codegen_agent          TEXT NOT NULL DEFAULT 'claude-code',
    max_verify_iterations  INTEGER NOT NULL DEFAULT 3,
    max_review_cycles      INTEGER NOT NULL DEFAULT 5,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
