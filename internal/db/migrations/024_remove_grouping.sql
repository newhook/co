-- +up
-- Remove bead grouping support from work_beads table

-- Drop the group counter table
DROP TABLE IF EXISTS work_bead_group_counters;

-- Drop the group_id index
DROP INDEX IF EXISTS idx_work_beads_group_id;

-- Create new table without group_id
CREATE TABLE work_beads_new (
    work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    bead_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_id, bead_id)
);

-- Copy data (excluding group_id)
INSERT INTO work_beads_new (work_id, bead_id, position, created_at)
SELECT work_id, bead_id, position, created_at FROM work_beads;

-- Drop old table and rename new one
DROP TABLE work_beads;
ALTER TABLE work_beads_new RENAME TO work_beads;

-- Recreate necessary indexes
CREATE INDEX idx_work_beads_work_id ON work_beads(work_id);
CREATE INDEX idx_work_beads_bead_id ON work_beads(bead_id);

-- +down
-- Restore grouping support

-- Recreate table with group_id
CREATE TABLE work_beads_new (
    work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    bead_id TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_id, bead_id)
);

-- Copy data (with default group_id of 0)
INSERT INTO work_beads_new (work_id, bead_id, group_id, position, created_at)
SELECT work_id, bead_id, 0, position, created_at FROM work_beads;

-- Drop current table and rename new one
DROP TABLE work_beads;
ALTER TABLE work_beads_new RENAME TO work_beads;

-- Recreate all indexes
CREATE INDEX idx_work_beads_work_id ON work_beads(work_id);
CREATE INDEX idx_work_beads_bead_id ON work_beads(bead_id);
CREATE INDEX idx_work_beads_group_id ON work_beads(work_id, group_id);

-- Recreate group counter table
CREATE TABLE work_bead_group_counters (
    work_id TEXT PRIMARY KEY REFERENCES works(id) ON DELETE CASCADE,
    next_group_id INTEGER NOT NULL DEFAULT 1
);
