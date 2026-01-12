-- name: AddTaskDependency :exec
INSERT INTO task_dependencies (task_id, depends_on_task_id)
VALUES (?, ?);

-- name: GetTaskDependencies :many
SELECT depends_on_task_id
FROM task_dependencies
WHERE task_id = ?;

-- name: GetTaskDependents :many
SELECT task_id
FROM task_dependencies
WHERE depends_on_task_id = ?;

-- name: DeleteTaskDependencies :execrows
DELETE FROM task_dependencies
WHERE task_id = ?;

-- name: DeleteTaskDependenciesForWork :execrows
DELETE FROM task_dependencies
WHERE task_id IN (
    SELECT task_id FROM work_tasks WHERE work_id = ?
);

-- name: GetReadyTasksForWork :many
SELECT t.id, t.status,
       COALESCE(t.task_type, 'implement') as task_type,
       t.complexity_budget,
       t.actual_complexity,
       t.work_id,
       t.zellij_session,
       t.zellij_pane,
       t.worktree_path,
       t.pr_url,
       t.error_message,
       t.started_at,
       t.completed_at,
       t.created_at,
       t.spawned_at,
       t.spawn_status
FROM tasks t
INNER JOIN work_tasks wt ON t.id = wt.task_id
WHERE wt.work_id = ?
  AND t.status = 'pending'
  AND NOT EXISTS (
      SELECT 1 FROM task_dependencies td
      INNER JOIN tasks dep ON td.depends_on_task_id = dep.id
      WHERE td.task_id = t.id
        AND dep.status != 'completed'
  )
ORDER BY wt.position ASC;

-- name: HasPendingDependencies :one
SELECT COUNT(*) > 0 as has_pending
FROM task_dependencies td
INNER JOIN tasks dep ON td.depends_on_task_id = dep.id
WHERE td.task_id = ?
  AND dep.status != 'completed';

-- name: DeleteTaskDependency :execrows
DELETE FROM task_dependencies
WHERE task_id = ? AND depends_on_task_id = ?;
