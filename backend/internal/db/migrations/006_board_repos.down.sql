ALTER TABLE board_cards DROP CONSTRAINT IF EXISTS board_cards_repo_id_fkey;
DROP INDEX IF EXISTS board_cards_user_repo_column_idx;
ALTER TABLE board_cards DROP COLUMN IF EXISTS repo_id;
DROP INDEX IF EXISTS board_repos_one_default_per_user;
DROP INDEX IF EXISTS board_repos_user_id_idx;
DROP TABLE IF EXISTS board_repos;
