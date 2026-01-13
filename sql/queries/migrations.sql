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

-- name: RecordMigrationWithDown :exec
INSERT INTO schema_migrations (version, name, down_sql) VALUES (?, ?, ?);

-- name: UpdateMigrationDownSQL :exec
UPDATE schema_migrations SET name = ?, down_sql = ? WHERE version = ?;

-- name: GetMigrationDownSQL :one
SELECT name, down_sql FROM schema_migrations WHERE version = ?;

-- name: DeleteMigration :exec
DELETE FROM schema_migrations WHERE version = ?;

-- name: ListMigrationVersions :many
SELECT version FROM schema_migrations ORDER BY version;

-- name: ListMigrationsWithDetails :many
SELECT version, name, down_sql, applied_at FROM schema_migrations ORDER BY version;
