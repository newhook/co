# Claude Orchestrator (co)

Go CLI tool that orchestrates Claude Code to process issues, creating PRs for each.

## Build & Test

```bash
go build -o co .
go test ./...
```

## Project Structure

- `main.go` - CLI entry point (cobra)
- `cmd/plan.go` - Plan command (create tasks from beads)
- `cmd/run.go` - Run command (execute pending tasks or works)
- `cmd/proj.go` - Project management (create/destroy/status)
- `cmd/work.go` - Work management (create/list/show/destroy)
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

### `co work create`
Creates a new work unit with auto-generated ID:
- Generates hash-based ID: w-abc, w-xyz, etc.
- Creates subdirectory: `<project>/<work-id>/`
- Creates git worktree: `<project>/<work-id>/tree/`
- Creates feature branch based on bead titles
- Initializes mise if configured

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
