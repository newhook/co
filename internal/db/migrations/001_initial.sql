-- +up
-- Beads table: tracks individual beads
CREATE TABLE beads (
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

CREATE INDEX idx_beads_status ON beads(status);

-- Tasks table: tracks virtual tasks (groups of beads)
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    task_type TEXT NOT NULL DEFAULT 'implement',
    complexity_budget INT,
    actual_complexity INT,
    zellij_session TEXT,
    zellij_pane TEXT,
    worktree_path TEXT,
    pr_url TEXT,
    error_message TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tasks_status ON tasks(status);

-- Task-beads junction table: links tasks to their beads
CREATE TABLE task_beads (
    task_id TEXT NOT NULL,
    bead_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    PRIMARY KEY (task_id, bead_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX idx_task_beads_task_id ON task_beads(task_id);
CREATE INDEX idx_task_beads_bead_id ON task_beads(bead_id);

-- Complexity cache: stores LLM complexity estimates
CREATE TABLE complexity_cache (
    bead_id TEXT PRIMARY KEY,
    description_hash TEXT NOT NULL,
    complexity_score INT NOT NULL,
    estimated_tokens INT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_complexity_cache_hash ON complexity_cache(description_hash);

-- +down
DROP INDEX IF EXISTS idx_complexity_cache_hash;
DROP TABLE IF EXISTS complexity_cache;
DROP INDEX IF EXISTS idx_task_beads_bead_id;
DROP INDEX IF EXISTS idx_task_beads_task_id;
DROP TABLE IF EXISTS task_beads;
DROP INDEX IF EXISTS idx_tasks_status;
DROP TABLE IF EXISTS tasks;
DROP INDEX IF EXISTS idx_beads_status;
DROP TABLE IF EXISTS beads;