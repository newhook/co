-- +up
-- Add name and down_sql columns to schema_migrations table
-- This allows rollback without needing the original migration files
ALTER TABLE schema_migrations ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE schema_migrations ADD COLUMN down_sql TEXT NOT NULL DEFAULT '';

-- +down
-- SQLite doesn't support DROP COLUMN easily, recreate table
CREATE TABLE schema_migrations_new (
    version TEXT PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schema_migrations_new (version, applied_at)
SELECT version, applied_at FROM schema_migrations;
DROP TABLE schema_migrations;
ALTER TABLE schema_migrations_new RENAME TO schema_migrations;
