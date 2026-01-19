-- name: CreateScheduledTask :exec
INSERT INTO scheduler (
    id, work_id, task_type, scheduled_at, status, metadata
) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetNextScheduledTask :one
SELECT * FROM scheduler
WHERE status = 'pending'
  AND scheduled_at <= CURRENT_TIMESTAMP
ORDER BY scheduled_at ASC
LIMIT 1;

-- name: GetScheduledTasksForWork :many
SELECT * FROM scheduler
WHERE work_id = ?
  AND status = 'pending'
ORDER BY scheduled_at ASC;

-- name: GetPendingTaskByType :one
SELECT * FROM scheduler
WHERE work_id = ?
  AND task_type = ?
  AND status = 'pending'
ORDER BY scheduled_at ASC
LIMIT 1;

-- name: UpdateScheduledTaskTime :exec
UPDATE scheduler
SET scheduled_at = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: MarkTaskExecuting :exec
UPDATE scheduler
SET status = 'executing',
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: MarkTaskCompleted :exec
UPDATE scheduler
SET status = 'completed',
    executed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: MarkTaskFailed :exec
UPDATE scheduler
SET status = 'failed',
    error_message = ?,
    executed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteScheduledTask :exec
DELETE FROM scheduler WHERE id = ?;

-- name: DeleteCompletedTasksOlderThan :exec
DELETE FROM scheduler
WHERE status IN ('completed', 'failed')
  AND executed_at < ?;

-- name: GetScheduledTaskByID :one
SELECT * FROM scheduler WHERE id = ?;

-- name: RescheduleTask :exec
UPDATE scheduler
SET scheduled_at = ?,
    status = 'pending',
    error_message = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetOverdueTasks :many
SELECT * FROM scheduler
WHERE status = 'pending'
  AND scheduled_at < datetime('now', '-10 minutes')
ORDER BY scheduled_at ASC;

-- name: CountPendingTasksForWork :one
SELECT COUNT(*) as count FROM scheduler
WHERE work_id = ?
  AND status = 'pending';

-- name: WatchSchedulerChanges :many
SELECT * FROM scheduler
WHERE updated_at > ?
ORDER BY updated_at ASC;