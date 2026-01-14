-- Schema for co project tracking database
-- Reference schema for sqlc code generation
-- The actual source of truth is in internal/db/migrations/

-- Works table: tracks work units (groups of tasks)
CREATE TABLE works (
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

CREATE INDEX idx_works_status ON works(status);

-- Beads table: tracks individual beads
CREATE TABLE beads (
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

CREATE INDEX idx_beads_status ON beads(status);

-- Tasks table: tracks virtual tasks (groups of beads)
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    task_type TEXT NOT NULL DEFAULT 'implement',
    complexity_budget INT NOT NULL DEFAULT 0,
    actual_complexity INT NOT NULL DEFAULT 0,
    work_id TEXT NOT NULL DEFAULT '' REFERENCES works(id),
    worktree_path TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    spawned_at DATETIME,
    spawn_status TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_work_id ON tasks(work_id);

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
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_complexity_cache_hash ON complexity_cache(description_hash);

-- Work-tasks junction table: links works to their tasks
CREATE TABLE work_tasks (
    work_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (work_id, task_id),
    FOREIGN KEY (work_id) REFERENCES works(id),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX idx_work_tasks_work_id ON work_tasks(work_id);
CREATE INDEX idx_work_tasks_task_id ON work_tasks(task_id);

-- Work task counters: tracks next task number for each work
CREATE TABLE work_task_counters (
    work_id TEXT PRIMARY KEY,
    next_task_num INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

-- Task metadata table: stores key-value metadata on tasks
CREATE TABLE task_metadata (
    task_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, key),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_task_metadata_task_id ON task_metadata(task_id);
CREATE INDEX idx_task_metadata_key ON task_metadata(key);

-- Task dependencies table: tracks dependencies between tasks
-- A task can depend on multiple other tasks, and those must complete before it can run
CREATE TABLE task_dependencies (
    task_id TEXT NOT NULL,
    depends_on_task_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_task_dependencies_task_id ON task_dependencies(task_id);
CREATE INDEX idx_task_dependencies_depends_on ON task_dependencies(depends_on_task_id);

-- Work beads table: beads assigned to work with optional grouping
CREATE TABLE work_beads (
    work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    bead_id TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,  -- 0 = ungrouped, >0 = grouped together
    position INTEGER NOT NULL DEFAULT 0,  -- ordering within work
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_id, bead_id)
);

CREATE INDEX idx_work_beads_work_id ON work_beads(work_id);
CREATE INDEX idx_work_beads_bead_id ON work_beads(bead_id);
CREATE INDEX idx_work_beads_group_id ON work_beads(work_id, group_id);

-- Unique group numbering per work
CREATE TABLE work_bead_group_counters (
    work_id TEXT PRIMARY KEY REFERENCES works(id) ON DELETE CASCADE,
    next_group_id INTEGER NOT NULL DEFAULT 1
);

-- Schema migrations table: tracks applied database migrations
CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    name TEXT NOT NULL DEFAULT '',
    down_sql TEXT NOT NULL DEFAULT ''
);