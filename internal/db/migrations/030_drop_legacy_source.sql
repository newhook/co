-- +up
-- Drop the legacy source column now that we have structured source_type and source_name
ALTER TABLE pr_feedback DROP COLUMN source;

-- +down
ALTER TABLE pr_feedback ADD COLUMN source TEXT NOT NULL DEFAULT '';
