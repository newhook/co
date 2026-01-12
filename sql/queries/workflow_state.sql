-- name: CreateWorkflowState :exec
INSERT INTO workflow_state (work_id, current_step, step_status, step_data)
VALUES (?, ?, ?, ?);

-- name: GetWorkflowState :one
SELECT work_id, current_step, step_status, step_data, error_message, created_at, updated_at
FROM workflow_state
WHERE work_id = ?;

-- name: UpdateWorkflowStep :execrows
UPDATE workflow_state
SET current_step = ?,
    step_status = ?,
    step_data = ?,
    error_message = '',
    updated_at = CURRENT_TIMESTAMP
WHERE work_id = ?;

-- name: FailWorkflowStep :execrows
UPDATE workflow_state
SET step_status = 'failed',
    error_message = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE work_id = ?;

-- name: CompleteWorkflowStep :execrows
UPDATE workflow_state
SET step_status = 'completed',
    updated_at = CURRENT_TIMESTAMP
WHERE work_id = ?;

-- name: DeleteWorkflowState :execrows
DELETE FROM workflow_state
WHERE work_id = ?;

-- name: ListPendingWorkflows :many
SELECT work_id, current_step, step_status, step_data, error_message, created_at, updated_at
FROM workflow_state
WHERE step_status = 'pending' OR step_status = 'processing'
ORDER BY created_at;
