-- name: SetTaskMetadata :exec
INSERT INTO task_metadata (task_id, key, value)
VALUES (?, ?, ?)
ON CONFLICT (task_id, key) DO UPDATE SET value = excluded.value;

-- name: GetTaskMetadata :one
SELECT value FROM task_metadata WHERE task_id = ? AND key = ?;

-- name: GetAllTaskMetadata :many
SELECT key, value FROM task_metadata WHERE task_id = ?;

-- name: DeleteTaskMetadata :execrows
DELETE FROM task_metadata WHERE task_id = ? AND key = ?;
