-- name: CreatePRFeedback :exec
INSERT INTO pr_feedback (
    id, work_id, pr_url, feedback_type, title, description,
    source, source_url, source_id, priority, bead_id, metadata
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPRFeedback :one
SELECT * FROM pr_feedback WHERE id = ?;

-- name: GetPRFeedbackByBead :one
SELECT * FROM pr_feedback WHERE bead_id = ? LIMIT 1;

-- name: ListPRFeedback :many
SELECT * FROM pr_feedback WHERE work_id = ? ORDER BY created_at DESC;

-- name: ListUnprocessedPRFeedback :many
SELECT * FROM pr_feedback
WHERE work_id = ? AND processed_at IS NULL
ORDER BY priority ASC, created_at ASC;

-- name: GetUnresolvedFeedbackForWork :many
SELECT * FROM pr_feedback
WHERE work_id = ?
  AND bead_id IS NOT NULL
  AND resolved_at IS NULL
  AND source_id IS NOT NULL
ORDER BY created_at ASC;

-- name: GetUnresolvedFeedbackForBeads :many
SELECT * FROM pr_feedback
WHERE bead_id IN (sqlc.slice('bead_ids'))
  AND resolved_at IS NULL
  AND source_id IS NOT NULL
ORDER BY created_at ASC;

-- name: MarkPRFeedbackProcessed :exec
UPDATE pr_feedback
SET processed_at = CURRENT_TIMESTAMP, bead_id = ?
WHERE id = ?;

-- name: MarkPRFeedbackResolved :exec
UPDATE pr_feedback
SET resolved_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: HasExistingFeedback :one
SELECT COUNT(*) as count FROM pr_feedback
WHERE work_id = ? AND title = ? AND source = ?;

-- name: DeletePRFeedback :exec
DELETE FROM pr_feedback WHERE id = ?;

-- name: DeletePRFeedbackForWork :exec
DELETE FROM pr_feedback WHERE work_id = ?;