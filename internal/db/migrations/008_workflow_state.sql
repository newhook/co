-- +up
-- Workflow state table: tracks orchestration steps for automated workflows
-- Each row represents a work unit going through the automated workflow
-- Note: No FK to works because workflow_state is created before work exists
-- (work is created in StepCreateWork, the first step of the workflow)
CREATE TABLE workflow_state (
    work_id TEXT PRIMARY KEY,
    current_step INTEGER NOT NULL DEFAULT 0,
    step_status TEXT NOT NULL DEFAULT 'pending',
    step_data TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_workflow_state_status ON workflow_state(step_status);

-- +down
DROP INDEX IF EXISTS idx_workflow_state_status;
DROP TABLE IF EXISTS workflow_state;
