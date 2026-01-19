-- +up
-- Add fields for tracking GitHub comment resolution
ALTER TABLE pr_feedback ADD COLUMN source_id TEXT;
ALTER TABLE pr_feedback ADD COLUMN resolved_at DATETIME;

-- Add index for finding unresolved feedback with closed beads
CREATE INDEX idx_pr_feedback_resolution ON pr_feedback(bead_id, resolved_at);

-- +down
DROP INDEX IF EXISTS idx_pr_feedback_resolution;
ALTER TABLE pr_feedback DROP COLUMN resolved_at;
ALTER TABLE pr_feedback DROP COLUMN source_id;