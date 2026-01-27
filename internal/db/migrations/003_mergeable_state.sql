-- +up
-- Add mergeable_state column to track PR merge status
ALTER TABLE works ADD COLUMN mergeable_state TEXT NOT NULL DEFAULT '';

-- +down
-- SQLite doesn't support DROP COLUMN directly, but we document the intent
-- This requires recreating the table without the column
