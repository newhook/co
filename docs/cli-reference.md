# CLI Reference

This document provides detailed documentation for all `co` CLI commands.

## Work Commands

### `co work create <bead-args...>`

Creates a new work unit from one or more beads.

```bash
co work create bead-1           # Single bead
co work create bead-1 bead-2    # Multiple beads
co work create epic-1           # Epic (includes all children)
co work create bead-1 --auto    # Automated workflow
```

This creates:
- A work directory (`w-abc/`)
- A git worktree with a generated branch (`w-abc/tree/`)
- A unique work ID using content-based hashing

If the bead is an epic, all child beads are automatically included.
Transitive dependencies are also included.

The branch name is generated from bead titles and you're prompted for confirmation.

| Flag | Description |
|------|-------------|
| `--auto` | Full automated workflow (implement, review/fix loop, PR) |

Base branch is configured in `config.toml` under `[repo] base_branch` (default: main).

### `co work add <bead-args...>`

Adds beads to an existing work.

```bash
co work add bead-4 bead-5           # In work directory
co work add bead-4 --work w-abc     # Explicit work ID
```

- Detects work from current directory or uses `--work` flag
- Expands epics to include all child beads
- Cannot add beads already assigned to a task

### `co work remove <bead-ids...>`

Removes beads from an existing work.

```bash
co work remove bead-4 bead-5        # In work directory
co work remove bead-4 --work w-abc  # Explicit work ID
```

- Detects work from current directory or uses `--work` flag
- Cannot remove beads already assigned to a pending/processing task

### `co work list`

Lists all work units with their status.

```bash
co work list
```

Shows ID, status, branch, and PR URL. Displays summary counts by status.

### `co work show [<id>]`

Shows detailed information about a work.

```bash
co work show          # Current directory
co work show w-abc    # Explicit ID
```

Displays status, branch, worktree path, PR URL. Lists associated beads and tasks with their status.

### `co work destroy <id>`

Destroys a work unit and its resources.

```bash
co work destroy w-abc
```

- Removes git worktree
- Deletes work subdirectory
- Updates database records
- Use with caution - destructive operation

### `co work restart [<id>]`

Restarts a failed work.

```bash
co work restart         # Current directory
co work restart w-abc   # Explicit ID
```

- Only works if work is in `failed` status
- Transitions work back to `processing`
- Orchestrator will resume processing pending tasks

### `co work complete [<id>]`

Explicitly marks an idle work as completed.

```bash
co work complete        # Current directory
co work complete w-abc  # Explicit ID
```

- Only works if work is in `idle` status
- Transitions work to `completed` (terminal state)
- Use when PR is merged or work is truly finished

### `co work pr [<id>]`

Creates a PR task for Claude to generate a pull request.

```bash
co work pr          # Current directory
co work pr w-abc    # Explicit ID
```

Work must be completed before creating PR. After creating the PR task, run `co run` to execute it.

### `co work review [<id>]`

Creates a review task to examine code changes.

```bash
co work review              # Current directory
co work review w-abc        # Explicit ID
co work review --auto       # Review-fix loop
```

| Flag | Description |
|------|-------------|
| `--auto` | Loop review/fix until clean (max 3 iterations) |

Claude examines the work's branch for quality and security issues and creates beads for issues found.

### `co work feedback [<id>]`

Processes PR feedback and creates beads from actionable items.

```bash
co work feedback                    # Current directory
co work feedback w-abc              # Explicit ID
co work feedback --dry-run          # Preview only
co work feedback --auto-add         # Add beads to work
co work feedback --min-priority 2   # Filter by priority
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what beads would be created |
| `--auto-add` | Automatically add beads to work |
| `--min-priority N` | Set minimum priority (0-4) |

The feedback system processes:
- **CI/Build Failures**: Failed status checks and workflow runs
- **Test Failures**: Extracts specific test failures from logs
- **Lint Errors**: Code style and quality issues
- **Review Comments**: Actionable feedback from code reviews
- **Security Issues**: Vulnerabilities and security concerns

## Run Command

### `co run`

Executes pending tasks for a work unit.

```bash
co run                      # Current work directory
co run --work w-abc         # Explicit work ID
co run --plan               # LLM complexity grouping
co run --auto               # Full automated workflow
co run --dry-run            # Preview execution plan
```

| Flag | Short | Description |
|------|-------|-------------|
| `--limit` | `-n` | Maximum number of tasks to process (0 = unlimited) |
| `--dry-run` | | Show execution plan without running |
| `--plan` | | Use LLM complexity estimation to auto-group beads |
| `--auto` | | Full automated workflow (implement, review/fix loop, PR) |
| `--project` | | Specify project directory (default: auto-detect from cwd) |
| `--work` | | Specify work ID (default: auto-detect from current directory) |

## Task Commands

### `co task list`

Lists all tasks with their status.

```bash
co task list                    # All tasks
co task list --status pending   # Filter by status
co task list --type estimate    # Filter by type
```

| Flag | Description |
|------|-------------|
| `--status` | Filter: pending, processing, completed, failed |
| `--type` | Filter: estimate, implement |

### `co task show <id>`

Shows detailed information about a task.

```bash
co task show w-abc.1
```

Displays status, type, budget, timestamps. Lists associated beads and their completion status.

### `co task delete <id>...`

Deletes one or more tasks from the database.

```bash
co task delete w-abc.1
co task delete w-abc.1 w-abc.2  # Multiple tasks
```

### `co task reset <id>`

Resets a failed or stuck task to pending.

```bash
co task reset w-abc.1
```

Changes task status from processing/failed back to pending. Resets all bead statuses for the task.

### `co task set-review-epic <epic-id>`

Associates a review epic with a review task.

```bash
co task set-review-epic epic-1
co task set-review-epic epic-1 --task w-abc.2
```

Task is auto-detected from CO_TASK_ID env var or current processing review task.

## Monitoring Commands

### `co tui`

Interactive TUI for managing works and beads (lazygit-style).

```bash
co tui
```

Features:
- Three-panel drill-down: Beads → Works → Tasks
- Create/destroy works, run tasks
- Bead filtering (ready/open/closed), search, multi-select
- Keyboard shortcuts for all operations (press `?` for help)
- F5 to poll PR feedback on-demand

### `co poll [work-id|task-id]`

Monitor work/task progress with text output.

```bash
co poll             # All active works
co poll w-abc       # Specific work
co poll w-abc.1     # Specific task
co poll --interval 5s
```

| Flag | Description |
|------|-------------|
| `--interval` | Polling interval (default: 2s) |

## Other Commands

### `co status [bead-id]`

Shows bead tracking status.

```bash
co status           # All processing beads
co status bead-1    # Specific bead
```

### `co list`

Lists tracked beads in the database.

```bash
co list
co list --status pending
co list --status completed
```

| Flag | Description |
|------|-------------|
| `--status` | Filter: pending, processing, completed, failed |

### `co sync`

Pulls from upstream in all repositories.

```bash
co sync
```

Runs git pull in each worktree (main and all work worktrees).

## Linear Integration

### `co linear import <issues...>`

Import issues from Linear into the beads issue tracker.

```bash
# Import single issue
co linear import ENG-123
co linear import https://linear.app/company/issue/ENG-123/title

# Import multiple issues
co linear import ENG-123 ENG-124 ENG-125

# Import with dependencies
co linear import ENG-123 --create-deps --max-dep-depth=2

# Update existing bead
co linear import ENG-123 --update

# Preview
co linear import ENG-123 --dry-run
```

| Flag | Description |
|------|-------------|
| `--api-key` | Linear API key (or use `[linear] api_key` in config.toml) |
| `--create-deps` | Import blocking issues as dependencies |
| `--max-dep-depth` | Maximum depth for dependency import (default: 1) |
| `--update` | Update existing beads if already imported |
| `--dry-run` | Preview import without creating beads |
| `--status-filter` | Only import issues matching status |
| `--priority-filter` | Only import issues matching priority |
| `--assignee-filter` | Only import issues matching assignee |

Linear metadata (ID, URL, assignee, labels) is preserved in the imported bead.

## Agent Commands

These commands are called by Claude Code during task execution. Not intended for direct user invocation.

### `co complete <bead-id|task-id>`

Marks a bead or task as completed (or failed with --error).

```bash
co complete bead-1
co complete w-abc.1
co complete w-abc.1 --error "Build failed"
co complete w-abc.1 --pr "https://github.com/user/repo/pull/123"
```

| Flag | Description |
|------|-------------|
| `--error` | Mark as failed with error message |
| `--pr` | Associate a PR URL with completion |

### `co estimate <bead-id>`

Reports complexity estimate for a bead.

```bash
co estimate bead-1 --score 5 --tokens 15000
co estimate bead-1 --score 5 --tokens 15000 --task w-abc.1
```

| Flag | Description |
|------|-------------|
| `--score` | Complexity score (1-10) |
| `--tokens` | Estimated tokens (5000-50000) |
| `--task` | Task ID (optional) |

## Work Status States

Works have the following status states:

| Status | Meaning |
|--------|---------|
| `pending` | Work created, no tasks started yet |
| `processing` | At least one task is running |
| `idle` | All tasks done, waiting for more work (e.g., PR feedback) |
| `completed` | Truly finished - explicitly closed by user |
| `failed` | A task failed - requires user intervention |
| `merged` | PR was merged on GitHub (auto-detected) |

**Key behaviors:**
- When all tasks complete successfully → work transitions to `idle` (not `completed`)
- When a task fails → work transitions to `failed` and orchestrator halts
- When new tasks are added to an idle work → work resumes to `processing`
- When PR is merged on GitHub → work automatically transitions to `merged`
- User must explicitly run `co work complete` to mark work as truly done
- User must run `co work restart` to resume a failed work after fixing issues

## ID Generation

CO uses a hierarchical ID system:

- **Work IDs**: Content-based hash (e.g., `w-8xa`)
  - Generated from branch name + project + timestamp
  - 3-8 character base36 hash
  - Collision-resistant with automatic lengthening

- **Task IDs**: Hierarchical format (e.g., `w-8xa.1`, `w-8xa.2`)
  - Format: `<work-id>.<sequence>`
  - Sequential numbering within each work

- **Bead IDs**: Managed by beads system (e.g., `ac-pjw`)
  - Project-specific prefixes
  - Content-based hashing similar to works

## Task Dependencies

Task dependencies are derived automatically from bead dependencies:
- If bead A depends on bead B, and they're in different tasks, task(A) depends on task(B)
- `co run` executes tasks in the correct dependency order
- Cycles are detected and reported as errors

## Error Handling and Retries

When a task fails:
- The task is automatically marked as failed in the database
- Claude can signal failure using `co complete <task-id> --error "message"`
- To retry a failed task:
  ```bash
  co task reset <task-id>    # Reset task status to pending
  co run                     # Retry the task
  ```
- On retry, Claude only processes incomplete beads (already completed beads are skipped)
