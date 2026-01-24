-- +up
-- Add pr_state field to track GitHub PR state (open, closed, merged)
ALTER TABLE works ADD COLUMN pr_state TEXT NOT NULL DEFAULT '';

-- +down
ALTER TABLE works DROP COLUMN pr_state;
