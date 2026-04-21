-- Phase out the temporary nullable repo_id added in migration 006.
-- Cards whose user had no settings (and thus no backfilled repo) are
-- unreachable from the board UI today and have no clear home; remove
-- them so we can guarantee every card has a repo.
DELETE FROM board_cards WHERE repo_id IS NULL;
ALTER TABLE board_cards ALTER COLUMN repo_id SET NOT NULL;
