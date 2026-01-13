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
- `cmd/plan.go` - Plan command (create tasks from beads)
- `cmd/poll.go` - Poll command (monitor progress with text output)
- `cmd/proj.go` - Project management (create/destroy/status)
- `cmd/run.go` - Run command (execute pending tasks or works)
- `cmd/status.go` - Status command (show bead tracking status)
- `cmd/sync.go` - Sync command (pull from upstream)
- `cmd/task.go` - Task management (list/show/delete/reset/set-review-epic)
- `cmd/work.go` - Work management (create/list/show/destroy/pr/review)
- `cmd/work_automated.go` - Automated bead-to-PR workflow
- `internal/beads/` - Beads database client (bd CLI wrapper)
- `internal/claude/` - Claude Code invocation
- `internal/db/` - SQLite tracking database
- `internal/task/` - Task planning and complexity estimation
- `internal/git/` - Git operations
- `internal/github/` - PR creation and merging (gh CLI)
- `internal/mise/` - Mise tool management
- `internal/project/` - Project discovery and configuration
- `internal/worktree/` - Git worktree operations

## External Dependencies

Uses CLI tools: `bd`, `claude`, `gh`, `git`, `mise` (optional), `zellij`

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

Three-phase workflow: **work** → **plan** → **run**.

1. Create project: `co proj create <dir> <repo>`
   - Initializes beads: `bd init` and `bd hooks install`
   - If mise enabled (`.mise.toml` or `.tool-versions`): runs `mise trust`, `mise install`
   - If mise `setup` task defined: runs `mise run setup` (use for npm/pnpm install)

2. Create work unit: `co work create`
   - Auto-generates work ID (w-abc, w-xyz, etc.)
   - Creates subdirectory with git worktree: `<project>/<work-id>/tree/`
   - Creates feature branch based on bead titles

3. Plan tasks within work:
   - `cd <work-id> && co plan` - context-aware planning
   - `co plan --work <work-id>` - explicit work specification
   - `co plan --auto-group` - LLM groups beads by complexity
   - `co plan bead-1,bead-2 bead-3` - manual grouping (comma = same task)

4. Execute work: `co run`
   - From work directory: `cd <work-id> && co run`
   - Or explicitly: `co run --work <work-id>`
   - Or run specific work: `co run w-abc`
   - Executes all tasks in the work sequentially:
     - Creates single zellij tab for the work
     - Tasks run in sequence within the work's tab
     - All tasks share the same worktree
     - Claude Code implements changes in the work's worktree
     - Closes beads with `bd close <id> --reason "..."`
   - Creates single PR for entire work when all tasks complete
   - Worktree persists (managed at work level, not task level)

Zellij sessions are named `co-<project-name>` for isolation between projects.

## Work Commands

### `co work create <branch>`
Creates a new work unit with auto-generated ID:
- Requires branch name argument (e.g., `co work create feat/my-feature`)
- Generates hash-based ID: w-abc, w-xyz, etc.
- Creates subdirectory: `<project>/<work-id>/`
- Creates git worktree: `<project>/<work-id>/tree/`
- Pushes branch and sets upstream
- Spawns orchestrator in zellij tab
- Initializes mise if configured
- Use `--base` to specify base branch (default: main)

### `co work list`
Lists all work units with their status:
- Shows ID, status, branch, and PR URL
- Displays summary counts by status

### `co work show [<id>]`
Shows detailed information about a work:
- If no ID provided, detects from current directory
- Displays status, branch, worktree path, PR URL
- Lists associated tasks and their status

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

### `co work create --bead=<bead-ids>`
Automated end-to-end workflow:
- Accepts comma-delimited bead IDs
- Auto-generates branch name from bead title(s)
- Collects transitive dependencies
- Plans tasks with auto-grouping
- Executes all tasks
- Runs review-fix loop until clean
- Creates a pull request
