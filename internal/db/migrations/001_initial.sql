-- +up
-- Schema for co project tracking database

-- Works table: tracks work units (groups of tasks)
CREATE TABLE works (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    name TEXT NOT NULL DEFAULT '',
    zellij_session TEXT NOT NULL DEFAULT '',
    zellij_tab TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    base_branch TEXT NOT NULL DEFAULT 'main',
    root_issue_id TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    auto BOOLEAN NOT NULL DEFAULT FALSE,
    ci_status TEXT NOT NULL DEFAULT 'pending',
    approval_status TEXT NOT NULL DEFAULT 'pending',
    approvers TEXT NOT NULL DEFAULT '[]',
    last_pr_poll_at DATETIME,
    has_unseen_pr_changes BOOLEAN NOT NULL DEFAULT FALSE,
    pr_state TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_works_status ON works(status);
CREATE INDEX idx_works_root_issue_id ON works(root_issue_id);

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
    spawn_status TEXT NOT NULL DEFAULT '',
    last_activity DATETIME
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

-- Work beads table: beads assigned to work
CREATE TABLE work_beads (
    work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    bead_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_id, bead_id)
);

CREATE INDEX idx_work_beads_work_id ON work_beads(work_id);
CREATE INDEX idx_work_beads_bead_id ON work_beads(bead_id);

-- Plan sessions table: tracks running plan mode Claude sessions per bead
CREATE TABLE plan_sessions (
    bead_id TEXT PRIMARY KEY,
    zellij_session TEXT NOT NULL,
    tab_name TEXT NOT NULL,
    pid INTEGER NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_plan_sessions_zellij_session ON plan_sessions(zellij_session);

-- PR Feedback table: tracks feedback from PRs (comments, CI failures, etc.)
CREATE TABLE pr_feedback (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    pr_url TEXT NOT NULL,
    feedback_type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    source_url TEXT,
    source_id TEXT,
    source_type TEXT,
    source_name TEXT,
    context TEXT,
    priority INTEGER NOT NULL DEFAULT 2,
    bead_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at DATETIME,
    resolved_at DATETIME,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

CREATE INDEX idx_pr_feedback_work_id ON pr_feedback(work_id);
CREATE INDEX idx_pr_feedback_bead_id ON pr_feedback(bead_id);
CREATE INDEX idx_pr_feedback_processed ON pr_feedback(processed_at);
CREATE INDEX idx_pr_feedback_resolution ON pr_feedback(bead_id, resolved_at);
CREATE INDEX idx_pr_feedback_source_type ON pr_feedback(source_type);

-- Scheduler table: manages scheduled tasks like PR feedback checks
CREATE TABLE scheduler (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    task_type TEXT NOT NULL,
    scheduled_at DATETIME NOT NULL,
    executed_at DATETIME,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    metadata TEXT NOT NULL DEFAULT '{}',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    idempotency_key TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

CREATE INDEX idx_scheduler_work_id ON scheduler(work_id);
CREATE INDEX idx_scheduler_status ON scheduler(status);
CREATE INDEX idx_scheduler_scheduled_at ON scheduler(scheduled_at);
CREATE INDEX idx_scheduler_task_type ON scheduler(work_id, task_type);
CREATE UNIQUE INDEX idx_scheduler_idempotency_key ON scheduler(idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +down
-- Drop tables in reverse dependency order

-- Scheduler depends on works
DROP INDEX IF EXISTS idx_scheduler_idempotency_key;
DROP INDEX IF EXISTS idx_scheduler_task_type;
DROP INDEX IF EXISTS idx_scheduler_scheduled_at;
DROP INDEX IF EXISTS idx_scheduler_status;
DROP INDEX IF EXISTS idx_scheduler_work_id;
DROP TABLE IF EXISTS scheduler;

-- PR feedback depends on works
DROP INDEX IF EXISTS idx_pr_feedback_source_type;
DROP INDEX IF EXISTS idx_pr_feedback_resolution;
DROP INDEX IF EXISTS idx_pr_feedback_processed;
DROP INDEX IF EXISTS idx_pr_feedback_bead_id;
DROP INDEX IF EXISTS idx_pr_feedback_work_id;
DROP TABLE IF EXISTS pr_feedback;

-- Plan sessions (no FK dependencies)
DROP INDEX IF EXISTS idx_plan_sessions_zellij_session;
DROP TABLE IF EXISTS plan_sessions;

-- Work beads depends on works
DROP INDEX IF EXISTS idx_work_beads_bead_id;
DROP INDEX IF EXISTS idx_work_beads_work_id;
DROP TABLE IF EXISTS work_beads;

-- Task dependencies depends on tasks
DROP INDEX IF EXISTS idx_task_dependencies_depends_on;
DROP INDEX IF EXISTS idx_task_dependencies_task_id;
DROP TABLE IF EXISTS task_dependencies;

-- Task metadata depends on tasks
DROP INDEX IF EXISTS idx_task_metadata_key;
DROP INDEX IF EXISTS idx_task_metadata_task_id;
DROP TABLE IF EXISTS task_metadata;

-- Work task counters depends on works
DROP TABLE IF EXISTS work_task_counters;

-- Work tasks depends on works and tasks
DROP INDEX IF EXISTS idx_work_tasks_task_id;
DROP INDEX IF EXISTS idx_work_tasks_work_id;
DROP TABLE IF EXISTS work_tasks;

-- Complexity cache (no FK dependencies)
DROP INDEX IF EXISTS idx_complexity_cache_hash;
DROP TABLE IF EXISTS complexity_cache;

-- Task beads depends on tasks
DROP INDEX IF EXISTS idx_task_beads_bead_id;
DROP INDEX IF EXISTS idx_task_beads_task_id;
DROP TABLE IF EXISTS task_beads;

-- Tasks depends on works
DROP INDEX IF EXISTS idx_tasks_work_id;
DROP INDEX IF EXISTS idx_tasks_status;
DROP TABLE IF EXISTS tasks;

-- Beads (no FK dependencies)
DROP INDEX IF EXISTS idx_beads_status;
DROP TABLE IF EXISTS beads;

-- Works (base table)
DROP INDEX IF EXISTS idx_works_root_issue_id;
DROP INDEX IF EXISTS idx_works_status;
DROP TABLE IF EXISTS works;
