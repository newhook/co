-- +up
-- Remove zellij_session and zellij_pane columns from tasks table
-- These columns are no longer used

ALTER TABLE tasks DROP COLUMN zellij_session;
ALTER TABLE tasks DROP COLUMN zellij_pane;

-- +down
-- Re-add zellij_session and zellij_pane columns to tasks table
ALTER TABLE tasks ADD COLUMN zellij_session TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN zellij_pane TEXT NOT NULL DEFAULT '';
