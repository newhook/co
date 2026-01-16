# Linear TUI Integration Status

## Current Status: Not Implemented

The Linear import feature is currently available **only via CLI** (`co linear import`). There is no TUI integration at this time.

## Planned TUI Integration

The TUI integration for Linear import is planned as a future enhancement. This document outlines the intended design and implementation approach.

### Proposed Features

#### 1. Quick Import Hotkey

Add a global hotkey in the TUI to trigger Linear import:

- **Hotkey**: `i` or `I` (for "Import from Linear")
- **Context**: Available from the main beads list view
- **Action**: Opens a text input dialog for entering Linear issue ID or URL

#### 2. Import Dialog

The import dialog should provide:

- **Text Input**: Enter Linear issue ID (e.g., `ENG-123`) or URL
- **Options Panel**:
  - `[ ] Create dependencies` - Import blocking issues as dependencies
  - `[ ] Update existing` - Update bead if already imported
  - `[ ] Dry run` - Preview without creating
  - `Max depth: [1]` - Maximum dependency depth
- **Buttons**:
  - `[Import]` - Execute the import
  - `[Cancel]` - Close dialog without importing

#### 3. Batch Import Support

For importing multiple issues:

- **Hotkey**: `Shift+I` (for batch import)
- **Multi-line Input**: Enter one issue ID/URL per line
- **Progress Display**: Show import progress (e.g., "Importing... [3/10]")
- **Results Summary**: Display success/skip/error counts

#### 4. Import Status Indicators

In the main beads list view:

- **External Reference Badge**: Show Linear icon/badge for beads imported from Linear
- **Metadata Display**: Show Linear issue ID in bead details (e.g., "External: ENG-123")
- **URL Link**: Make Linear URL clickable/copyable from bead details view

#### 5. Workflow Integration

Integrate with existing TUI workflows:

- **Work Creation**: When creating work, suggest importing related Linear issues
- **Bead Details**: From bead details view, allow "Update from Linear" action
- **Search Integration**: Search for beads by Linear issue ID

## Implementation Guide

### Step 1: Add Import Dialog Component

Create a new TUI component for the import dialog:

```go
// File: cmd/tui_linear_import.go

type linearImportDialog struct {
    input         textinput.Model
    createDeps    bool
    updateExist   bool
    dryRun        bool
    maxDepth      int
    fetcher       *linear.Fetcher
    importing     bool
    result        *linear.ImportResult
    err           error
}

func newLinearImportDialog() linearImportDialog {
    input := textinput.New()
    input.Placeholder = "Enter Linear issue ID or URL (e.g., ENG-123)"
    input.Focus()
    input.Width = 60

    return linearImportDialog{
        input:    input,
        maxDepth: 1,
    }
}

func (d linearImportDialog) Update(msg tea.Msg) (linearImportDialog, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "enter":
            if !d.importing {
                return d, d.startImport()
            }
        case "esc":
            return d, closeDialogCmd
        case "tab":
            // Cycle through options
            return d, nil
        }
    case importCompleteMsg:
        d.importing = false
        d.result = msg.result
        d.err = msg.err
        return d, nil
    }

    var cmd tea.Cmd
    d.input, cmd = d.input.Update(msg)
    return d, cmd
}

func (d linearImportDialog) View() string {
    if d.importing {
        return lipgloss.NewStyle().
            Padding(1, 2).
            Render("⏳ Importing from Linear...")
    }

    if d.result != nil || d.err != nil {
        return d.renderResult()
    }

    return d.renderInput()
}

func (d linearImportDialog) startImport() tea.Cmd {
    d.importing = true
    issueID := d.input.Value()

    return func() tea.Msg {
        ctx := context.Background()
        opts := &linear.ImportOptions{
            CreateDeps:     d.createDeps,
            UpdateExisting: d.updateExist,
            DryRun:         d.dryRun,
            MaxDepDepth:    d.maxDepth,
        }

        result, err := d.fetcher.FetchAndImport(ctx, issueID, opts)
        return importCompleteMsg{result: result, err: err}
    }
}

type importCompleteMsg struct {
    result *linear.ImportResult
    err    error
}
```

### Step 2: Integrate with Main TUI

Modify the main TUI model to include the import dialog:

```go
// File: cmd/tui_root.go

type model struct {
    // ... existing fields ...
    linearImportDialog *linearImportDialog
    showLinearImport   bool
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle import dialog if visible
    if m.showLinearImport && m.linearImportDialog != nil {
        var cmd tea.Cmd
        *m.linearImportDialog, cmd = m.linearImportDialog.Update(msg)
        return m, cmd
    }

    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "i", "I":
            // Open Linear import dialog
            m.linearImportDialog = newLinearImportDialog()
            m.showLinearImport = true
            return m, nil
        // ... existing key handlers ...
        }
    }

    // ... existing update logic ...
}
```

### Step 3: Add Visual Indicators

Update the beads list view to show Linear metadata:

```go
// File: cmd/tui_shared.go

func renderBeadRow(bead Bead) string {
    var badges []string

    // Add Linear badge if imported from Linear
    if bead.ExternalRef != "" && isLinearID(bead.ExternalRef) {
        badges = append(badges, "🔗 Linear")
    }

    // ... existing badge logic ...

    return lipgloss.JoinHorizontal(
        lipgloss.Left,
        bead.ID,
        " ",
        strings.Join(badges, " "),
        " ",
        bead.Title,
    )
}

func isLinearID(ref string) bool {
    // Check if external ref matches Linear ID format (e.g., "ENG-123")
    matched, _ := regexp.MatchString(`^[A-Z]+-\d+$`, ref)
    return matched
}
```

### Step 4: Add Batch Import Support

Create a batch import dialog:

```go
// File: cmd/tui_linear_batch_import.go

type linearBatchImportDialog struct {
    textarea     textarea.Model
    fetcher      *linear.Fetcher
    issues       []string
    results      []*linear.ImportResult
    currentIndex int
    importing    bool
}

func (d linearBatchImportDialog) startBatchImport() tea.Cmd {
    // Parse input into issue IDs
    d.issues = strings.Split(d.textarea.Value(), "\n")
    d.currentIndex = 0
    d.importing = true

    return d.importNext()
}

func (d linearBatchImportDialog) importNext() tea.Cmd {
    if d.currentIndex >= len(d.issues) {
        return nil // Done
    }

    issueID := strings.TrimSpace(d.issues[d.currentIndex])
    if issueID == "" {
        d.currentIndex++
        return d.importNext()
    }

    return func() tea.Msg {
        ctx := context.Background()
        result, err := d.fetcher.FetchAndImport(ctx, issueID, nil)
        if err != nil {
            result.Error = err
        }
        return batchImportProgressMsg{result: result}
    }
}

type batchImportProgressMsg struct {
    result *linear.ImportResult
}
```

## Configuration

### Environment Setup

The TUI will require the `LINEAR_API_KEY` environment variable to be set:

```bash
export LINEAR_API_KEY=lin_api_...
co tui
```

### User Preferences

Future enhancement: Allow users to configure default import options in `~/.co/config.toml`:

```toml
[linear]
api_key = "lin_api_..."  # Optional: can also use env var
create_deps_default = true
max_dep_depth = 2
```

## Testing

### Manual Testing Checklist

- [ ] Open import dialog with `i` hotkey
- [ ] Enter valid Linear issue ID
- [ ] Verify import success with visual feedback
- [ ] Test invalid issue ID error handling
- [ ] Test network error handling
- [ ] Test batch import with multiple issues
- [ ] Verify Linear badge appears in beads list
- [ ] Test "Update from Linear" action
- [ ] Test dry run mode
- [ ] Test dependency creation

### Automated Tests

Add TUI interaction tests:

```go
// File: cmd/tui_linear_test.go

func TestLinearImportDialogKeyHandling(t *testing.T) {
    dialog := newLinearImportDialog()

    // Test 'esc' closes dialog
    // Test 'enter' triggers import
    // Test input field updates
}

func TestLinearBadgeRendering(t *testing.T) {
    // Test that beads with Linear external refs show badge
}
```

## Documentation for Users

When TUI integration is implemented, update the help screen:

```
Hotkeys:
  i       Import from Linear (single issue)
  I       Batch import from Linear (multiple issues)
  u       Update current bead from Linear (when viewing bead details)
  ?       Show help
```

## See Also

- `docs/linear-import-api.md` - API documentation and code examples
- `docs/linear-import-interface-design.md` - Original design specification
- `cmd/linear.go` - CLI implementation (can be used as reference)
- `internal/linear/` - Core import logic used by both CLI and TUI
