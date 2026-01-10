-- Schema for co project tracking database

-- Beads table: tracks individual beads
CREATE TABLE IF NOT EXISTS beads (
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

CREATE INDEX IF NOT EXISTS idx_beads_status ON beads(status);

-- Tasks table: tracks virtual tasks (groups of beads)
CREATE TABLE IF NOT EXISTS tasks (
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

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

-- Task-beads junction table: links tasks to their beads
CREATE TABLE IF NOT EXISTS task_beads (
    task_id TEXT NOT NULL,
    bead_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    PRIMARY KEY (task_id, bead_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_task_beads_task_id ON task_beads(task_id);
CREATE INDEX IF NOT EXISTS idx_task_beads_bead_id ON task_beads(bead_id);

-- Complexity cache: stores LLM complexity estimates
CREATE TABLE IF NOT EXISTS complexity_cache (
    bead_id TEXT PRIMARY KEY,
    description_hash TEXT NOT NULL,
    complexity_score INT NOT NULL,
    estimated_tokens INT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_complexity_cache_hash ON complexity_cache(description_hash);