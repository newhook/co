# Claude Orchestrator (co)

A Go CLI tool that orchestrates Claude Code to process issues, creating PRs for each.

## Prerequisites

The following CLI tools must be installed and available in your PATH:

| Tool | Purpose | Installation |
|------|---------|--------------|
| `bd` | Beads issue tracking | `curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh \| bash` |
| `claude` | Claude Code CLI | [docs.anthropic.com/claude-code](https://docs.anthropic.com/en/docs/claude-code) |
| `gh` | GitHub CLI | [cli.github.com](https://cli.github.com/) |
| `git` | Version control | Usually pre-installed |
| `zellij` | Terminal multiplexer | [zellij.dev](https://zellij.dev/) |

## Installation

```bash
go install github.com/newhook/co@latest
```

### Build from Source (Alternative)

```bash
git clone https://github.com/newhook/co.git
cd co
go build -o co .
mv co /usr/local/bin/
```

### Verify Installation

```bash
co --help
```

## Project Setup

Claude Orchestrator uses a project-based model with a 3-tier hierarchy: **Work → Tasks → Beads**

```
<project-dir>/
├── .co/
│   ├── config.toml      # Project configuration
│   └── tracking.db      # SQLite coordination database
├── main/                # Symlink to local repo OR clone from GitHub
│   └── .beads/          # Beads issue tracker (in repo)
├── w-8xa/               # Work unit directory
│   └── tree/            # Git worktree for feature branch
├── w-3kp/               # Another work unit
│   └── tree/            # Git worktree for its feature branch
└── ...
```

### Hierarchy

- **Work**: A feature branch with its own worktree, groups related tasks (ID: `w-8xa`)
- **Tasks**: Units of Claude execution within a work (ID: `w-8xa.1`, `w-8xa.2`)
- **Beads**: Individual issues from the beads tracker (ID: `ac-pjw`)

### Create a Project

From a local repository:
```bash
co proj create ~/myproject ~/path/to/repo
```

From a GitHub repository:
```bash
co proj create ~/myproject https://github.com/user/repo
```

This creates the project structure with `main/` pointing to your repository.

### Project Commands

| Command | Description |
|---------|-------------|
| `co proj create <dir> <repo>` | Create a new project (local path or GitHub URL) |
| `co proj destroy [--force]` | Remove project and all worktrees |
| `co proj status` | Show project info, worktrees, and task status |

## Usage

All commands must be run from within a project directory (or subdirectory).

### Quick Start

```bash
# Create a project
co proj create ~/myproject https://github.com/user/repo
cd ~/myproject

# Create a work unit for your feature
co work create feature/user-auth
# → Creates w-8xa/tree/ worktree

# Plan and execute tasks
cd w-8xa
co plan --auto-group    # Groups beads into tasks
co run                  # Executes tasks, creates PR
```

Claude Orchestrator uses a three-phase workflow: **work** → **plan** → **run**.

### Phase 1: Work - Create a Work Unit

```bash
co work create feature/my-feature     # Create work with feature branch
```

This creates:
- A work directory (`w-abc/`)
- A git worktree with the specified branch (`w-abc/tree/`)
- A unique work ID using content-based hashing

### Phase 2: Plan - Create Tasks from Beads

```bash
cd w-abc                             # Enter work directory
co plan                              # One task per ready bead (default)
co plan --auto-group                 # LLM groups beads by complexity
co plan bead-1,bead-2 bead-3         # Manual: task1=[bead-1,bead-2], task2=[bead-3]
```

The plan command creates tasks under the current work.

**Manual grouping syntax:**
- `bead-1,bead-2` - beads separated by commas are grouped into one task
- Space-separated arguments create separate tasks
- Example: `co plan a,b c,d e` creates 3 tasks: [a,b], [c,d], [e]

### Phase 3: Run - Execute Pending Tasks

```bash
co run                               # Execute all pending tasks in current work
co run w-abc.1                       # Execute specific task
co run w-abc                         # Execute all tasks in work w-abc
```

Tasks within a work are executed sequentially in the work's worktree.

### Work Commands

| Command | Description |
|---------|-------------|
| `co work create <branch>` | Create a new work unit with specified feature branch |
| `co work list` | List all work units with their status |
| `co work show [<id>]` | Show detailed work information (current directory or specified) |
| `co work pr [<id>]` | Create a PR task for Claude to generate pull request |
| `co work destroy <id>` | Delete work unit and all associated data |

### Plan Command Flags

| Flag | Description |
|------|-------------|
| `--auto-group` | Automatically group beads by complexity using LLM estimation |
| `--budget` | Complexity budget per task (1-100, default: 70, used with `--auto-group`) |
| `--project` | Specify project directory (default: auto-detect from cwd) |
| `--work` | Specify work ID (default: auto-detect from current directory) |

### Run Command Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--limit` | `-n` | Maximum number of tasks to process (0 = unlimited) |
| `--dry-run` | | Show execution plan without running |
| `--auto-close` | | Automatically close zellij tabs after task completion |
| `--project` | | Specify project directory (default: auto-detect from cwd) |
| `--work` | | Specify work ID (default: auto-detect from current directory) |

### Typical Workflow Example

```bash
# 1. Create a work unit for your feature
co work create feature/user-auth
# Output: Generated work ID: w-8xa (from branch: feature/user-auth)

# 2. Navigate to the work directory
cd w-8xa

# 3. Plan tasks from ready beads
co plan --auto-group

# 4. Execute tasks
co run

# 5. Create PR (Claude generates comprehensive description)
co work pr
co run w-8xa.pr  # Execute Claude to create the PR

# 6. Review and merge PR manually
gh pr merge --squash

# 7. Clean up
co work destroy w-8xa
```

Each work has its own feature branch, and all tasks within the work are executed sequentially in the work's worktree.

### Auto-Grouping

Use `--auto-group` during planning to have the LLM automatically group beads by complexity:

```bash
co plan --auto-group
co run
```

This mode:
1. Estimates complexity for each bead using an LLM
2. Groups beads into tasks using bin-packing algorithm (respecting dependencies)
3. Each task is processed in a single Claude session
4. Handles partial failures gracefully (creates partial PRs for completed work)

Control the grouping with `--budget`:
```bash
co plan --auto-group --budget 50  # Smaller tasks (fewer beads per task)
co plan --auto-group --budget 90  # Larger tasks (more beads per task)
```

### Task Dependencies

Task dependencies are derived automatically from bead dependencies:
- If bead A depends on bead B, and they're in different tasks, task(A) depends on task(B)
- `co run` executes tasks in the correct dependency order
- Cycles are detected and reported as errors

### Task Management

Manage tasks with the `co task` command:

```bash
co task list                    # List all tasks
co task list --status pending   # List pending tasks
co task list --type estimate    # List estimation tasks
co task show <task-id>          # Show detailed task information
co task delete <task-id>        # Delete a task from database
co task reset <task-id>         # Reset failed/stuck task to pending
```

#### Error Handling and Retries

When a task fails:
- The task is automatically marked as failed in the database
- Claude can signal failure using `co complete <task-id> --error "message"`
- To retry a failed task:
  ```bash
  co task reset <task-id>    # Reset task status to pending
  co run <task-id>           # Retry the task
  ```
- On retry, Claude only processes incomplete beads (already completed beads are skipped)

### ID Generation

CO uses a hierarchical ID system:

- **Work IDs**: Content-based hash (e.g., `w-8xa`)
  - Generated from branch name + project + timestamp
  - 3-8 character base36 hash
  - Collision-resistant with automatic lengthening

- **Task IDs**: Hierarchical format (e.g., `w-8xa.1`, `w-8xa.2`)
  - Format: `<work-id>.<sequence>`
  - Sequential numbering within each work
  - Shows clear task ownership

- **Bead IDs**: Managed by beads system (e.g., `ac-pjw`)
  - Project-specific prefixes
  - Content-based hashing similar to works

### Other Commands

| Command | Description |
|---------|-------------|
| `co status [bead-id]` | Show tracking status for beads |
| `co list [-s status]` | List tracked beads with optional status filter |
| `co complete <bead-id> [--pr URL] [--error "message"]` | Mark a bead/task as completed or failed (called by Claude) |
| `co estimate <bead-id> --score N --tokens N` | Report complexity estimate for a bead (called by Claude during estimation) |

## How It Works

Claude Orchestrator uses a three-phase workflow with the Work → Tasks → Beads hierarchy:

```
┌─────────────────────────────────────────────────────────────────┐
│                      co work create                             │
├─────────────────────────────────────────────────────────────────┤
│  1. Generate unique work ID using content-based hashing         │
│  2. Create work directory: <project>/<work-id>/                 │
│  3. Create git worktree: <work-id>/tree/                        │
│     └─ git worktree add tree -b <branch-name>                   │
│  4. Create zellij tab for the work                              │
│  5. Store work in tracking database                             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        co plan                                  │
├─────────────────────────────────────────────────────────────────┤
│  1. Detect work context from current directory                  │
│  2. Query ready beads from main/.beads/ (bd ready --json)       │
│  3. Create tasks from beads:                                    │
│     • Default: one task per bead                                │
│     • --auto-group: LLM estimates complexity, bin-packs         │
│     • Manual: user specifies groupings                          │
│  4. Assign hierarchical task IDs: w-abc.1, w-abc.2, etc.        │
│  5. Store tasks in tracking database under the work             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        co run                                   │
├─────────────────────────────────────────────────────────────────┤
│  1. Load work and its pending tasks from database               │
│  2. Execute tasks sequentially in the work's worktree:          │
│     │                                                           │
│     ├─ For each task:                                           │
│     │  ├─ Check for uncommitted changes:                        │
│     │  │  • If related to task beads: complete implementation  │
│     │  │  • If unrelated: stash with descriptive message       │
│     │  │  • Fail-fast if can't handle cleanly                  │
│     │  │                                                        │
│     │  ├─ Run Claude Code in work's zellij tab                  │
│     │  │  └─ zellij run -- claude --dangerously-skip-permissions│
│     │  │     Claude receives prompt and autonomously:           │
│     │  │     • Implements all beads in the task                 │
│     │  │     • Commits after each bead completion               │
│     │  │     • Pushes commits to remote immediately            │
│     │  │     • Closes beads (bd close <id> --reason "...")      │
│     │  │     • Signals completion (co complete <id>)            │
│     │  │     • Or signals failure (co complete <id> --error)   │
│     │  │                                                        │
│     │  └─ Manager polls database for completion                 │
│     │                                                           │
│  3. After all tasks complete:                                   │
│     ├─ Mark work as completed                                   │
│     └─ Prompt user to create PR: co work pr                     │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      co work pr                                 │
├─────────────────────────────────────────────────────────────────┤
│  1. Check work is completed and no PR exists                    │
│  2. Create special PR task (w-abc.pr)                           │
│  3. User runs: co run w-abc.pr                                  │
│  4. Claude analyzes all changes and completed work:             │
│     ├─ Reviews git log and diff                                 │
│     ├─ Summarizes completed tasks and beads                     │
│     ├─ Generates comprehensive PR description                   │
│     ├─ Creates PR using gh pr create                            │
│     └─ Returns PR URL (does NOT auto-merge)                     │
│  5. User reviews and merges PR manually                         │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

- **Three-tier hierarchy**: Work → Tasks → Beads provides clear organization
- **Work-based isolation**: Each work has its own worktree and feature branch
- **Content-based IDs**: Works use hash-based IDs (w-abc), tasks use hierarchical IDs (w-abc.1)
- **Three-phase workflow**: Work creation, planning, and execution are separate phases
- **Sequential task execution**: Tasks within a work run sequentially in the same worktree
- **Project isolation**: Each project has its own tracking database in `.co/`
- **Claude handles implementation**: Claude autonomously implements, commits, and closes beads
- **Zellij for terminal management**: Each work gets its own tab in the project's zellij session
- **Database polling**: Manager polls SQLite database for completion signals from Claude
- **Fail-fast for uncommitted changes**: Tasks verify clean working tree and handle partial work appropriately
- **Continuous integration**: Each bead completion is committed and pushed immediately to prevent work loss
- **Intelligent retries**: Failed tasks can be reset and retried, with Claude skipping already-completed beads
- **Error signaling**: Claude can explicitly mark tasks as failed with error messages for better debugging

## Project Structure

```
co/
├── main.go              # CLI entry point
├── cmd/
│   ├── root.go          # Root command
│   ├── work.go          # Work management commands
│   ├── plan.go          # Plan command (create tasks from beads)
│   ├── run.go           # Run command (execute pending tasks)
│   ├── proj.go          # Project management commands
│   ├── task.go          # Task management commands
│   ├── status.go        # Status command
│   ├── list.go          # List command
│   └── complete.go      # Complete command
└── internal/
    ├── beads/           # Beads client (bd CLI wrapper)
    ├── claude/          # Claude Code invocation
    ├── db/              # SQLite tracking database
    │   └── sqlc/        # Generated SQL queries
    ├── git/             # Git operations
    ├── github/          # PR creation/merging (gh CLI)
    ├── project/         # Project discovery and config
    ├── task/            # Task planning and complexity estimation
    └── worktree/        # Git worktree operations
```

## Development

### Run Tests

```bash
go test ./...
```

### Build

```bash
go build -o co .
```

### Generate SQL Queries

After modifying SQL files in `sql/queries/`:

```bash
mise run sqlc-generate
```

## Troubleshooting

### "not in a project directory"

All commands must be run from within a project. Create one first:
```bash
co proj create ~/myproject ~/path/to/repo
cd ~/myproject
```

### "bd: command not found"

Install beads:
```bash
curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
```

### "gh: command not found"

Install GitHub CLI:
- macOS: `brew install gh`
- Linux: See [cli.github.com/manual/installation](https://cli.github.com/manual/installation)

### "not logged into any GitHub hosts"

Authenticate with GitHub:
```bash
gh auth login
```

### No beads found

Ensure you have ready beads in your project's main repo:
```bash
cd ~/myproject/main
bd ready
```

If empty, create work items:
```bash
bd create --title "Your task" --type task
```

### "no work context found"

Tasks must be associated with a work. Create one first:
```bash
co work create feature/my-feature
cd w-abc  # Use the generated work ID
co plan
```

## License

MIT
