-- +up
-- Remove any existing review_epic_id metadata entries
DELETE FROM task_metadata WHERE key = 'review_epic_id';

-- +down
-- WARNING: This migration performs a destructive data cleanup.
-- The review_epic_id metadata entries that were deleted cannot be restored
-- during rollback because the original data is not preserved.
--
-- If rollback is needed, you would need to:
-- 1. Restore from a database backup made before this migration
-- 2. Or manually recreate the review_epic_id entries if the data is available elsewhere
--
-- This is intentionally left as a no-op since we cannot recreate deleted data.