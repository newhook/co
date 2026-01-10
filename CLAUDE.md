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
- `cmd/run.go` - Run command (execute pending tasks)
- `cmd/proj.go` - Project management (create/destroy/status)
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
└── <task-id>/           # Worktrees for active tasks
```

## Workflow

Two-phase workflow: **plan** then **run**.

1. Create project: `co proj create <dir> <repo>`
   - Initializes beads: `bd init` and `bd hooks install`
   - If mise enabled (`.mise.toml` or `.tool-versions`): runs `mise trust`, `mise install`
   - If mise `setup` task defined: runs `mise run setup` (use for npm/pnpm install)
2. Plan tasks from beads:
   - `co plan` - one task per ready bead
   - `co plan --auto-group` - LLM groups beads by complexity
   - `co plan bead-1,bead-2 bead-3` - manual grouping (comma = same task)
3. Execute tasks: `co run`
   - Derives task dependencies from bead dependencies
   - Executes in topological order
   - For each task:
     - Create worktree: `git worktree add ../<task-id> -b task/<task-id>`
     - Initialize mise in worktree (if enabled)
     - Claude Code implements changes in isolated worktree
     - Close beads with `bd close <id> --reason "..."`
     - Create PR and merge it
     - Remove worktree on success (keep on failure for debugging)

Zellij sessions are named `co-<project-name>` for isolation between projects.
