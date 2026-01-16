-- name: GetIssuesByIDs :many
SELECT * FROM issues
WHERE id IN (sqlc.slice('ids'))
  AND deleted_at IS NULL
  AND status != 'tombstone';

-- name: GetDependenciesForIssues :many
SELECT
    d.issue_id,
    d.depends_on_id,
    d.type,
    i.id,
    i.title,
    i.description,
    i.status,
    i.priority,
    i.issue_type,
    i.created_at,
    i.updated_at,
    i.closed_at
FROM dependencies d
INNER JOIN issues i ON d.depends_on_id = i.id
WHERE d.issue_id IN (sqlc.slice('issue_ids'))
  AND i.deleted_at IS NULL
  AND i.status != 'tombstone';

-- name: GetDependentsForIssues :many
SELECT
    d.issue_id,
    d.depends_on_id,
    d.type,
    i.id,
    i.title,
    i.description,
    i.status,
    i.priority,
    i.issue_type,
    i.created_at,
    i.updated_at,
    i.closed_at
FROM dependencies d
INNER JOIN issues i ON d.issue_id = i.id
WHERE d.depends_on_id IN (sqlc.slice('depends_on_ids'))
  AND i.deleted_at IS NULL
  AND i.status != 'tombstone';
