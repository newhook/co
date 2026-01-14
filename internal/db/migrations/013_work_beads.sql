-- +up
-- Work beads table: beads assigned to work with optional grouping
CREATE TABLE work_beads (
    work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    bead_id TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,  -- 0 = ungrouped, >0 = grouped together
    position INTEGER NOT NULL DEFAULT 0,  -- ordering within work
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_id, bead_id)
);

CREATE INDEX idx_work_beads_work_id ON work_beads(work_id);
CREATE INDEX idx_work_beads_bead_id ON work_beads(bead_id);
CREATE INDEX idx_work_beads_group_id ON work_beads(work_id, group_id);

-- Unique group numbering per work
CREATE TABLE work_bead_group_counters (
    work_id TEXT PRIMARY KEY REFERENCES works(id) ON DELETE CASCADE,
    next_group_id INTEGER NOT NULL DEFAULT 1
);

-- +down
DROP TABLE IF EXISTS work_bead_group_counters;
DROP TABLE IF EXISTS work_beads;
