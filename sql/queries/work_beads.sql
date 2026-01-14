-- name: AddWorkBead :exec
INSERT INTO work_beads (work_id, bead_id, group_id, position)
VALUES (?, ?, ?, ?);

-- name: AddWorkBeadsBatch :exec
INSERT INTO work_beads (work_id, bead_id, group_id, position)
VALUES (?, ?, ?, ?)
ON CONFLICT (work_id, bead_id) DO UPDATE SET
    group_id = excluded.group_id,
    position = excluded.position;

-- name: RemoveWorkBead :execrows
DELETE FROM work_beads
WHERE work_id = ? AND bead_id = ?;

-- name: GetWorkBeads :many
SELECT work_id, bead_id, group_id, position, created_at
FROM work_beads
WHERE work_id = ?
ORDER BY position;

-- name: GetWorkBeadsByGroup :many
SELECT work_id, bead_id, group_id, position, created_at
FROM work_beads
WHERE work_id = ? AND group_id = ?
ORDER BY position;

-- name: GetWorkBeadGroups :many
SELECT DISTINCT group_id
FROM work_beads
WHERE work_id = ? AND group_id > 0
ORDER BY group_id;

-- name: GetUnassignedWorkBeads :many
SELECT wb.work_id, wb.bead_id, wb.group_id, wb.position, wb.created_at
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

-- name: UpdateWorkBeadGroup :execrows
UPDATE work_beads
SET group_id = ?
WHERE work_id = ? AND bead_id = ?;

-- name: DeleteWorkBeads :execrows
DELETE FROM work_beads
WHERE work_id = ?;

-- name: InitializeBeadGroupCounter :exec
INSERT INTO work_bead_group_counters (work_id, next_group_id)
VALUES (?, 1)
ON CONFLICT (work_id) DO NOTHING;

-- name: GetAndIncrementBeadGroupCounter :one
UPDATE work_bead_group_counters
SET next_group_id = next_group_id + 1
WHERE work_id = ?
RETURNING next_group_id - 1 as group_id;

-- name: GetMaxWorkBeadPosition :one
SELECT CAST(COALESCE(MAX(position), -1) AS INTEGER) as max_position
FROM work_beads
WHERE work_id = ?;
