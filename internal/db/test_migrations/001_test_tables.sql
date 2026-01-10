-- +up
-- Test tables for migration tests
CREATE TABLE test_table1 (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_test_table1_name ON test_table1(name);

CREATE TABLE test_table2 (
    id INTEGER PRIMARY KEY,
    test1_id INTEGER,
    value TEXT,
    FOREIGN KEY (test1_id) REFERENCES test_table1(id)
);

-- +down
DROP TABLE test_table2;
DROP INDEX idx_test_table1_name;
DROP TABLE test_table1;