-- +up
-- Processes table: tracks running orchestrator and control plane processes with heartbeats
-- Enables detection of hung processes (alive but not making progress) via stale heartbeats

CREATE TABLE processes (
    id TEXT PRIMARY KEY,
    process_type TEXT NOT NULL,           -- 'orchestrator' or 'control_plane'
    work_id TEXT,                          -- work ID for orchestrators (NULL for control plane)
    pid INTEGER NOT NULL,                  -- OS process ID
    hostname TEXT NOT NULL DEFAULT '',     -- machine hostname
    heartbeat DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for looking up by type
CREATE INDEX idx_processes_type ON processes(process_type);

-- Index for looking up orchestrators by work_id
CREATE INDEX idx_processes_work_id ON processes(work_id);

-- Index for finding stale heartbeats
CREATE INDEX idx_processes_heartbeat ON processes(heartbeat);

-- Unique partial index: only one orchestrator per work
CREATE UNIQUE INDEX idx_processes_unique_orchestrator ON processes(work_id)
    WHERE process_type = 'orchestrator' AND work_id IS NOT NULL;

-- Unique partial index: only one control plane per project
CREATE UNIQUE INDEX idx_processes_unique_control_plane ON processes(process_type)
    WHERE process_type = 'control_plane';

-- +down
DROP INDEX IF EXISTS idx_processes_unique_control_plane;
DROP INDEX IF EXISTS idx_processes_unique_orchestrator;
DROP INDEX IF EXISTS idx_processes_heartbeat;
DROP INDEX IF EXISTS idx_processes_work_id;
DROP INDEX IF EXISTS idx_processes_type;
DROP TABLE IF EXISTS processes;
