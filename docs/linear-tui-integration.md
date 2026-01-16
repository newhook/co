# Linear TUI Integration Status

## Current Status: ✅ Implemented

The Linear import feature is now available **both via CLI and TUI**:
- **CLI**: `co linear import` command
- **TUI**: Interactive dialogs accessible via `i` and `I` hotkeys in Plan Mode

## Implementation Details

The TUI integration has been fully implemented with all planned features. This document describes the implementation and usage.

### Implemented Features

#### 1. Quick Import Hotkey

A hotkey in Plan Mode triggers Linear import:

- **Hotkey**: `i` (for "Import from Linear")
- **Context**: Available from the Plan Mode beads list view
- **Action**: Opens a text input dialog for entering Linear issue ID or URL

#### 2. Import Dialog

The import dialog provides:

- **Text Input**: Enter Linear issue ID (e.g., `ENG-123`) or URL
- **Options Panel**:
  - `[x] Create dependencies` - Import blocking issues as dependencies (toggle with space)
  - `[x] Update existing` - Update bead if already imported (toggle with space)
  - `[x] Dry run` - Preview without creating (toggle with space)
  - `Max depth: 2` - Maximum dependency depth (adjust with +/- keys)
- **Buttons**:
  - `[Import]` - Execute the import
  - `[Cancel]` - Close dialog without importing
- **Navigation**: Tab/Shift+Tab cycles between fields
- **Submit**: Enter key from input field or Import button

#### 3. Batch Import Support

For importing multiple issues:

- **Hotkey**: `I` (Shift+i for batch import)
- **Multi-line Input**: Enter one issue ID/URL per line in a textarea
- **Options Panel**: Same import options as single import (toggle with 1/2/3 keys)
- **Results Summary**: Status messages show success/error counts after import completes

#### 4. Import Status Indicators

Import status is shown in status messages:

- **Success**: "Successfully imported <bead-id>" for single imports
- **Success (batch)**: "Successfully imported N issues" for batch imports
- **Error**: Error messages displayed in red with details

Note: Beads imported from Linear will have Linear metadata stored by the beads system.

#### 5. Help Integration

The Linear import commands are documented in the TUI help screen:

- Press `?` in Plan Mode to see help
- Help lists both `i` (single import) and `I` (batch import) commands
- Located in the "Issue Management" section of the help screen

## Implementation Architecture

The implementation follows the established TUI patterns in the `co` codebase:

### File Organization

**View Modes** (`cmd/tui_shared.go`):
- `ViewLinearImport` - Single issue import dialog
- `ViewLinearBatchImport` - Batch import dialog

**State Management** (`cmd/tui_plan.go`):
- `linearImportInput` - Text input for issue ID/URL
- `linearImportCreateDeps`, `linearImportUpdate`, `linearImportDryRun` - Import options
- `linearImportMaxDepth` - Dependency depth setting
- `linearImportFocus` - Currently focused field
- `linearImporting` - Import in progress flag
- `linearBatchInput` - Textarea for batch import

**Message Types** (`cmd/tui_plan.go`):
- `linearImportCompleteMsg` - Sent when import completes (success or error)
- `linearImportProgressMsg` - Sent during batch import progress (future enhancement)

**Dialog Handlers** (`cmd/tui_plan_dialogs.go`):
- `updateLinearImport()` - Handles keyboard input for single import dialog
- `updateLinearBatchImport()` - Handles keyboard input for batch import dialog
- `renderLinearImportDialogContent()` - Renders single import dialog UI
- `renderLinearBatchImportDialogContent()` - Renders batch import dialog UI

**Import Execution** (`cmd/tui_plan_data.go`):
- `importLinearIssue()` - Executes single issue import using `linear.Fetcher`
- `importLinearBatch()` - Executes batch import using `linear.Fetcher.FetchBatch()`

**Key Design Decisions:**

1. **Bubbles Integration**: Uses `bubbles/textinput` for single-line input and `bubbles/textarea` for multi-line batch import
2. **Async Execution**: Import operations run asynchronously using Bubble Tea commands, preventing UI freezing
3. **Direct Fetcher Use**: Calls `internal/linear.Fetcher` directly rather than shelling out to CLI commands
4. **Error Handling**: Import errors are displayed in the status bar and can show partial success for batch imports

## Usage

### Environment Setup

Set the Linear API key environment variable:

```bash
export LINEAR_API_KEY=lin_api_...
```

Then launch the TUI in Plan Mode (Ctrl+P):

```bash
co tui
# Press Ctrl+P to switch to Plan Mode
```

### Single Issue Import

1. Press `i` to open the import dialog
2. Enter a Linear issue ID (e.g., `ENG-123`) or full URL
3. (Optional) Tab through fields to configure options:
   - Space to toggle "Create dependencies"
   - Space to toggle "Update existing"
   - Space to toggle "Dry run"
   - +/- to adjust max dependency depth
4. Press Enter to import or Esc to cancel
5. Import result will appear in the status bar

### Batch Import

1. Press `I` (Shift+i) to open batch import dialog
2. Enter one Linear issue ID or URL per line
3. (Optional) Tab to options and press 1/2/3 to toggle settings
4. Press Ctrl+Enter (from textarea) or Enter (from buttons) to import
5. Results summary will appear in status bar

## Configuration

The TUI uses the same configuration as the CLI command. Required environment variable:

```bash
LINEAR_API_KEY=lin_api_...  # Required for Linear API access
```

Optional beads directory (auto-detected by default):

```bash
BEADS_DIR=/path/to/.beads
```

## Testing

### Manual Testing Checklist

- [x] Open import dialog with `i` hotkey
- [x] Enter valid Linear issue ID
- [x] Verify import success with status message
- [x] Test invalid issue ID error handling
- [x] Test network error handling (LINEAR_API_KEY not set)
- [x] Test batch import with multiple issues
- [x] Test dry run mode
- [x] Test dependency creation with --create-deps option
- [x] Test Tab/Shift+Tab navigation between fields
- [x] Test dialog cancellation with Esc
- [x] Verify help screen documents `i` and `I` hotkeys

### Automated Tests

Future enhancement: Add TUI interaction tests for import dialogs.

## See Also

- `docs/linear-import-api.md` - API documentation and code examples
- `docs/linear-import-interface-design.md` - Original design specification
- `cmd/linear.go` - CLI implementation (can be used as reference)
- `internal/linear/` - Core import logic used by both CLI and TUI
