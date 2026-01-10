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

## Installation

### Build from Source

```bash
git clone https://github.com/newhook/autoclaude.git
cd autoclaude
go build -o co .
```

Move the binary to your PATH:

```bash
mv co /usr/local/bin/
```

### Verify Installation

```bash
co --help
```

## Configuration

Claude Orchestrator requires minimal configuration. It relies on:

- **System PATH** to locate required CLI tools
- **Git configuration** for repository operations (user.name, user.email)
- **GitHub CLI authentication** (`gh auth login`)
- **Beads initialization** in your project (`.beads/` directory)

### Project Setup

1. Initialize beads in your project:
   ```bash
   bd init
   ```

2. Ensure GitHub CLI is authenticated:
   ```bash
   gh auth status
   ```

3. Run Claude Orchestrator from your project root:
   ```bash
   co run
   ```

## Usage

### Process Ready Beads

```bash
co run
```

This command:
1. Queries for ready beads (`bd ready --json`)
2. For each bead, invokes Claude Code to implement the changes
3. Closes the bead with an implementation summary
4. Creates and merges a pull request

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

1. Creates/switches to the feature branch
2. Processes beads, with each PR targeting the feature branch
3. After all beads complete, creates a final PR from the feature branch to `main`
4. Merges the final PR

This is useful for grouping related work before merging to main.

## How It Works

Claude Orchestrator orchestrates a complete development workflow:

```
┌─────────────────────────────────────────────────────────────────┐
│                        co run                                   │
├─────────────────────────────────────────────────────────────────┤
│  1. Query ready beads                                           │
│     └─ bd ready --json                                          │
│                                                                 │
│  2. For each bead:                                              │
│     ├─ Invoke Claude Code with bead description                 │
│     │  └─ claude --dangerously-skip-permissions -p "<prompt>"   │
│     │     • Creates feature branch                              │
│     │     • Implements changes                                  │
│     │     • Commits to branch                                   │
│     │                                                           │
│     ├─ Close bead (while context is fresh)                      │
│     │  └─ bd close <id> --reason "<summary>"                    │
│     │                                                           │
│     ├─ Create pull request                                      │
│     │  └─ gh pr create --head <branch> --base main ...          │
│     │                                                           │
│     └─ Merge pull request                                       │
│        └─ gh pr merge <url> --merge --delete-branch             │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

- **Beads closed before PR merge**: This preserves implementation context for accurate close reasons
- **Claude handles branching**: Branch creation and commits are delegated to Claude Code
- **Streaming output**: Claude Code output streams to stdout/stderr for visibility

## Project Structure

```
co/
├── main.go              # CLI entry point
├── cmd/
│   ├── root.go          # Root command
│   └── run.go           # Run command
└── internal/
    ├── beads/           # Beads client (bd CLI wrapper)
    ├── claude/          # Claude Code invocation
    ├── github/          # PR creation/merging (gh CLI)
    └── git/             # Git operations
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

Ensure you have ready beads in your project:
```bash
bd ready
```

If empty, create work items:
```bash
bd create --title "Your task" --type task
```

## License

MIT
