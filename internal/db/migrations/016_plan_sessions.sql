-- +up
-- Plan sessions table: tracks running plan mode Claude sessions
CREATE TABLE plan_sessions (
    session_name TEXT PRIMARY KEY,
    pid INTEGER NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +down
DROP TABLE IF EXISTS plan_sessions;
