-- +up
-- Drop the redundant metadata column now that we have structured context
ALTER TABLE pr_feedback DROP COLUMN metadata;

-- +down
ALTER TABLE pr_feedback ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';
