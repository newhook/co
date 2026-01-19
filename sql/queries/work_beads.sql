-- name: AddWorkBead :exec
INSERT INTO work_beads (work_id, bead_id, position)
VALUES (?, ?, ?);

-- name: AddWorkBeadsBatch :exec
INSERT INTO work_beads (work_id, bead_id, position)
VALUES (?, ?, ?)
ON CONFLICT (work_id, bead_id) DO UPDATE SET
    position = excluded.position;

-- name: RemoveWorkBead :execrows
DELETE FROM work_beads
WHERE work_id = ? AND bead_id = ?;

-- name: GetWorkBeads :many
SELECT work_id, bead_id, position, created_at
FROM work_beads
WHERE work_id = ?
ORDER BY position;

-- name: GetUnassignedWorkBeads :many
SELECT wb.work_id, wb.bead_id, wb.position, wb.created_at
FROM work_beads wb
WHERE wb.work_id = ?
  AND NOT EXISTS (
    SELECT 1 FROM task_beads tb
    JOIN work_tasks wt ON tb.task_id = wt.task_id
    WHERE wt.work_id = wb.work_id AND tb.bead_id = wb.bead_id
  )
ORDER BY wb.position;

-- name: IsBeadInTask :one
SELECT COUNT(*) > 0 as in_task
FROM task_beads tb
JOIN work_tasks wt ON tb.task_id = wt.task_id
WHERE wt.work_id = ? AND tb.bead_id = ?;

-- name: DeleteWorkBeads :execrows
DELETE FROM work_beads
WHERE work_id = ?;

-- name: GetMaxWorkBeadPosition :one
SELECT CAST(COALESCE(MAX(position), -1) AS INTEGER) as max_position
FROM work_beads
WHERE work_id = ?;

-- name: GetAllAssignedBeads :many
-- Returns all beads assigned to any work, with their work ID.
-- This is used by plan mode to show which beads are already assigned.
SELECT bead_id, work_id
FROM work_beads
ORDER BY bead_id;
