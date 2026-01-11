-- +up
-- Works table: tracks work units (groups of tasks)
CREATE TABLE works (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    zellij_session TEXT,
    zellij_tab TEXT,
    worktree_path TEXT,
    branch_name TEXT,
    pr_url TEXT,
    error_message TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_works_status ON works(status);

-- Add work_id to tasks table
ALTER TABLE tasks ADD COLUMN work_id TEXT REFERENCES works(id);
CREATE INDEX idx_tasks_work_id ON tasks(work_id);

-- Junction table for work-task relationship (for flexibility)
CREATE TABLE work_tasks (
    work_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    position INTEGER DEFAULT 0,
    PRIMARY KEY (work_id, task_id),
    FOREIGN KEY (work_id) REFERENCES works(id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX idx_work_tasks_work_id ON work_tasks(work_id);
CREATE INDEX idx_work_tasks_task_id ON work_tasks(task_id);

-- +down
DROP INDEX IF EXISTS idx_work_tasks_task_id;
DROP INDEX IF EXISTS idx_work_tasks_work_id;
DROP TABLE IF EXISTS work_tasks;
DROP INDEX IF EXISTS idx_tasks_work_id;
ALTER TABLE tasks DROP COLUMN work_id;
DROP INDEX IF EXISTS idx_works_status;
DROP TABLE IF EXISTS works;