DROP INDEX IF EXISTS board_cards_github_issue_number_idx;
ALTER TABLE board_cards DROP COLUMN IF EXISTS github_issue_url;
ALTER TABLE board_cards DROP COLUMN IF EXISTS github_issue_number;
