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

-- name: GetLatestReviewTaskWithMetadata :one
SELECT t.id FROM tasks t
JOIN task_metadata tm ON t.id = tm.task_id
WHERE t.work_id = ? AND t.task_type = 'review' AND tm.key = 'review_epic_id'
ORDER BY t.created_at DESC LIMIT 1;
