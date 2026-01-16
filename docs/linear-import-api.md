# Linear Import API Documentation

This document describes how to use the Linear import API from both CLI and TUI contexts.

## Overview

The Linear import integration is implemented in the `internal/linear` package and provides a programmatic API for importing Linear issues into beads. The integration preserves all Linear metadata including:

- Issue ID and URL (stored as external reference)
- Title, description, priority, status
- Assignee
- Labels
- Dependencies (optional)

## Package Structure

### `internal/linear/client.go`
MCP client for fetching data from Linear via the Linear MCP server.

### `internal/linear/models.go`
Data structures for Linear issues and import options.

### `internal/linear/mapper.go`
Mapping functions to convert Linear data to beads format:
- `MapStatus()` - Converts Linear state to beads status
- `MapPriority()` - Converts Linear priority (0-4) to beads P0-P4
- `MapType()` - Infers issue type from labels and title
- `MapIssueToBeadCreate()` - Main mapping function
- `FormatBeadDescription()` - Formats description with Linear metadata

### `internal/linear/fetcher.go`
Main orchestration logic:
- `Fetcher` - Main struct that coordinates fetching and importing
- `FetchAndImport()` - Import a single issue
- `FetchBatch()` - Import multiple issues
- Duplicate detection via external references
- Support for updating existing beads
- Dependency creation

### `internal/beads/client.go`
Extended with new functions for metadata:
- `AddLabels()` - Add labels to a bead
- `SetExternalRef()` - Set external reference
- Enhanced `UpdateOptions` with assignee, priority, status

## API Usage

### Basic Usage

```go
import (
    "context"
    "github.com/newhook/co/internal/linear"
)

func importLinearIssue() error {
    ctx := context.Background()

    // Create fetcher
    apiKey := os.Getenv("LINEAR_API_KEY")
    beadsDir := "." // or specify path to .beads directory

    fetcher, err := linear.NewFetcher(apiKey, beadsDir)
    if err != nil {
        return err
    }

    // Import a single issue
    result, err := fetcher.FetchAndImport(ctx, "ENG-123", nil)
    if err != nil {
        return err
    }

    if result.Success {
        fmt.Printf("Imported as bead %s\n", result.BeadID)
    }

    return nil
}
```

### Import with Options

```go
opts := &linear.ImportOptions{
    DryRun:         false,              // Set true to preview without creating
    UpdateExisting: true,               // Update if already imported
    CreateDeps:     true,               // Import blocking issues as dependencies
    MaxDepDepth:    2,                  // Maximum dependency depth
    StatusFilter:   "in_progress",      // Only import with this status
    PriorityFilter: "P1",               // Only import with this priority
    AssigneeFilter: "user@company.com", // Only import assigned to this user
}

result, err := fetcher.FetchAndImport(ctx, "ENG-123", opts)
```

### Batch Import

```go
issues := []string{"ENG-100", "ENG-101", "ENG-102"}

results, err := fetcher.FetchBatch(ctx, issues, opts)
if err != nil {
    return err
}

for _, result := range results {
    if result.Success {
        fmt.Printf("✓ %s -> %s\n", result.LinearID, result.BeadID)
    } else if result.Error != nil {
        fmt.Printf("✗ %s: %v\n", result.LinearID, result.Error)
    } else {
        fmt.Printf("○ %s: %s\n", result.LinearID, result.SkipReason)
    }
}
```

### Import by URL

```go
url := "https://linear.app/company/issue/ENG-123/feature-title"
result, err := fetcher.FetchAndImport(ctx, url, nil)
```

## CLI Usage

The `co linear import` command provides a command-line interface to the Linear import API:

```bash
# Import single issue
co linear import ENG-123

# Import multiple issues
co linear import ENG-123 ENG-124 ENG-125

# Import by URL
co linear import https://linear.app/company/issue/ENG-123/title

# Import with dependencies
co linear import ENG-123 --create-deps --max-dep-depth=2

# Update existing bead from Linear
co linear import ENG-123 --update

# Dry run (preview without creating)
co linear import ENG-123 --dry-run

# Import with filters
co linear import ENG-123 --status-filter=in_progress --priority-filter=P1

# Set API key via environment
export LINEAR_API_KEY=lin_api_...
co linear import ENG-123
```

## TUI Integration

To integrate Linear import into a TUI application:

### 1. Create Import Dialog

```go
import (
    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/newhook/co/internal/linear"
)

type LinearImportModel struct {
    input    textinput.Model
    fetcher  *linear.Fetcher
    result   *linear.ImportResult
    loading  bool
}

func (m LinearImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "enter" && !m.loading {
            issueID := m.input.Value()
            return m, m.startImport(issueID)
        }
    case importCompleteMsg:
        m.loading = false
        m.result = msg.result
        return m, nil
    }

    var cmd tea.Cmd
    m.input, cmd = m.input.Update(msg)
    return m, cmd
}

func (m LinearImportModel) startImport(issueID string) tea.Cmd {
    return func() tea.Msg {
        ctx := context.Background()
        result, err := m.fetcher.FetchAndImport(ctx, issueID, nil)
        if err != nil {
            result.Error = err
        }
        return importCompleteMsg{result: result}
    }
}

type importCompleteMsg struct {
    result *linear.ImportResult
}
```

### 2. Display Results

```go
func (m LinearImportModel) View() string {
    if m.loading {
        return "Importing from Linear...\n"
    }

    if m.result != nil {
        if m.result.Error != nil {
            return fmt.Sprintf("✗ Error: %v\n", m.result.Error)
        }
        if m.result.Success {
            return fmt.Sprintf("✓ Imported %s as bead %s\n",
                m.result.LinearID, m.result.BeadID)
        }
        if m.result.SkipReason != "" {
            return fmt.Sprintf("○ %s: %s\n",
                m.result.LinearID, m.result.SkipReason)
        }
    }

    return m.input.View()
}
```

### 3. Progress Indication for Batch Import

```go
type BatchImportModel struct {
    issues   []string
    results  []*linear.ImportResult
    current  int
    fetcher  *linear.Fetcher
}

func (m BatchImportModel) importNext() tea.Cmd {
    if m.current >= len(m.issues) {
        return nil // Done
    }

    issueID := m.issues[m.current]
    return func() tea.Msg {
        ctx := context.Background()
        result, err := m.fetcher.FetchAndImport(ctx, issueID, nil)
        if err != nil {
            result.Error = err
        }
        return importCompleteMsg{result: result}
    }
}

func (m BatchImportModel) View() string {
    if m.current >= len(m.issues) {
        return m.renderSummary()
    }

    progress := fmt.Sprintf("Importing... [%d/%d]", m.current+1, len(m.issues))
    return progress + "\n" + m.renderResults()
}

func (m BatchImportModel) renderResults() string {
    var b strings.Builder
    for _, result := range m.results {
        if result.Success {
            fmt.Fprintf(&b, "✓ %s -> %s\n", result.LinearID, result.BeadID)
        } else if result.Error != nil {
            fmt.Fprintf(&b, "✗ %s: %v\n", result.LinearID, result.Error)
        } else {
            fmt.Fprintf(&b, "○ %s: %s\n", result.LinearID, result.SkipReason)
        }
    }
    return b.String()
}
```

## Error Handling

The API provides comprehensive error handling:

### Import Errors

```go
result, err := fetcher.FetchAndImport(ctx, "ENG-123", nil)

// Check for API-level errors (network, auth, etc.)
if err != nil {
    // Handle critical errors
    log.Printf("Failed to import: %v", err)
    return err
}

// Check result for issue-specific errors
if result.Error != nil {
    // Handle per-issue errors (not found, invalid format, etc.)
    log.Printf("Issue import failed: %v", result.Error)
}

// Check if skipped
if result.SkipReason != "" {
    log.Printf("Skipped: %s", result.SkipReason)
}

// Success
if result.Success {
    log.Printf("Imported as %s", result.BeadID)
}
```

### Common Error Types

1. **Authentication Errors**: Invalid API key
2. **Not Found**: Linear issue doesn't exist or no access
3. **Network Errors**: Connection failures
4. **Validation Errors**: Invalid issue ID format
5. **Beads Errors**: Failed to create/update bead

## Metadata Preservation

The integration preserves all Linear metadata:

### External Reference
The Linear issue ID is stored as the bead's external reference:
```bash
bd show beads-abc --json | jq .external_ref
# Output: "ENG-123"
```

### Description
The description includes Linear metadata:
```markdown
[Original description...]

---
**Imported from Linear**
- ID: ENG-123
- URL: https://linear.app/company/issue/ENG-123/...
- State: In Progress (started)
- Project: Q1 Features
- Estimate: 3.0
- Assignee: John Doe
```

### Labels
Linear labels are preserved as bead labels:
```bash
bd show beads-abc --json | jq .labels
# Output: ["bug", "high-priority", "backend"]
```

### Assignee
Linear assignee is preserved:
```bash
bd show beads-abc --json | jq .assignee
# Output: "john@company.com"
```

## Testing

See `internal/linear/integration_test.go` for comprehensive examples of:
- Single issue import
- Batch import
- Import with options
- Update existing beads
- Error handling
- Dry run mode

Run tests:
```bash
# Unit tests (mocked)
go test ./internal/linear

# Integration tests (requires LINEAR_API_KEY)
LINEAR_API_KEY=lin_api_xxx go test -v ./internal/linear -run TestLinearImportIntegration
```

## Future Enhancements

Potential improvements:
1. **Bi-directional Sync**: Update Linear when beads change
2. **Real-time Sync**: Watch Linear for changes
3. **Webhook Integration**: Receive Linear webhooks
4. **Comment Sync**: Import and sync comments
5. **Attachment Sync**: Download files from Linear
6. **Epic Mapping**: Map Linear projects to beads epics
7. **Custom Field Mapping**: User-defined mapping rules

## See Also

- `docs/linear-import-interface-design.md` - Complete UI/UX design spec
- `docs/linear-mcp-research.md` - Linear MCP server research
- `cmd/linear.go` - CLI command implementation
- `internal/linear/integration_test.go` - API usage examples
