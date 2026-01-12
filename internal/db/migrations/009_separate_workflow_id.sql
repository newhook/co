-- +up
-- Separate workflow_id from work_id: workflow_id is the primary key,
-- work_id is set after StepCreateWork completes

-- SQLite doesn't support modifying primary keys, so we recreate the table
CREATE TABLE workflow_state_new (
    workflow_id TEXT PRIMARY KEY,
    work_id TEXT,
    current_step INTEGER NOT NULL DEFAULT 0,
    step_status TEXT NOT NULL DEFAULT 'pending',
    step_data TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Migrate existing data: use work_id as workflow_id for backwards compatibility
INSERT INTO workflow_state_new (workflow_id, work_id, current_step, step_status, step_data, error_message, created_at, updated_at)
SELECT work_id, work_id, current_step, step_status, step_data, error_message, created_at, updated_at
FROM workflow_state;

-- Drop old table and index
DROP INDEX IF EXISTS idx_workflow_state_status;
DROP TABLE workflow_state;

-- Rename new table
ALTER TABLE workflow_state_new RENAME TO workflow_state;

-- Recreate index
CREATE INDEX idx_workflow_state_status ON workflow_state(step_status);

-- +down
-- Revert to work_id as primary key
CREATE TABLE workflow_state_old (
    work_id TEXT PRIMARY KEY,
    current_step INTEGER NOT NULL DEFAULT 0,
    step_status TEXT NOT NULL DEFAULT 'pending',
    step_data TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Migrate data back (use workflow_id as work_id, or work_id if set)
INSERT INTO workflow_state_old (work_id, current_step, step_status, step_data, error_message, created_at, updated_at)
SELECT COALESCE(work_id, workflow_id), current_step, step_status, step_data, error_message, created_at, updated_at
FROM workflow_state;

DROP INDEX IF EXISTS idx_workflow_state_status;
DROP TABLE workflow_state;

ALTER TABLE workflow_state_old RENAME TO workflow_state;

CREATE INDEX idx_workflow_state_status ON workflow_state(step_status);
