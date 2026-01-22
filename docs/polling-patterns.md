# Polling Patterns Documentation

This document explains the polling patterns in the codebase and why certain patterns should remain as-is rather than being converted to database watchers.

## Polling Patterns That Have Been Converted to Watchers

The following polling patterns have been successfully replaced with database watchers for improved efficiency:

1. **TUI Work/Task Updates** (Previously: 5-second polling)
   - Location: `cmd/tui_plan.go`
   - Now uses tracking database watcher for real-time updates

2. **Scheduler Task Polling** (Previously: configurable interval polling)
   - Location: `cmd/scheduler_handler.go`
   - Now uses tracking database watcher with 30-second safety net timer

3. **Claude Task Completion Monitoring** (Previously: 2-second polling)
   - Location: `internal/claude/inline.go`
   - Now uses tracking database watcher for immediate task status detection

4. **Orchestrator Main Loop** (Previously: 5-10 second polling)
   - Location: `cmd/orchestrate.go`
   - Polling intervals reduced to 2 seconds as watchers handle most events

## Polling Patterns That Should Remain As-Is

The following polling patterns should NOT be converted to watchers:

### 1. Activity Tracker Heartbeat
**Location:** `cmd/orchestrate.go` - Activity ticker (30-second interval)

**Why Keep Polling:**
- Updates `last_activity` timestamps for health monitoring
- Used to detect stuck or crashed tasks
- Requires periodic writes regardless of database changes
- Not triggered by external events but rather by time passage

### 2. GitHub API Polling
**Location:** `cmd/orchestrate.go` - PR feedback polling (configurable interval)

**Why Keep Polling:**
- GitHub API is external and doesn't support webhooks for all events
- Rate limits require controlled polling intervals
- Need to respect GitHub API usage guidelines
- Cannot use file system watchers for external APIs

### 3. User-Facing Poll Command
**Location:** `cmd/poll.go`

**Why Keep Polling:**
- Explicitly requested polling behavior by the user
- Provides human-readable progress updates
- Users expect to see continuous status updates
- Interactive command that should work without database watchers

### 4. Test Polling Utilities
**Location:** Various test files

**Why Keep Polling:**
- Test environments may not have watchers available
- Tests need predictable timing for assertions
- Simpler setup without watcher dependencies
- Test isolation requirements

### 5. Zellij Pane/Session Status Checks
**Location:** Various zellij integration points

**Why Keep Polling:**
- Zellij doesn't provide event notifications for all state changes
- External process monitoring requires periodic checks
- Terminal multiplexer state is outside our control

## Guidelines for Future Development

When deciding whether to use polling or watchers:

### Use Watchers When:
- Monitoring local database changes
- Need immediate response to state changes
- High-frequency polling would waste resources
- Events are clearly defined and detectable

### Keep Polling When:
- Interfacing with external APIs
- Time-based actions (heartbeats, timeouts)
- User explicitly requests polling behavior
- Watcher setup complexity outweighs benefits
- Need fallback for environments without watcher support

## Implementation Notes

- All watcher implementations should include polling fallbacks
- Use sensible safety net timers (30 seconds) even with watchers
- Log clearly whether using watchers or polling for debugging
- Consider watcher resource usage in containerized environments