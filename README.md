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

Project creation also:
- Initializes beads: `bd init` and `bd hooks install`
- If mise enabled (`.mise.toml` or `.tool-versions`): runs `mise trust`, `mise install`
- If mise `setup` task defined: runs `mise run setup` (use for npm/pnpm install)

### Project Commands

| Command | Description |
|---------|-------------|
| `co proj create <dir> <repo>` | Create a new project (local path or GitHub URL) |
| `co proj destroy [--force]` | Remove project and all worktrees |
| `co proj status` | Show project info, worktrees, and task status |

### Project Configuration

The `.co/config.toml` file stores project settings:

```toml
[project]
  name = "my-project"
  created_at = 2024-01-15T10:00:00-05:00

[repo]
  type = "github"  # or "local"
  source = "https://github.com/user/repo"
  path = "main"

[hooks]
  # Environment variables set when spawning Claude
  # Supports variable expansion (e.g., $PATH)
  env = [
    "CLAUDE_CODE_USE_VERTEX=1",
    "CLOUD_ML_REGION=us-east5",
    "MY_VAR=value"
  ]
```

The `hooks.env` setting is useful for:
- Configuring Claude Code to use Vertex AI
- Setting custom PATH for tools
- Any environment variables Claude needs

## Usage

All commands must be run from within a project directory (or subdirectory).

### Quick Start

```bash
# Create a project
co proj create ~/myproject https://github.com/user/repo
cd ~/myproject

# Create a work unit with beads (generates branch from bead titles)
co work create bead-1,bead-2 bead-3
# → Creates w-8xa/tree/ worktree with beads grouped into tasks

# Execute work
cd w-8xa
co run                  # Execute tasks sequentially
```

Claude Orchestrator uses a two-phase workflow: **work** → **run**.

### Phase 1: Work - Create a Work Unit

```bash
co work create bead-1,bead-2 bead-3     # Create work with beads
```

This creates:
- A work directory (`w-abc/`)
- A git worktree with a generated branch (`w-abc/tree/`)
- Tasks from the bead groupings (comma-separated beads go in one task)
- A unique work ID using content-based hashing

**Bead grouping syntax:**
- `bead-1,bead-2` - beads separated by commas are grouped into one task
- Space-separated arguments create separate tasks
- Example: `co work create a,b c,d e` creates 3 tasks: [a,b], [c,d], [e]

The branch name is generated from bead titles and you're prompted for confirmation.

### Phase 2: Run - Execute Pending Tasks

```bash
co run                               # Execute all pending tasks in current work
co run --work w-abc                  # Execute all tasks in work w-abc
```

Tasks within a work are executed sequentially in the work's worktree.

**Run command options:**
- `--plan`: Use LLM complexity estimation to auto-group beads into tasks
- `--auto`: Full automated workflow (implement, review/fix loop, PR creation)

### Work Commands

| Command | Description |
|---------|-------------|
| `co work create <bead-args...>` | Create a new work unit with beads (generates branch from titles) |
| `co work add <bead-args...>` | Add beads to existing work |
| `co work remove <bead-ids...>` | Remove beads from existing work |
| `co work list` | List all work units with their status |
| `co work show [<id>]` | Show detailed work information (current directory or specified) |
| `co work pr [<id>]` | Create a PR task for Claude to generate pull request |
| `co work review [<id>]` | Create a review task to examine code changes |
| `co work destroy <id>` | Delete work unit and all associated data |

### Work Create Options

| Flag | Description |
|------|-------------|
| `--base` | Specify base branch (default: main) |
| `--auto` | Full automated workflow (implement, review/fix loop, PR) |

### Run Command Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--limit` | `-n` | Maximum number of tasks to process (0 = unlimited) |
| `--dry-run` | | Show execution plan without running |
| `--plan` | | Use LLM complexity estimation to auto-group beads |
| `--auto` | | Full automated workflow (implement, review/fix loop, PR) |
| `--project` | | Specify project directory (default: auto-detect from cwd) |
| `--work` | | Specify work ID (default: auto-detect from current directory) |

### Typical Workflow Example

```bash
# 1. Create a work unit with beads
co work create bead-1 bead-2,bead-3
# Output: Generated work ID: w-8xa (from branch: feat/implement-feature-x)

# 2. Navigate to the work directory
cd w-8xa

# 3. Execute tasks
co run

# 4. Create PR (Claude generates comprehensive description)
co work pr
co run                  # Execute the PR task

# 5. Review and merge PR manually
gh pr merge --squash

# 6. Clean up
co work destroy w-8xa
```

Each work has its own feature branch, and all tasks within the work are executed sequentially in the work's worktree.

### Automated Workflow

Use `--auto` for a fully automated workflow:

```bash
co work create bead-1 bead-2 --auto
```

This mode:
1. Creates work unit and tasks from beads
2. Executes all implementation tasks
3. Runs review/fix loop until code is clean (max 3 iterations)
4. Creates PR automatically
5. Returns PR URL when complete

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
co task set-review-epic <epic>  # Associate review epic with review task
```

#### Error Handling and Retries

When a task fails:
- The task is automatically marked as failed in the database
- Claude can signal failure using `co complete <task-id> --error "message"`
- To retry a failed task:
  ```bash
  co task reset <task-id>    # Reset task status to pending
  co run                     # Retry the task
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

### Monitoring Commands

| Command | Description |
|---------|-------------|
| `co tui` | Interactive TUI for managing works and beads (lazygit-style) |
| `co poll [work-id\|task-id]` | Monitor work/task progress with text output |

The TUI (`co tui`) provides a full management interface with:
- Three-panel drill-down: Beads → Works → Tasks
- Create/destroy works, run tasks
- Bead filtering (ready/open/closed), search, multi-select
- Keyboard shortcuts for all operations (press `?` for help)

The poll command (`co poll`) provides simple text-based monitoring:
- Use `--interval` to set polling interval (default: 2s)
- Useful for scripting or when you don't need interactive features

### Other Commands

| Command | Description |
|---------|-------------|
| `co status [bead-id]` | Show tracking status for beads |
| `co list [-s status]` | List tracked beads with optional status filter |
| `co sync` | Pull from upstream in all repositories |
| `co complete <bead-id> [--pr URL] [--error "message"]` | Mark a bead/task as completed or failed (called by Claude) |
| `co estimate <bead-id> --score N --tokens N` | Report complexity estimate for a bead (called by Claude during estimation) |

## How It Works

Claude Orchestrator uses a two-phase workflow with the Work → Tasks → Beads hierarchy:

```
┌─────────────────────────────────────────────────────────────────┐
│                      co work create                             │
├─────────────────────────────────────────────────────────────────┤
│  1. Parse bead arguments and groupings                          │
│  2. Generate branch name from bead titles (prompt for confirm)  │
│  3. Generate unique work ID using content-based hashing         │
│  4. Create work directory: <project>/<work-id>/                 │
│  5. Create git worktree: <work-id>/tree/                        │
│     └─ git worktree add tree -b <branch-name>                   │
│  6. Create tasks from bead groupings                            │
│  7. Create zellij tab for the work                              │
│  8. Expand epics to include all child beads                     │
│  9. Push branch and set upstream                                │
│  10. Store work in tracking database                            │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        co run                                   │
├─────────────────────────────────────────────────────────────────┤
│  1. Load work and its pending tasks from database               │
│  2. (Optional with --plan) Estimate complexity and regroup      │
│  3. Execute tasks sequentially in the work's worktree:          │
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
│  4. After all tasks complete:                                   │
│     ├─ Mark work as completed                                   │
│     └─ (With --auto) Run review/fix loop, then create PR        │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      co work pr                                 │
├─────────────────────────────────────────────────────────────────┤
│  1. Check work is completed and no PR exists                    │
│  2. Create special PR task (w-abc.pr)                           │
│  3. User runs: co run                                           │
│  4. Claude analyzes all changes and completed work:             │
│     ├─ Reviews git log and diff                                 │
│     ├─ Summarizes completed tasks and beads                     │
│     ├─ Generates comprehensive PR description                   │
│     ├─ Creates PR using gh pr create                            │
│     └─ Returns PR URL (does NOT auto-merge)                     │
│  5. User reviews and merges PR manually                         │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      co work review                             │
├─────────────────────────────────────────────────────────────────┤
│  1. Create review task (w-abc.review-1, w-abc.review-2, etc.)   │
│  2. Claude examines the work's branch for:                      │
│     ├─ Code quality issues                                      │
│     ├─ Security vulnerabilities                                 │
│     └─ Potential bugs                                           │
│  3. Creates beads for issues found (attached to review epic)    │
│  4. (With --auto) Loops: review → fix → review until clean      │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

- **Three-tier hierarchy**: Work → Tasks → Beads provides clear organization
- **Work-based isolation**: Each work has its own worktree and feature branch
- **Content-based IDs**: Works use hash-based IDs (w-abc), tasks use hierarchical IDs (w-abc.1)
- **Two-phase workflow**: Work creation and execution are separate phases
- **Sequential task execution**: Tasks within a work run sequentially in the same worktree
- **Project isolation**: Each project has its own tracking database in `.co/`
- **Claude handles implementation**: Claude autonomously implements, commits, and closes beads
- **Zellij for terminal management**: Each work gets its own tab in the project's zellij session
- **Database polling**: Manager polls SQLite database for completion signals from Claude
- **Fail-fast for uncommitted changes**: Tasks verify clean working tree and handle partial work appropriately
- **Continuous integration**: Each bead completion is committed and pushed immediately to prevent work loss
- **Intelligent retries**: Failed tasks can be reset and retried, with Claude skipping already-completed beads
- **Error signaling**: Claude can explicitly mark tasks as failed with error messages for better debugging
- **Automated reviews**: Review tasks can find issues and create fix beads automatically

## Project Structure

```
co/
├── main.go              # CLI entry point
├── cmd/
│   ├── root.go          # Root command
│   ├── complete.go      # Complete command (mark beads/tasks as done)
│   ├── estimate.go      # Estimate command (report complexity)
│   ├── list.go          # List command (list tracked beads)
│   ├── migrate.go       # Database migration command
│   ├── orchestrate.go   # Orchestrate command (internal, execute tasks)
│   ├── poll.go          # Text-based progress monitoring
│   ├── proj.go          # Project management commands
│   ├── run.go           # Run command (execute pending tasks)
│   ├── status.go        # Status command
│   ├── sync.go          # Sync command (pull from upstream)
│   ├── task.go          # Task management commands
│   ├── task_processing.go # Task processing helpers
│   ├── tui.go           # Interactive TUI (lazygit-style)
│   ├── work.go          # Work management commands
│   └── work_automated.go # Automated bead-to-PR workflow
└── internal/
    ├── beads/           # Beads client (bd CLI wrapper)
    ├── claude/          # Claude Code invocation
    ├── db/              # SQLite tracking database
    │   ├── migrations/  # Schema migrations
    │   ├── queries/     # SQL query definitions
    │   └── sqlc/        # Generated SQL queries
    ├── git/             # Git operations
    ├── mise/            # Mise tool management
    ├── project/         # Project discovery and config
    ├── signal/          # Signal handling
    ├── task/            # Task planning and complexity estimation
    ├── worktree/        # Git worktree operations
    └── zellij/          # Zellij terminal multiplexer integration
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

After modifying SQL files in `internal/db/queries/`:

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

You need to be in a work directory or specify the work ID:
```bash
co work create bead-1 bead-2
cd w-abc  # Use the generated work ID
co run
```

## License

MIT
