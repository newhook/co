-- +up
-- Add root_issue_id column to track the single root issue for each work
ALTER TABLE works ADD COLUMN root_issue_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_works_root_issue_id ON works(root_issue_id);

-- +down
DROP INDEX IF EXISTS idx_works_root_issue_id;
-- SQLite doesn't support DROP COLUMN, so we need to recreate the table
-- For simplicity, we'll just leave the column (it has a default value)
