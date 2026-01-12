-- +up
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

-- +down
DROP INDEX IF EXISTS idx_task_dependencies_depends_on;
DROP INDEX IF EXISTS idx_task_dependencies_task_id;
DROP TABLE IF EXISTS task_dependencies;
