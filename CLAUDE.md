# Claude Orchestrator (co)

Go CLI tool that orchestrates Claude Code to process issues, creating PRs for each.

## Build & Test

```bash
go build -o co .
go test ./...
```

## Project Structure

- `main.go` - CLI entry point (cobra)
- `cmd/complete.go` - Complete command (mark beads/tasks as done or failed)
- `cmd/estimate.go` - Estimate command (report complexity for beads)
- `cmd/list.go` - List command (list tracked beads)
- `cmd/orchestrate.go` - Orchestrate command (internal, execute tasks)
- `cmd/poll.go` - Poll command (monitor progress with text output)
- `cmd/proj.go` - Project management (create/destroy/status)
- `cmd/run.go` - Run command (execute pending tasks or works)
- `cmd/status.go` - Status command (show bead tracking status)
- `cmd/sync.go` - Sync command (pull from upstream)
- `cmd/task.go` - Task management (list/show/delete/reset/set-review-epic)
- `cmd/work.go` - Work management (create/list/show/destroy/pr/review)
- `cmd/work_automated.go` - Automated bead-to-PR workflow
- `cmd/work_feedback.go` - PR feedback processing (fetch feedback, create beads)
- `cmd/linear.go` - Linear integration commands (import issues from Linear)
- `internal/beads/` - Beads database client (bd CLI wrapper)
- `internal/linear/` - Linear MCP client and import logic
- `internal/claude/` - Claude Code invocation
- `internal/db/` - SQLite tracking database
- `internal/task/` - Task planning and complexity estimation
- `internal/git/` - Git operations
- `internal/github/` - PR creation and merging (gh CLI)
- `internal/mise/` - Mise tool management
- `internal/project/` - Project discovery and configuration
- `internal/worktree/` - Git worktree operations
- `internal/logging/` - Structured logging using slog

## External Dependencies

Uses CLI tools: `bd`, `claude`, `gh`, `git`, `mise` (optional), `zellij`

## Context Usage

Functions that execute external commands or perform I/O should accept `context.Context` as their first parameter:

- Use `exec.CommandContext(ctx, ...)` instead of `exec.Command(...)` for shell commands
- Pass context through the call chain from CLI commands down to helper functions
- This enables proper cancellation and timeout handling

## Debug Logging

The project uses Go's `slog` for structured debug logging via `internal/logging`.

- Logs are written to `.co/debug.log` in JSON format (append mode)
- Logging is automatically initialized when a project is loaded
- Log file location: `<project-root>/.co/debug.log`

### Usage

```go
import "github.com/newhook/co/internal/logging"

// Simple logging
logging.Debug("operation started", "key", value)
logging.Info("status update", "count", 42)
logging.Warn("potential issue", "error", err)
logging.Error("operation failed", "error", err, "context", ctx)

// Get logger for custom use
logger := logging.Logger()
logger.With("component", "beads").Debug("message")
```

### Viewing Logs

```bash
# View recent logs
tail -f .co/debug.log

# Pretty-print JSON logs
cat .co/debug.log | jq .
```

## Database Migrations

The project uses a SQLite database (`tracking.db`) with schema migrations.

### Creating a New Migration

1. Create a new SQL file in `internal/db/migrations/` with sequential numbering:
   ```
   internal/db/migrations/005_your_migration_name.sql
   ```

2. Structure the migration with `+up` and `+down` sections:
   ```sql
   -- +up
   -- SQL commands to apply the migration
   CREATE TABLE example (...);

   -- +down
   -- SQL commands to reverse the migration
   DROP TABLE IF EXISTS example;
   ```

3. Migrations run automatically when the database is accessed
4. The `+down` section is critical for rollback capability

### Regenerating SQLC Code

After modifying SQL queries in `internal/db/queries/` or changing the schema:

```bash
mise run sqlc-generate
```

This regenerates the Go code in `internal/db/sqlc/` from the SQL query definitions.

## PR Feedback Integration

The system integrates with GitHub to automatically process PR feedback and create actionable beads.

### Architecture

#### Database Schema

The `pr_feedback` table stores feedback items from GitHub:

```sql
CREATE TABLE pr_feedback (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    pr_url TEXT NOT NULL,
    source TEXT NOT NULL,      -- e.g., "CI: Test Suite", "Review: User123"
    source_id TEXT,             -- GitHub comment ID for resolution
    title TEXT NOT NULL,
    description TEXT,
    feedback_type TEXT NOT NULL, -- test, build, lint, review, security
    priority INTEGER NOT NULL,   -- 0-4 (0=critical, 4=backlog)
    bead_id TEXT,               -- Created bead ID
    processed BOOLEAN DEFAULT FALSE,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### GitHub API Client

Located in `internal/github/`, the integration provides:

- `FetchAndStoreFeedback()`: Fetches PR status checks, workflow runs, comments
- `CreateBeadFromFeedback()`: Creates beads using the bd CLI
- `FeedbackRules`: Configurable rules for what feedback to process

Feedback types processed:
- **CI/Build failures**: Failed status checks and workflow runs
- **Test failures**: Extracted from CI logs using pattern matching
- **Lint errors**: Code style and quality issues
- **Review comments**: Actionable feedback from code reviews
- **Security issues**: Vulnerabilities and dependency issues

#### Orchestrator Integration

The orchestrator (`cmd/orchestrate.go`) includes three feedback-related goroutines:

1. **Manual Poll Watcher**: Watches for signal files to trigger on-demand polling
2. **Automatic Feedback Poller**: Polls every 5 minutes for new PR feedback
3. **Comment Resolution Poller**: Posts resolution comments when beads are closed

#### Feedback Processing Flow

1. GitHub PR has new feedback (comments, CI failures, etc.)
2. Orchestrator polls feedback (automatic or manual trigger)
3. `co work feedback` command fetches and processes feedback:
   - Queries GitHub API for PR data
   - Filters actionable items based on rules
   - Stores in pr_feedback table
   - Creates beads under work's root issue
   - Optionally adds beads to work for immediate execution
4. Claude addresses feedback beads in subsequent tasks
5. When bead is closed, resolution comment is posted to GitHub

### Commands

#### `co work feedback [<work-id>]`

Processes PR feedback for a work unit:

Options:
- `--dry-run`: Preview without creating beads
- `--auto-add`: Automatically add created beads to work
- `--min-priority N`: Set minimum priority for created beads (0-4)

The command:
1. Fetches PR status, comments, and workflow runs from GitHub
2. Filters for actionable feedback based on configured rules
3. Creates beads for each feedback item
4. Stores feedback in database for tracking
5. Optionally adds beads to work for immediate execution

### TUI Integration

The TUI (`co tui`) provides:
- F5 key binding for manual feedback polling
- Visual feedback indicator showing polling status
- Automatic refresh when new beads are created from feedback

## PR Requirements

**NEVER push directly to main.** All changes must go through a PR.

### Correct PR Workflow

When implementing changes:
1. Create a feature branch: `git checkout -b feature-name`
2. Make and commit changes on the feature branch
3. Push the feature branch: `git push -u origin feature-name`
4. Create a PR: `gh pr create --title "..." --body "..."`
5. Merge the PR: `gh pr merge --squash`
6. Delete the branch: `git branch -d feature-name`

All PRs must be squash merged.

## Project Model

All commands require a project context. Projects are created with `co proj create` and have this structure:

```
<project-dir>/
├── .co/
│   ├── config.toml      # Project configuration
│   └── tracking.db      # SQLite coordination database
├── main/                # Symlink (local) or clone (GitHub)
│   └── .beads/          # Beads issue tracker
└── <work-id>/           # Work subdirectory (e.g., w-abc, w-xyz)
    └── tree/            # Git worktree for this work
```

## Work Concept (3-Tier Hierarchy)

The system uses a 3-tier hierarchy: **Work → Tasks → Beads**

- **Work**: Controls git worktree, feature branch, and zellij tab
- **Tasks**: Run as Claude Code sessions sequentially within work's tab
- **Beads**: Individual issues solved within tasks

## Workflow

Two-phase workflow: **work** → **run**.

1. Create project: `co proj create <dir> <repo>`
   - Initializes beads: `bd init` and `bd hooks install`
   - If mise enabled (`.mise.toml` or `.tool-versions`): runs `mise trust`, `mise install`
   - If mise `setup` task defined: runs `mise run setup` (use for npm/pnpm install)

2. Create work unit with beads: `co work create <bead-args...>`
   - Syntax: `co work create bead-1,bead-2 bead-3` (comma = same group, space = different groups)
   - Auto-generates work ID (w-abc, w-xyz, etc.)
   - Generates branch name from bead titles, prompts for confirmation
   - Expands epics to include all child beads
   - Creates subdirectory with git worktree: `<project>/<work-id>/tree/`
   - Use `--auto` for full automated workflow (implement, review/fix loop, PR)

3. Execute work: `co run`
   - From work directory: `cd <work-id> && co run`
   - Or explicitly: `co run --work <work-id>`
   - Automatically creates tasks from unassigned work beads
   - Use `--plan` for LLM complexity estimation to auto-group beads
   - Use `--auto` for full automated workflow (implement, review/fix loop, PR)
   - Executes all tasks in the work sequentially:
     - Creates single zellij tab for the work
     - Tasks run in sequence within the work's tab
     - All tasks share the same worktree
     - Claude Code implements changes in the work's worktree
     - Closes beads with `bd close <id> --reason "..."`
   - Worktree persists (managed at work level, not task level)

Zellij sessions are named `co-<project-name>` for isolation between projects.

## Work Commands

### `co work create <bead-args...>`
Creates a new work unit with beads:
- Syntax: `co work create bead-1,bead-2 bead-3`
  - Comma-separated beads are grouped together for a single task
  - Space-separated arguments create separate task groups
- Generates branch name from bead titles, prompts for confirmation
- Expands epics to include all child beads with transitive dependencies
- Auto-generates work ID (w-abc, w-xyz, etc.)
- Creates subdirectory: `<project>/<work-id>/`
- Creates git worktree: `<project>/<work-id>/tree/`
- Pushes branch and sets upstream
- Spawns orchestrator in zellij tab
- Initializes mise if configured
- Use `--base` to specify base branch (default: main)
- Use `--auto` for full automated workflow (implement, review/fix loop, PR)

### `co work add <bead-args...>`
Adds beads to an existing work:
- Syntax: `co work add bead-4,bead-5 bead-6 [--work=<id>]`
  - Same grouping syntax as `create`
- Detects work from current directory or uses `--work` flag
- Expands epics to include all child beads
- Cannot add beads already assigned to a task

### `co work remove <bead-ids...>`
Removes beads from an existing work:
- Syntax: `co work remove bead-4 bead-5 [--work=<id>]`
- Detects work from current directory or uses `--work` flag
- Cannot remove beads already assigned to a pending/processing task

### `co work list`
Lists all work units with their status:
- Shows ID, status, branch, and PR URL
- Displays summary counts by status

### `co work show [<id>]`
Shows detailed information about a work:
- If no ID provided, detects from current directory
- Displays status, branch, worktree path, PR URL
- Lists associated beads and tasks with their status

### `co work destroy <id>`
Destroys a work unit and its resources:
- Removes git worktree
- Deletes work subdirectory
- Updates database records
- Use with caution - destructive operation

## Task Commands

### `co task list`
Lists all tasks with their status:
- Shows ID, status, type, budget, creation time, and associated beads
- Filter by status: `--status pending|processing|completed|failed`
- Filter by type: `--type estimate|implement`

### `co task show <id>`
Shows detailed information about a task:
- Displays status, type, budget, timestamps
- Lists associated beads and their completion status
- Shows worktree path, zellij session/pane if applicable

### `co task delete <id>...`
Deletes one or more tasks from the database:
- Removes task and all associated records
- Accepts multiple task IDs

### `co task reset <id>`
Resets a failed or stuck task to pending:
- Changes task status from processing/failed back to pending
- Resets all bead statuses for the task
- Use when a task gets stuck or needs to be rerun

### `co task set-review-epic <epic-id>`
Associates a review epic with a review task:
- Sets the review_epic_id metadata on a review task
- Task is auto-detected from CO_TASK_ID env var or current processing review task
- Use `--task` flag for explicit specification

## Additional Commands

### `co complete <bead-id|task-id>`
Marks a bead or task as completed (or failed with --error):
- Called by Claude Code when work is done
- Supports both bead IDs and task IDs (task IDs contain dots like "w-xxx.1")
- Use `--error "message"` to mark a task as failed
- Use `--pr "url"` to associate a PR URL with completion

### `co estimate <bead-id>`
Reports complexity estimate for a bead:
- Called by Claude Code during estimation tasks
- Required flags: `--score` (1-10) and `--tokens` (5000-50000)
- Optional: `--task` to specify the task ID

### `co list`
Lists tracked beads in the database:
- Filter by status with `--status` (pending, processing, completed, failed)
- Shows ID, status, title, and PR URL

### `co status [bead-id]`
Shows bead tracking status:
- With a bead ID: shows detailed status including zellij session/pane info
- Without ID: shows all beads currently processing with their session/pane

### `co poll [work-id|task-id]`
Monitors work/task progress with simple text output:
- Without arguments: monitors all active works or detected work from directory
- With work ID: monitors that work's tasks
- With task ID: monitors that specific task
- Use `--interval` to set polling interval (default: 2s)
- For interactive TUI with management features, use `co tui` instead

### `co sync`
Pulls from upstream in all repositories:
- Runs git pull in each worktree (main and all work worktrees)
- Skips internal beads worktrees

### `co work pr [<id>]`
Creates a PR task for Claude to generate a pull request:
- Work must be completed before creating PR
- If no ID provided, uses work from current directory

### `co work review [<id>]`
Creates a review task to examine code changes:
- Claude examines the work's branch for quality and security issues
- Generates unique review task IDs (w-xxx.review-1, w-xxx.review-2, etc.)
- Use `--auto` for review-fix loop until clean (max 3 iterations)
