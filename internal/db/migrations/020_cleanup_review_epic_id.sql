-- +up
-- Remove any existing review_epic_id metadata entries
DELETE FROM task_metadata WHERE key = 'review_epic_id';

-- +down
-- No rollback for data cleanup