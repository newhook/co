-- name: CreateWork :exec
INSERT INTO works (id, status, worktree_path, branch_name, base_branch)
VALUES (?, 'pending', ?, ?, ?);

-- name: StartWork :execrows
UPDATE works
SET status = 'processing',
    zellij_session = ?,
    zellij_tab = ?,
    started_at = ?
WHERE id = ?;

-- name: CompleteWork :execrows
UPDATE works
SET status = 'completed',
    pr_url = ?,
    completed_at = ?
WHERE id = ?;

-- name: FailWork :execrows
UPDATE works
SET status = 'failed',
    error_message = ?,
    completed_at = ?
WHERE id = ?;

-- name: GetWork :one
SELECT id, status,
       zellij_session,
       zellij_tab,
       worktree_path,
       branch_name,
       base_branch,
       pr_url,
       error_message,
       started_at,
       completed_at,
       created_at
FROM works
WHERE id = ?;

-- name: ListWorks :many
SELECT id, status,
       zellij_session,
       zellij_tab,
       worktree_path,
       branch_name,
       base_branch,
       pr_url,
       error_message,
       started_at,
       completed_at,
       created_at
FROM works
ORDER BY created_at DESC;

-- name: ListWorksByStatus :many
SELECT id, status,
       zellij_session,
       zellij_tab,
       worktree_path,
       branch_name,
       base_branch,
       pr_url,
       error_message,
       started_at,
       completed_at,
       created_at
FROM works
WHERE status = ?
ORDER BY created_at DESC;

-- name: GetLastWorkID :one
SELECT id FROM works
ORDER BY created_at DESC
LIMIT 1;

-- name: GetWorkByDirectory :one
SELECT id, status,
       zellij_session,
       zellij_tab,
       worktree_path,
       branch_name,
       base_branch,
       pr_url,
       error_message,
       started_at,
       completed_at,
       created_at
FROM works
WHERE worktree_path LIKE ?
LIMIT 1;

-- name: AddTaskToWork :exec
INSERT INTO work_tasks (work_id, task_id, position)
VALUES (?, ?, ?);

-- name: GetWorkTasks :many
SELECT t.id, t.status,
       COALESCE(t.task_type, 'implement') as task_type,
       t.complexity_budget,
       t.actual_complexity,
       t.work_id,
       t.worktree_path,
       t.pr_url,
       t.error_message,
       t.started_at,
       t.completed_at,
       t.created_at,
       t.spawned_at,
       t.spawn_status
FROM tasks t
JOIN work_tasks wt ON t.id = wt.task_id
WHERE wt.work_id = ?
ORDER BY wt.position;

-- name: DeleteWorkTasks :execrows
DELETE FROM work_tasks
WHERE work_id = ?;

-- name: DeleteWork :execrows
DELETE FROM works
WHERE id = ?;

-- name: InitializeTaskCounter :exec
INSERT INTO work_task_counters (work_id, next_task_num)
VALUES (?, 1)
ON CONFLICT (work_id) DO NOTHING;

-- name: GetAndIncrementTaskCounter :one
UPDATE work_task_counters
SET next_task_num = next_task_num + 1
WHERE work_id = ?
RETURNING next_task_num - 1 as task_num;