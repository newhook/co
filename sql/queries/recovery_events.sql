-- name: InsertRecoveryEvent :exec
INSERT INTO recovery_events (event_type, task_id, work_id, bead_id, reason, details)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecoveryEventsForTask :many
SELECT id, event_type, task_id, work_id, bead_id, reason, details, created_at
FROM recovery_events
WHERE task_id = ?
ORDER BY created_at DESC;

-- name: GetRecoveryEventsForWork :many
SELECT id, event_type, task_id, work_id, bead_id, reason, details, created_at
FROM recovery_events
WHERE work_id = ?
ORDER BY created_at DESC;

-- name: GetRecentRecoveryEvents :many
SELECT id, event_type, task_id, work_id, bead_id, reason, details, created_at
FROM recovery_events
ORDER BY created_at DESC
LIMIT ?;
