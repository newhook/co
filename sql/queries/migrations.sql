-- name: CreateMigrationsTable :exec
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: GetAppliedMigrations :many
SELECT version FROM schema_migrations;

-- name: GetLastMigration :one
SELECT version FROM schema_migrations
ORDER BY version DESC
LIMIT 1;

-- name: RecordMigration :exec
INSERT INTO schema_migrations (version) VALUES (?);

-- name: DeleteMigration :exec
DELETE FROM schema_migrations WHERE version = ?;

-- name: ListMigrationVersions :many
SELECT version FROM schema_migrations ORDER BY version;
