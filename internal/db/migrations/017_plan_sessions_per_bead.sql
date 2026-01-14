-- +up
-- Migrate plan_sessions to be bead-centric instead of session-centric
-- Each planning session is now associated with a specific bead (issue)

CREATE TABLE plan_sessions_new (
    bead_id TEXT PRIMARY KEY,
    zellij_session TEXT NOT NULL,
    tab_name TEXT NOT NULL,
    pid INTEGER NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE plan_sessions;
ALTER TABLE plan_sessions_new RENAME TO plan_sessions;

CREATE INDEX idx_plan_sessions_zellij_session ON plan_sessions(zellij_session);

-- +down
DROP INDEX IF EXISTS idx_plan_sessions_zellij_session;

CREATE TABLE plan_sessions_old (
    session_name TEXT PRIMARY KEY,
    pid INTEGER NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE plan_sessions;
ALTER TABLE plan_sessions_old RENAME TO plan_sessions;
