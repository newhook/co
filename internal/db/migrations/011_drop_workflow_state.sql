-- +up
-- Drop the workflow_state table - workflow state is no longer used.
-- The new model uses task dependencies and a polling orchestrator instead.
DROP TABLE IF EXISTS workflow_state;

-- +down
-- Recreate workflow_state table if needed
CREATE TABLE workflow_state (
    workflow_id TEXT PRIMARY KEY NOT NULL,
    work_id TEXT,
    current_step INTEGER NOT NULL DEFAULT 0,
    step_status TEXT NOT NULL DEFAULT 'pending',
    step_data TEXT NOT NULL DEFAULT '{}',
    error_message TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_workflow_state_status ON workflow_state(step_status);
CREATE INDEX IF NOT EXISTS idx_workflow_state_work ON workflow_state(work_id);
