-- +up
-- Add human-readable name column to works table for worker identification (e.g., "Alice", "Bob")
ALTER TABLE works ADD COLUMN name TEXT NOT NULL DEFAULT '';

-- +down
ALTER TABLE works DROP COLUMN name;
