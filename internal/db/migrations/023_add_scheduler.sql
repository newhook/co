-- +up
-- Scheduler table for managing scheduled tasks
CREATE TABLE scheduler (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    task_type TEXT NOT NULL, -- 'pr_feedback', 'comment_resolution', etc.
    scheduled_at DATETIME NOT NULL,
    executed_at DATETIME,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'executing', 'completed', 'failed'
    error_message TEXT,
    metadata TEXT NOT NULL DEFAULT '{}', -- JSON metadata
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

CREATE INDEX idx_scheduler_work_id ON scheduler(work_id);
CREATE INDEX idx_scheduler_status ON scheduler(status);
CREATE INDEX idx_scheduler_scheduled_at ON scheduler(scheduled_at);
CREATE INDEX idx_scheduler_task_type ON scheduler(work_id, task_type);

-- +down
DROP INDEX IF EXISTS idx_scheduler_task_type;
DROP INDEX IF EXISTS idx_scheduler_scheduled_at;
DROP INDEX IF EXISTS idx_scheduler_status;
DROP INDEX IF EXISTS idx_scheduler_work_id;
DROP TABLE IF EXISTS scheduler;