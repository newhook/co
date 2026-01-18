-- +up
-- Create table to track PR feedback and associated beads
CREATE TABLE pr_feedback (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    pr_url TEXT NOT NULL,
    feedback_type TEXT NOT NULL, -- ci_failure, test_failure, lint_error, build_error, review_comment, security_issue, general
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    source TEXT NOT NULL, -- e.g., "CI: test-suite", "Review: johndoe"
    source_url TEXT,
    priority INTEGER NOT NULL DEFAULT 2, -- 0-4 (0=critical, 4=backlog)
    bead_id TEXT, -- ID of the bead created from this feedback (null if not created yet)
    metadata TEXT NOT NULL DEFAULT '{}', -- JSON metadata
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at DATETIME, -- When the feedback was processed to create a bead
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

CREATE INDEX idx_pr_feedback_work_id ON pr_feedback(work_id);
CREATE INDEX idx_pr_feedback_bead_id ON pr_feedback(bead_id);
CREATE INDEX idx_pr_feedback_processed ON pr_feedback(processed_at);

-- +down
DROP INDEX IF EXISTS idx_pr_feedback_processed;
DROP INDEX IF EXISTS idx_pr_feedback_bead_id;
DROP INDEX IF EXISTS idx_pr_feedback_work_id;
DROP TABLE IF EXISTS pr_feedback;