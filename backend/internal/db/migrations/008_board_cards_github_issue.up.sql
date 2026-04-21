ALTER TABLE board_cards ADD COLUMN IF NOT EXISTS github_issue_number INTEGER;
ALTER TABLE board_cards ADD COLUMN IF NOT EXISTS github_issue_url TEXT;

CREATE INDEX IF NOT EXISTS board_cards_github_issue_number_idx
    ON board_cards (github_issue_number)
    WHERE github_issue_number IS NOT NULL;
