-- +up
-- Add PR status tracking fields to works table
ALTER TABLE works ADD COLUMN ci_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE works ADD COLUMN approval_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE works ADD COLUMN approvers TEXT NOT NULL DEFAULT '[]';
ALTER TABLE works ADD COLUMN last_pr_poll_at DATETIME;
ALTER TABLE works ADD COLUMN has_unseen_pr_changes BOOLEAN NOT NULL DEFAULT FALSE;

-- +down
-- Remove PR status tracking fields from works table
ALTER TABLE works DROP COLUMN ci_status;
ALTER TABLE works DROP COLUMN approval_status;
ALTER TABLE works DROP COLUMN approvers;
ALTER TABLE works DROP COLUMN last_pr_poll_at;
ALTER TABLE works DROP COLUMN has_unseen_pr_changes;
