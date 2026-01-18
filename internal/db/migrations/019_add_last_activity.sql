-- +up
-- Add last_activity column to track orchestrator health
ALTER TABLE tasks ADD COLUMN last_activity DATETIME;

-- Initialize existing processing tasks with current timestamp
UPDATE tasks SET last_activity = CURRENT_TIMESTAMP WHERE status = 'processing';

-- +down
-- Remove the last_activity column
ALTER TABLE tasks DROP COLUMN last_activity;
