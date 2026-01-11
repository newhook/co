-- +up
-- Add a table to track the next task number for each work
-- This ensures atomic task ID generation without race conditions
CREATE TABLE work_task_counters (
    work_id TEXT PRIMARY KEY,
    next_task_num INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (work_id) REFERENCES works(id) ON DELETE CASCADE
);

-- Initialize counters for existing works based on their current tasks
INSERT INTO work_task_counters (work_id, next_task_num)
SELECT
    w.id,
    COALESCE(
        MAX(CAST(SUBSTR(t.id, LENGTH(w.id) + 2) AS INTEGER)) + 1,
        1
    ) as next_num
FROM works w
LEFT JOIN tasks t ON t.id LIKE w.id || '.%'
GROUP BY w.id;

-- +down
-- Remove the work_task_counters table
DROP TABLE IF EXISTS work_task_counters;