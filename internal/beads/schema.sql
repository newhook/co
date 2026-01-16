-- Schema for beads database (for sqlc type generation only)
-- This schema is derived from the actual beads.db SQLite database

CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    content_hash TEXT,
    title TEXT NOT NULL CHECK(length(title) <= 500),
    description TEXT NOT NULL DEFAULT '',
    design TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    priority INTEGER NOT NULL DEFAULT 2 CHECK(priority >= 0 AND priority <= 4),
    issue_type TEXT NOT NULL DEFAULT 'task',
    assignee TEXT,
    estimated_minutes INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT DEFAULT '',
    owner TEXT DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME,
    closed_by_session TEXT DEFAULT '',
    external_ref TEXT,
    compaction_level INTEGER DEFAULT 0,
    compacted_at DATETIME,
    compacted_at_commit TEXT,
    original_size INTEGER,
    deleted_at DATETIME,
    deleted_by TEXT DEFAULT '',
    delete_reason TEXT DEFAULT '',
    original_type TEXT DEFAULT '',
    sender TEXT DEFAULT '',
    ephemeral INTEGER DEFAULT 0,
    pinned INTEGER DEFAULT 0,
    is_template INTEGER DEFAULT 0,
    crystallizes INTEGER DEFAULT 0,
    mol_type TEXT DEFAULT '',
    work_type TEXT DEFAULT 'mutex',
    quality_score REAL,
    source_system TEXT DEFAULT '',
    event_kind TEXT DEFAULT '',
    actor TEXT DEFAULT '',
    target TEXT DEFAULT '',
    payload TEXT DEFAULT '',
    source_repo TEXT DEFAULT '.',
    close_reason TEXT DEFAULT '',
    await_type TEXT,
    await_id TEXT,
    timeout_ns INTEGER,
    waiters TEXT,
    hook_bead TEXT DEFAULT '',
    role_bead TEXT DEFAULT '',
    agent_state TEXT DEFAULT '',
    last_activity DATETIME,
    role_type TEXT DEFAULT '',
    rig TEXT DEFAULT '',
    due_at DATETIME,
    defer_until DATETIME,
    CHECK (
        (status = 'closed' AND closed_at IS NOT NULL) OR
        (status = 'tombstone') OR
        (status NOT IN ('closed', 'tombstone') AND closed_at IS NULL)
    )
);

CREATE INDEX idx_issues_status ON issues(status);
CREATE INDEX idx_issues_priority ON issues(priority);
CREATE INDEX idx_issues_assignee ON issues(assignee);
CREATE INDEX idx_issues_created_at ON issues(created_at);
CREATE INDEX idx_issues_external_ref ON issues(external_ref);
CREATE UNIQUE INDEX idx_issues_external_ref_unique ON issues(external_ref) WHERE external_ref IS NOT NULL;
CREATE INDEX idx_issues_source_repo ON issues(source_repo);
CREATE INDEX idx_issues_deleted_at ON issues(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_issues_ephemeral ON issues(ephemeral) WHERE ephemeral = 1;
CREATE INDEX idx_issues_sender ON issues(sender) WHERE sender != '';
CREATE INDEX idx_issues_pinned ON issues(pinned) WHERE pinned = 1;
CREATE INDEX idx_issues_is_template ON issues(is_template) WHERE is_template = 1;
CREATE INDEX idx_issues_updated_at ON issues(updated_at);
CREATE INDEX idx_issues_status_priority ON issues(status, priority);
CREATE INDEX idx_issues_gate ON issues(issue_type) WHERE issue_type = 'gate';
CREATE INDEX idx_issues_due_at ON issues(due_at);
CREATE INDEX idx_issues_defer_until ON issues(defer_until);

CREATE TABLE IF NOT EXISTS dependencies (
    issue_id TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'blocks',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    metadata TEXT,
    thread_id TEXT,
    PRIMARY KEY (issue_id, depends_on_id, type),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX idx_dependencies_issue_id ON dependencies(issue_id);
CREATE INDEX idx_dependencies_depends_on ON dependencies(depends_on_id);
CREATE INDEX idx_dependencies_type ON dependencies(type);
CREATE INDEX idx_dependencies_depends_on_type ON dependencies(depends_on_id, type);
CREATE INDEX idx_dependencies_depends_on_type_issue ON dependencies(depends_on_id, type, issue_id);
CREATE INDEX idx_dependencies_issue_type ON dependencies(issue_id, type);
