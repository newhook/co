-- +up
-- Add spawn tracking columns to tasks table for async execution state
ALTER TABLE tasks ADD COLUMN spawned_at DATETIME;
ALTER TABLE tasks ADD COLUMN spawn_status TEXT NOT NULL DEFAULT '';

-- +down
-- SQLite doesn't support DROP COLUMN directly, need to recreate table
-- For rollback, create new table without the spawn tracking columns
CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    task_type TEXT NOT NULL DEFAULT 'implement',
    complexity_budget INT NOT NULL DEFAULT 0,
    actual_complexity INT NOT NULL DEFAULT 0,
    work_id TEXT NOT NULL DEFAULT '',
    zellij_session TEXT NOT NULL DEFAULT '',
    zellij_pane TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO tasks_new (id, status, task_type, complexity_budget, actual_complexity, work_id, zellij_session, zellij_pane, worktree_path, pr_url, error_message, started_at, completed_at, created_at)
SELECT id, status, task_type, complexity_budget, actual_complexity, work_id, zellij_session, zellij_pane, worktree_path, pr_url, error_message, started_at, completed_at, created_at
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_work_id ON tasks(work_id);
