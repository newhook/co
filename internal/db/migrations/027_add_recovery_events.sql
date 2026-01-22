-- +up
-- Add recovery_events table for audit trail of task/bead recovery operations
CREATE TABLE recovery_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,          -- 'task_reset', 'task_stale_failed', 'bead_preserved', 'bead_reset'
    task_id TEXT NOT NULL,
    work_id TEXT NOT NULL,
    bead_id TEXT,                       -- NULL for task-level events
    reason TEXT NOT NULL,
    details TEXT,                       -- JSON for additional context
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_recovery_events_task_id ON recovery_events(task_id);
CREATE INDEX idx_recovery_events_work_id ON recovery_events(work_id);
CREATE INDEX idx_recovery_events_created_at ON recovery_events(created_at);

-- +down
DROP INDEX IF EXISTS idx_recovery_events_created_at;
DROP INDEX IF EXISTS idx_recovery_events_work_id;
DROP INDEX IF EXISTS idx_recovery_events_task_id;
DROP TABLE IF EXISTS recovery_events;
