-- name: CacheComplexity :exec
REPLACE INTO complexity_cache (bead_id, description_hash, complexity_score, estimated_tokens)
VALUES (?, ?, ?, ?);

-- name: GetCachedComplexity :one
SELECT complexity_score, estimated_tokens
FROM complexity_cache
WHERE bead_id = ? AND description_hash = ?;

-- name: GetAllCachedComplexity :many
SELECT bead_id, complexity_score, estimated_tokens
FROM complexity_cache;

-- name: CountEstimatedBeads :one
SELECT COUNT(DISTINCT bead_id) as count
FROM complexity_cache
WHERE bead_id IN (sqlc.slice('bead_ids'));