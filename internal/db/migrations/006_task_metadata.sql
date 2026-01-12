-- +up
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

-- +down
DROP INDEX IF EXISTS idx_task_metadata_key;
DROP INDEX IF EXISTS idx_task_metadata_task_id;
DROP TABLE IF EXISTS task_metadata;
