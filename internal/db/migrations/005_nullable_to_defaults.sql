-- +up
-- SQLite doesn't support ALTER COLUMN, so we need to recreate tables with new constraints.
-- This migration converts nullable string/int columns to NOT NULL with defaults.

-- Recreate beads table with NOT NULL constraints
CREATE TABLE beads_new (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    title TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    zellij_session TEXT NOT NULL DEFAULT '',
    zellij_pane TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO beads_new (id, status, title, pr_url, error_message, zellij_session, zellij_pane, worktree_path, started_at, completed_at, created_at, updated_at)
SELECT id, status,
       COALESCE(title, ''),
       COALESCE(pr_url, ''),
       COALESCE(error_message, ''),
       COALESCE(zellij_session, ''),
       COALESCE(zellij_pane, ''),
       COALESCE(worktree_path, ''),
       started_at, completed_at,
       COALESCE(created_at, CURRENT_TIMESTAMP),
       COALESCE(updated_at, CURRENT_TIMESTAMP)
FROM beads;

DROP TABLE beads;
ALTER TABLE beads_new RENAME TO beads;
CREATE INDEX idx_beads_status ON beads(status);

-- Recreate works table with NOT NULL constraints
CREATE TABLE works_new (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    zellij_session TEXT NOT NULL DEFAULT '',
    zellij_tab TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    base_branch TEXT NOT NULL DEFAULT 'main',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO works_new (id, status, zellij_session, zellij_tab, worktree_path, branch_name, base_branch, pr_url, error_message, started_at, completed_at, created_at)
SELECT id, status,
       COALESCE(zellij_session, ''),
       COALESCE(zellij_tab, ''),
       COALESCE(worktree_path, ''),
       COALESCE(branch_name, ''),
       COALESCE(base_branch, 'main'),
       COALESCE(pr_url, ''),
       COALESCE(error_message, ''),
       started_at, completed_at,
       COALESCE(created_at, CURRENT_TIMESTAMP)
FROM works;

DROP TABLE works;
ALTER TABLE works_new RENAME TO works;
CREATE INDEX idx_works_status ON works(status);

-- Recreate tasks table with NOT NULL constraints
-- First save foreign key references
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
SELECT id, status,
       COALESCE(task_type, 'implement'),
       COALESCE(complexity_budget, 0),
       COALESCE(actual_complexity, 0),
       COALESCE(work_id, ''),
       COALESCE(zellij_session, ''),
       COALESCE(zellij_pane, ''),
       COALESCE(worktree_path, ''),
       COALESCE(pr_url, ''),
       COALESCE(error_message, ''),
       started_at, completed_at,
       COALESCE(created_at, CURRENT_TIMESTAMP)
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_work_id ON tasks(work_id);

-- Recreate work_tasks table with NOT NULL constraint on position
CREATE TABLE work_tasks_new (
    work_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (work_id, task_id)
);

INSERT INTO work_tasks_new (work_id, task_id, position)
SELECT work_id, task_id, COALESCE(position, 0)
FROM work_tasks;

DROP TABLE work_tasks;
ALTER TABLE work_tasks_new RENAME TO work_tasks;
CREATE INDEX idx_work_tasks_work_id ON work_tasks(work_id);
CREATE INDEX idx_work_tasks_task_id ON work_tasks(task_id);

-- +down
-- Recreate tables with nullable columns (original schema)

-- Recreate beads with nullable columns
CREATE TABLE beads_old (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    title TEXT,
    pr_url TEXT,
    error_message TEXT,
    zellij_session TEXT,
    zellij_pane TEXT,
    worktree_path TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO beads_old SELECT * FROM beads;
DROP TABLE beads;
ALTER TABLE beads_old RENAME TO beads;
CREATE INDEX idx_beads_status ON beads(status);

-- Recreate works with nullable columns
CREATE TABLE works_old (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    zellij_session TEXT,
    zellij_tab TEXT,
    worktree_path TEXT,
    branch_name TEXT,
    base_branch TEXT DEFAULT 'main',
    pr_url TEXT,
    error_message TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO works_old SELECT * FROM works;
DROP TABLE works;
ALTER TABLE works_old RENAME TO works;
CREATE INDEX idx_works_status ON works(status);

-- Recreate tasks with nullable columns
CREATE TABLE tasks_old (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    task_type TEXT NOT NULL DEFAULT 'implement',
    complexity_budget INT,
    actual_complexity INT,
    work_id TEXT,
    zellij_session TEXT,
    zellij_pane TEXT,
    worktree_path TEXT,
    pr_url TEXT,
    error_message TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO tasks_old SELECT * FROM tasks;
DROP TABLE tasks;
ALTER TABLE tasks_old RENAME TO tasks;
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_work_id ON tasks(work_id);

-- Recreate work_tasks with nullable position
CREATE TABLE work_tasks_old (
    work_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    position INTEGER DEFAULT 0,
    PRIMARY KEY (work_id, task_id)
);

INSERT INTO work_tasks_old SELECT * FROM work_tasks;
DROP TABLE work_tasks;
ALTER TABLE work_tasks_old RENAME TO work_tasks;
CREATE INDEX idx_work_tasks_work_id ON work_tasks(work_id);
CREATE INDEX idx_work_tasks_task_id ON work_tasks(task_id);
