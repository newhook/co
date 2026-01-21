-- +up
-- Add auto field to works table to track automated workflow flag
ALTER TABLE works ADD COLUMN auto BOOLEAN DEFAULT FALSE;

-- +down
-- Remove auto field from works table
ALTER TABLE works DROP COLUMN auto;