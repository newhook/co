-- +up
-- Add test column for migration tests
ALTER TABLE test_table1 ADD COLUMN test_field TEXT;

-- +down
-- Remove test column
ALTER TABLE test_table1 DROP COLUMN test_field;