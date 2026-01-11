-- +up
-- Add base_branch column to works table
-- This tracks which branch the work's feature branch was based on
-- and which branch the PR should target when merging
ALTER TABLE works ADD COLUMN base_branch TEXT DEFAULT 'main';

-- +down
ALTER TABLE works DROP COLUMN base_branch;
