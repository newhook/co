-- +up
-- Add retry support columns to scheduler table
ALTER TABLE scheduler ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduler ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 5;
ALTER TABLE scheduler ADD COLUMN idempotency_key TEXT;

-- Create unique index on idempotency_key (only for non-null values)
CREATE UNIQUE INDEX idx_scheduler_idempotency_key ON scheduler(idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +down
DROP INDEX IF EXISTS idx_scheduler_idempotency_key;
-- Note: SQLite doesn't support DROP COLUMN, so we can't fully reverse this migration
-- In practice, the columns would remain but be unused
