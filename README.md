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

Claude Orchestrator uses a project-based model with worktree isolation. Each project has its own directory structure:

```
<project-dir>/
├── .co/
│   ├── config.toml      # Project configuration
│   └── tracking.db      # SQLite coordination database
├── main/                # Symlink to local repo OR clone from GitHub
│   └── .beads/          # Beads issue tracker (in repo)
├── bead-123/            # Worktree for task bead-123
├── bead-456/            # Worktree for task bead-456
└── ...
```

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

Claude Orchestrator uses a two-phase workflow: **plan** then **run**.

### Phase 1: Plan - Create Tasks from Beads

```bash
co plan                              # One task per ready bead (default)
co plan --auto-group                 # LLM groups beads by complexity
co plan bead-1,bead-2 bead-3         # Manual: task1=[bead-1,bead-2], task2=[bead-3]
```

The plan command creates tasks in the database for later execution.

**Manual grouping syntax:**
- `bead-1,bead-2` - beads separated by commas are grouped into one task
- Space-separated arguments create separate tasks
- Example: `co plan a,b c,d e` creates 3 tasks: [a,b], [c,d], [e]

### Phase 2: Run - Execute Pending Tasks

```bash
co run                               # Execute all pending tasks
co run task-id                       # Execute specific task
```

Tasks are executed in dependency order (derived from bead dependencies).

### Plan Command Flags

| Flag | Description |
|------|-------------|
| `--auto-group` | Automatically group beads by complexity using LLM estimation |
| `--budget` | Complexity budget per task (1-100, default: 70, used with `--auto-group`) |
| `--project` | Specify project directory (default: auto-detect from cwd) |

### Run Command Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--branch` | `-b` | Target branch for PRs (default: `main`). When not `main`, uses feature branch workflow |
| `--limit` | `-n` | Maximum number of tasks to process (0 = unlimited) |
| `--dry-run` | | Show execution plan without running |
| `--no-merge` | | Create PRs but don't merge them |
| `--project` | | Specify project directory (default: auto-detect from cwd) |

### Feature Branch Workflow

When `--branch` specifies a branch other than `main`, Claude Orchestrator uses a feature branch workflow:

```bash
co plan
co run --branch feature/my-epic
```

1. Creates/switches to the feature branch in the main repo
2. Executes tasks, with each PR targeting the feature branch
3. After all tasks complete, creates a final PR from the feature branch to `main`
4. Merges the final PR

This is useful for grouping related work before merging to main.

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

### Other Commands

| Command | Description |
|---------|-------------|
| `co status [bead-id]` | Show tracking status for beads |
| `co list [-s status]` | List tracked beads with optional status filter |
| `co complete <bead-id> [--pr URL]` | Mark a bead as completed (called by Claude) |

## How It Works

Claude Orchestrator uses a two-phase workflow with worktree isolation:

```
┌─────────────────────────────────────────────────────────────────┐
│                        co plan                                  │
├─────────────────────────────────────────────────────────────────┤
│  1. Query ready beads from main/.beads/ (bd ready --json)       │
│  2. Create tasks from beads:                                    │
│     • Default: one task per bead                                │
│     • --auto-group: LLM estimates complexity, bin-packs         │
│     • Manual: user specifies groupings                          │
│  3. Store tasks in tracking database                            │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        co run                                   │
├─────────────────────────────────────────────────────────────────┤
│  1. Load pending tasks from database                            │
│  2. Derive task dependencies from bead dependencies             │
│  3. Sort tasks in dependency order (topological sort)           │
│  4. For each task:                                              │
│     ├─ Create worktree                                          │
│     │  └─ git worktree add ../<task-id> -b task/<task-id>       │
│     │                                                           │
│     ├─ Run Claude Code in zellij session (co-<project>)         │
│     │  └─ zellij run -- claude --dangerously-skip-permissions   │
│     │     Claude receives prompt and autonomously:              │
│     │     • Implements all beads in the task                    │
│     │     • Commits to branch                                   │
│     │     • Pushes and creates PR (gh pr create)                │
│     │     • Closes beads (bd close <id> --reason "...")         │
│     │     • Merges PR (gh pr merge --squash --delete-branch)    │
│     │     • Signals completion (co complete <id>)               │
│     │                                                           │
│     ├─ Manager polls database for completion                    │
│     │                                                           │
│     └─ On success: remove worktree                              │
│        On failure: keep worktree for debugging                  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

- **Two-phase workflow**: Planning (co plan) and execution (co run) are separate
- **Task abstraction**: Beads are grouped into tasks; tasks are the unit of execution
- **Dependency derivation**: Task dependencies derived at runtime from bead dependencies
- **Project isolation**: Each project has its own tracking database in `.co/`
- **Worktree isolation**: Each task runs in its own git worktree, preventing conflicts
- **Claude handles full workflow**: Implementation, PR creation, bead closing, and merging are all done by Claude
- **Zellij for terminal management**: Each Claude instance runs in a project-specific zellij session
- **Database polling**: Manager polls SQLite database for completion signals from Claude

## Project Structure

```
co/
├── main.go              # CLI entry point
├── cmd/
│   ├── root.go          # Root command
│   ├── plan.go          # Plan command (create tasks from beads)
│   ├── run.go           # Run command (execute pending tasks)
│   ├── proj.go          # Project management commands
│   ├── status.go        # Status command
│   ├── list.go          # List command
│   └── complete.go      # Complete command
└── internal/
    ├── beads/           # Beads client (bd CLI wrapper)
    ├── claude/          # Claude Code invocation
    ├── db/              # SQLite tracking database
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

## License

MIT
