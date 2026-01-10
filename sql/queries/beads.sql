-- name: StartBead :exec
INSERT INTO beads (id, status, title, zellij_session, zellij_pane, worktree_path, started_at, updated_at)
VALUES (?, 'processing', ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    status = 'processing',
    title = excluded.title,
    zellij_session = excluded.zellij_session,
    zellij_pane = excluded.zellij_pane,
    worktree_path = excluded.worktree_path,
    started_at = excluded.started_at,
    updated_at = excluded.updated_at;

-- name: CompleteBead :execrows
UPDATE beads
SET status = 'completed',
    pr_url = ?,
    completed_at = ?,
    updated_at = ?
WHERE id = ?;

-- name: FailBead :execrows
UPDATE beads
SET status = 'failed',
    error_message = ?,
    completed_at = ?,
    updated_at = ?
WHERE id = ?;

-- name: GetBead :one
SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
       worktree_path, started_at, completed_at, created_at, updated_at
FROM beads
WHERE id = ?;

-- name: GetBeadStatus :one
SELECT status
FROM beads
WHERE id = ?;

-- name: ListBeads :many
SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
       worktree_path, started_at, completed_at, created_at, updated_at
FROM beads
ORDER BY created_at DESC;

-- name: ListBeadsByStatus :many
SELECT id, status, title, pr_url, error_message, zellij_session, zellij_pane,
       worktree_path, started_at, completed_at, created_at, updated_at
FROM beads
WHERE status = ?
ORDER BY created_at DESC;