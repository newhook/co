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

### Process Ready Beads

```bash
cd ~/myproject
co run
```

This command:
1. Queries for ready beads from `main/.beads/`
2. For each bead, creates an isolated worktree
3. Invokes Claude Code to implement the changes
4. Closes the bead with an implementation summary
5. Creates and merges a pull request
6. Cleans up the worktree on success (keeps on failure for debugging)

### Process a Specific Bead

```bash
co run <bead-id>
```

Process only the specified bead instead of all ready beads.

### Command Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--branch` | `-b` | Target branch for PRs (default: `main`). When not `main`, uses feature branch workflow |
| `--limit` | `-n` | Maximum number of beads to process (0 = unlimited) |
| `--dry-run` | | Show plan without executing |
| `--no-merge` | | Create PRs but don't merge them |
| `--deps` | | Also process open dependencies of the specified bead (requires bead ID) |
| `--project` | | Specify project directory (default: auto-detect from cwd) |
| `--task` | | Use task-based mode (group beads by complexity) |
| `--budget` | | Complexity budget per task (1-100, default: 70, used with `--task`) |

### Processing Dependencies

When processing a specific bead, use `--deps` to also process its open dependencies first:

```bash
co run ac-xyz --deps
```

This recursively resolves dependencies and processes them in the correct order before processing the target bead.

### Feature Branch Workflow

When `--branch` specifies a branch other than `main`, Claude Orchestrator uses a feature branch workflow:

```bash
co run --branch feature/my-epic
```

1. Creates/switches to the feature branch in the main repo
2. Processes beads, with each PR targeting the feature branch
3. After all beads complete, creates a final PR from the feature branch to `main`
4. Merges the final PR

This is useful for grouping related work before merging to main.

### Task-Based Processing

Use `--task` mode to group beads by complexity, allowing Claude to process multiple related beads in a single session:

```bash
co run --task
```

This mode:
1. Estimates complexity for each bead using an LLM
2. Groups beads into tasks using bin-packing algorithm (respecting dependencies)
3. Processes each task in a single Claude session
4. Handles partial failures gracefully (creates partial PRs for completed work)

Control the grouping with `--budget`:
```bash
co run --task --budget 50  # Smaller tasks (fewer beads per task)
co run --task --budget 90  # Larger tasks (more beads per task)
```

The budget (1-100) represents target complexity per task. Lower values create more granular tasks, higher values group more beads together.

### Other Commands

| Command | Description |
|---------|-------------|
| `co status [bead-id]` | Show tracking status for beads |
| `co list [-s status]` | List tracked beads with optional status filter |
| `co complete <bead-id> [--pr URL]` | Mark a bead as completed (called by Claude) |

## How It Works

Claude Orchestrator orchestrates a complete development workflow with worktree isolation:

```
┌─────────────────────────────────────────────────────────────────┐
│                        co run                                   │
├─────────────────────────────────────────────────────────────────┤
│  1. Find project context (.co/ directory)                       │
│                                                                 │
│  2. Query ready beads from main/.beads/                         │
│     └─ bd ready --json                                          │
│                                                                 │
│  3. For each bead:                                              │
│     ├─ Create worktree                                          │
│     │  └─ git worktree add ../<bead-id> -b bead/<bead-id>       │
│     │                                                           │
│     ├─ Run Claude Code in zellij session (co-<project>)         │
│     │  └─ zellij run -- claude --dangerously-skip-permissions   │
│     │     Claude receives prompt and autonomously:              │
│     │     • Implements the changes                              │
│     │     • Commits to branch                                   │
│     │     • Pushes and creates PR (gh pr create)                │
│     │     • Closes bead (bd close <id> --reason "...")          │
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
│   ├── run.go           # Run command
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
