# Auto Claude (ac)

Go CLI tool that orchestrates Claude Code to process beads, creating PRs for each.

## Build & Test

```bash
go build -o ac .
go test ./...
```

## Project Structure

- `main.go` - CLI entry point (cobra)
- `cmd/run.go` - Main run command
- `internal/beads/` - Beads database client (bd CLI wrapper)
- `internal/claude/` - Claude Code invocation
- `internal/github/` - PR creation and merging (gh CLI)

## External Dependencies

Uses CLI tools: `bd`, `claude`, `gh`, `git`

## Workflow

1. Query ready beads via `bd ready --json`
2. For each bead:
   - Claude Code creates branch, implements changes, and commits
   - Manager creates PR and merges it
3. Close bead with `bd close <id> --reason "..."`

Note: Branch creation and commits are handled by Claude Code, not the manager.
