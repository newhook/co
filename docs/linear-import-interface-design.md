# Linear Import Interface Design

## Overview

This document specifies the user-facing interfaces for importing Linear issues into beads, including CLI commands and TUI integration.

## CLI Command Interface

### Basic Command Syntax

```bash
# Import single issue by identifier
bd import linear ENG-123

# Import single issue by URL
bd import linear https://linear.app/company/issue/ENG-123/issue-title

# Import multiple issues (batch mode)
bd import linear ENG-123 ENG-124 ENG-125
bd import linear https://linear.app/company/issue/ENG-123/... https://linear.app/company/issue/ENG-124/...

# Import with options
bd import linear ENG-123 --assignee=me --status=open
```

### Command Flags

```bash
--assignee <name>     # Override imported assignee (use 'me' for current user)
--status <status>     # Override imported status (open, in_progress, blocked, closed)
--priority <P0-P4>    # Override imported priority
--type <type>         # Override imported type (task, bug, feature)
--dry-run             # Preview import without creating beads
--verbose             # Show detailed import information
--create-deps         # Create dependency relationships for blocked issues
```

### Field Mapping: Linear → Beads

| Linear Field | Beads Field | Mapping Rules |
|--------------|-------------|---------------|
| `title` | `title` | Direct copy |
| `identifier` (e.g., "ENG-123") | `metadata.linear_id` | Store for reference/sync |
| `url` | `metadata.linear_url` | Store for easy access |
| `description` | `description` | Direct copy (preserve markdown) |
| `priority` (0-4) | `priority` (P0-P4) | Map: 1→P0, 2→P1, 3→P2, 4→P3, 0→P4 |
| `state` / `status` | `status` | Map workflow states (see below) |
| `assignee` | `assignee` | Use Linear name or map to local username |
| `labels` | `metadata.linear_labels` | Store as array for reference |
| `project` | `metadata.linear_project` | Store project name |
| `estimate` | `metadata.linear_estimate` | Preserve estimate if present |
| `createdAt` | `created` | Use Linear creation date |
| `updatedAt` | `updated` | Use Linear update date |

### Status/State Mapping

Linear workflow states map to beads statuses based on state type:

```
Linear State Type → Beads Status
─────────────────────────────────
"unstarted"       → open
"started"         → in_progress
"completed"       → closed
"canceled"        → closed (with reason: "canceled in Linear")
"backlog"         → open (with priority downgrade to P4)
```

### Type Inference

Beads `type` field is inferred from Linear metadata:

```
1. Check labels:
   - Contains "bug", "defect", "issue" → bug
   - Contains "feature", "enhancement" → feature
   - Otherwise → task

2. Check project name:
   - Contains "bug" → bug
   - Contains "feature" → feature
   - Otherwise → task

3. Default: task
```

Allow explicit override with `--type` flag.

### Handling Linear-Specific Fields

**Labels**:
- Store in `metadata.linear_labels` as array
- Not mapped to beads types/statuses automatically
- Can be used for type inference (see above)

**Projects**:
- Store in `metadata.linear_project`
- Optionally could map to beads epics in future

**Teams**:
- Store in `metadata.linear_team`
- Not directly relevant to beads structure

**Attachments**:
- Store URLs in `metadata.linear_attachments` as array
- Include in description as markdown links

**Git Branches**:
- Store in `metadata.linear_git_branches` as array
- Useful for cross-referencing with git history

**Comments**:
- Not imported by default (too verbose)
- Use `--include-comments` flag to append to description

**Estimate**:
- Store in `metadata.linear_estimate`
- Could inform beads complexity/planning in future

### Edge Cases & Error Handling

#### Invalid URLs/IDs
```bash
$ bd import linear INVALID-123
Error: Issue "INVALID-123" not found in Linear
Hint: Verify the issue exists and you have access

$ bd import linear https://not-linear.app/issue/123
Error: Invalid Linear URL format
Expected: https://linear.app/<workspace>/issue/<ID>/...
```

#### Missing Fields
```
- No title: Error (title is required)
- No description: Create bead with empty description
- No priority: Default to P2 (medium)
- No assignee: Leave unassigned in beads
- No status: Default to "open"
```

#### API Errors
```bash
# Authentication failure
Error: Linear authentication failed
Hint: Run 'bd config set linear.api_key <key>' or authenticate via OAuth

# Rate limit exceeded
Error: Linear API rate limit exceeded (1500 req/hr)
Next available: 2026-01-16 14:30:00
Hint: Use --batch mode to reduce API calls

# Network error
Error: Failed to connect to Linear API
Hint: Check internet connection and try again
```

#### Duplicate Imports
```bash
$ bd import linear ENG-123
Bead beads-abc already exists for Linear issue ENG-123
Options:
  1. Skip import (default)
  2. Update existing bead (--update)
  3. Create new bead (--force-duplicate)

Use: bd import linear ENG-123 --update
```

#### Batch Import Considerations
```bash
# Partial failures in batch mode
$ bd import linear ENG-123 ENG-124 INVALID-125 ENG-126

Results:
  ✓ ENG-123 → beads-abc
  ✓ ENG-124 → beads-def
  ✗ INVALID-125: Not found
  ✓ ENG-126 → beads-ghi

Summary: 3 imported, 1 failed

# Continue on error (default)
Use --fail-fast to stop on first error
```

### Batch Import Support

**Single Command, Multiple Issues**:
```bash
# Import all issues in a project
bd import linear --project="Q1 Features"

# Import all issues assigned to user
bd import linear --assignee=john

# Import issues matching query
bd import linear --query="bug high priority"

# Import from file (one issue ID/URL per line)
bd import linear --from-file=issues.txt
```

**Rate Limit Handling**:
- Batch imports automatically throttle to respect rate limits
- Show progress indicator: `Importing... [3/10] ENG-125`
- Option to continue later if rate limit hit: save progress to `.linear-import-state`

### Dependency Creation

Linear issues can be "blocked by" other issues. Handle this with `--create-deps`:

```bash
$ bd import linear ENG-123 --create-deps

# If ENG-123 is blocked by ENG-120 and ENG-121:
1. Import ENG-123 → beads-abc
2. Import ENG-120 → beads-xyz (if not exists)
3. Import ENG-121 → beads-uvw (if not exists)
4. Create dependencies: beads-abc depends on beads-xyz, beads-uvw

# Use --max-dep-depth to limit recursive imports (default: 1)
```

### Configuration

Store Linear API credentials and preferences:

```bash
# Set API key
bd config set linear.api_key <key>

# Set default import options
bd config set linear.import.default_status open
bd config set linear.import.default_priority P2
bd config set linear.import.create_deps true
bd config set linear.import.include_comments false

# Set team/workspace context
bd config set linear.team ENG
bd config set linear.workspace my-company
```

## TUI Integration

### Trigger Import from TUI

**Hotkey**: `Ctrl+I` or `i` (when in issue list view)

**Menu Option**: Add "Import from Linear" to main menu:
```
Beads Issue Tracker
-------------------
[c] Create new issue
[i] Import from Linear
[l] List issues
[s] Search issues
...
```

### UI Flow

#### 1. Import Dialog

```
┌─────────────────────────────────────────────────────┐
│ Import from Linear                                  │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Enter Linear issue ID or URL:                      │
│ ┌─────────────────────────────────────────────────┐│
│ │ ENG-123                                         ││
│ └─────────────────────────────────────────────────┘│
│                                                     │
│ Options:                                            │
│ [×] Import dependencies                             │
│ [ ] Include comments                                │
│                                                     │
│ [Import]  [Cancel]                                  │
└─────────────────────────────────────────────────────┘
```

**Input validation**:
- Highlight invalid format in red
- Show autocomplete suggestions if possible
- Support pasting full URLs

#### 2. Progress Indication

For single imports:
```
┌─────────────────────────────────────────────────────┐
│ Importing from Linear...                            │
├─────────────────────────────────────────────────────┤
│                                                     │
│ [▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░] 50%                         │
│                                                     │
│ Fetching issue ENG-123...                           │
│                                                     │
└─────────────────────────────────────────────────────┘
```

For batch imports:
```
┌─────────────────────────────────────────────────────┐
│ Batch Import Progress                               │
├─────────────────────────────────────────────────────┤
│                                                     │
│ ✓ ENG-123 → beads-abc                              │
│ ✓ ENG-124 → beads-def                              │
│ ⋯ ENG-125 (fetching...)                            │
│   ENG-126 (queued)                                  │
│   ENG-127 (queued)                                  │
│                                                     │
│ [3/5 complete]  [Cancel]                            │
└─────────────────────────────────────────────────────┘
```

#### 3. Success Confirmation

```
┌─────────────────────────────────────────────────────┐
│ Import Successful                                   │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Created bead: beads-abc                             │
│ Title: Implement user authentication               │
│ Priority: P1 (High)                                 │
│ Status: In Progress                                 │
│                                                     │
│ Linear: ENG-123                                     │
│ URL: https://linear.app/company/issue/ENG-123/...   │
│                                                     │
│ [View Bead]  [Import Another]  [Close]              │
└─────────────────────────────────────────────────────┘
```

**Actions**:
- `[View Bead]`: Navigate to the newly created bead
- `[Import Another]`: Return to import dialog
- `[Close]`: Return to previous view

#### 4. Batch Import Summary

```
┌─────────────────────────────────────────────────────┐
│ Batch Import Complete                               │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Imported: 4 issues                                  │
│ Failed: 1 issue                                     │
│                                                     │
│ Results:                                            │
│ ✓ ENG-123 → beads-abc                              │
│ ✓ ENG-124 → beads-def                              │
│ ✗ INVALID-125 (not found)                          │
│ ✓ ENG-126 → beads-ghi                              │
│ ✓ ENG-127 → beads-jkl                              │
│                                                     │
│ [View Log]  [Close]                                 │
└─────────────────────────────────────────────────────┘
```

### Error Handling in TUI

#### Authentication Error
```
┌─────────────────────────────────────────────────────┐
│ ⚠️  Authentication Required                          │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Linear API authentication failed.                   │
│                                                     │
│ Set your API key:                                   │
│   bd config set linear.api_key <key>                │
│                                                     │
│ Or authenticate via OAuth:                          │
│   bd auth linear                                    │
│                                                     │
│ [Configure]  [Cancel]                               │
└─────────────────────────────────────────────────────┘
```

Clicking `[Configure]` opens a sub-dialog:
```
┌─────────────────────────────────────────────────────┐
│ Configure Linear Authentication                     │
├─────────────────────────────────────────────────────┤
│                                                     │
│ API Key:                                            │
│ ┌─────────────────────────────────────────────────┐│
│ │ ••••••••••••••••••••••••••••••••                ││
│ └─────────────────────────────────────────────────┘│
│                                                     │
│ Get your API key from:                              │
│ https://linear.app/settings/api                     │
│                                                     │
│ [Save]  [Cancel]                                    │
└─────────────────────────────────────────────────────┘
```

#### Issue Not Found
```
┌─────────────────────────────────────────────────────┐
│ ⚠️  Issue Not Found                                  │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Linear issue "ENG-999" was not found.               │
│                                                     │
│ Possible reasons:                                   │
│ • Issue doesn't exist                               │
│ • You don't have access                             │
│ • Team prefix is incorrect                          │
│                                                     │
│ [Try Again]  [Cancel]                               │
└─────────────────────────────────────────────────────┘
```

#### Rate Limit Exceeded
```
┌─────────────────────────────────────────────────────┐
│ ⚠️  Rate Limit Exceeded                              │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Linear API rate limit reached (1500/hr)             │
│                                                     │
│ Resets at: 14:30:00 (in 45 minutes)                 │
│                                                     │
│ Imported so far: 3 issues                           │
│                                                     │
│ [Save Progress]  [Cancel Import]                    │
└─────────────────────────────────────────────────────┘
```

Clicking `[Save Progress]`:
- Saves import state to `.linear-import-state`
- Shows message: "Progress saved. Run 'bd import linear --resume' to continue."

#### Network Error
```
┌─────────────────────────────────────────────────────┐
│ ⚠️  Connection Failed                                │
├─────────────────────────────────────────────────────┤
│                                                     │
│ Could not connect to Linear API                     │
│                                                     │
│ Error: Connection timeout                           │
│                                                     │
│ Check your internet connection and try again.       │
│                                                     │
│ [Retry]  [Cancel]                                   │
└─────────────────────────────────────────────────────┘
```

#### Duplicate Issue
```
┌─────────────────────────────────────────────────────┐
│ ⚠️  Issue Already Imported                           │
├─────────────────────────────────────────────────────┤
│                                                     │
│ A bead already exists for Linear issue ENG-123      │
│                                                     │
│ Existing bead: beads-abc                            │
│ Title: Implement user authentication               │
│ Status: In Progress                                 │
│                                                     │
│ [Update Existing]  [Create Duplicate]  [Cancel]     │
└─────────────────────────────────────────────────────┘
```

### Keyboard Shortcuts in TUI

| Key | Action |
|-----|--------|
| `Ctrl+I` or `i` | Open import dialog |
| `Enter` | Confirm import |
| `Esc` | Cancel dialog |
| `Tab` | Cycle through fields/buttons |
| `Space` | Toggle checkboxes |
| `Ctrl+V` | Paste Linear URL |

### Context Menu Integration

Right-click on issue list to show context menu:
```
┌──────────────────────┐
│ Create New Issue     │
│ Import from Linear   │  ← New option
│ ──────────────────   │
│ Search               │
│ Filter               │
│ Sort                 │
└──────────────────────┘
```

### Batch Import in TUI

**Multi-select Mode**:
```
Import Multiple Issues
─────────────────────────────────────────────────────
Paste Linear URLs or IDs (one per line):

┌───────────────────────────────────────────────────┐
│ ENG-123                                           │
│ ENG-124                                           │
│ https://linear.app/company/issue/ENG-125/...      │
│                                                   │
│                                                   │
└───────────────────────────────────────────────────┘

[Import All]  [Cancel]
```

Or import by filter:
```
Import by Filter
─────────────────────────────────────────────────────
Project: ┌─────────────────────┐
         │ Q1 Features         │
         └─────────────────────┘

Assignee: ┌─────────────────────┐
          │ john                │
          └─────────────────────┘

Status: ┌─────────────────────┐
        │ In Progress         │
        └─────────────────────┘

[Preview (12 issues)]  [Import]  [Cancel]
```

## Implementation Notes

### Phase 1: CLI Basic Import
- Single issue import by ID/URL
- Basic field mapping
- Error handling
- Configuration management

### Phase 2: CLI Advanced Features
- Batch import
- Dependency creation
- Rate limit handling
- Import state management

### Phase 3: TUI Integration
- Import dialog
- Progress indication
- Error dialogs
- Success confirmation

### Phase 4: Advanced TUI Features
- Batch import UI
- Filter-based import
- Import history/resume
- Real-time sync option

## Configuration File Schema

```toml
[linear]
api_key = "lin_api_..."
workspace = "my-company"
team = "ENG"

[linear.import]
default_status = "open"
default_priority = "P2"
create_deps = true
max_dep_depth = 1
include_comments = false
auto_update = false  # Sync changes from Linear periodically

[linear.mapping]
# Custom status mapping
[linear.mapping.status]
"Backlog" = "open"
"Todo" = "open"
"In Progress" = "in_progress"
"In Review" = "in_progress"
"Done" = "closed"
"Canceled" = "closed"

# Custom priority mapping
[linear.mapping.priority]
0 = "P4"  # No priority
1 = "P0"  # Urgent
2 = "P1"  # High
3 = "P2"  # Medium
4 = "P3"  # Low
```

## Testing Scenarios

### CLI Testing
1. Import single issue by ID
2. Import single issue by URL
3. Import with invalid ID
4. Import with invalid URL
5. Import with missing authentication
6. Import duplicate issue
7. Batch import multiple issues
8. Import with dependencies
9. Import with rate limit exceeded
10. Import with all optional flags

### TUI Testing
1. Open import dialog with hotkey
2. Enter valid issue ID
3. Enter invalid issue ID
4. Paste Linear URL
5. Toggle import options
6. Cancel import
7. View imported bead
8. Handle authentication error
9. Handle network error
10. Batch import via multi-select

## Security Considerations

1. **API Key Storage**: Store in config file with restricted permissions (600)
2. **Input Validation**: Sanitize all user input to prevent injection
3. **Rate Limiting**: Respect Linear's rate limits to avoid account suspension
4. **OAuth Tokens**: Use secure storage, refresh tokens as needed
5. **Error Messages**: Don't expose sensitive information in error messages

## Future Enhancements

1. **Bi-directional Sync**: Update Linear when beads change
2. **Real-time Sync**: Watch Linear for changes and auto-import
3. **Webhook Integration**: Receive Linear webhooks for instant updates
4. **Custom Field Mapping**: User-defined mapping rules
5. **Export to Linear**: Create Linear issues from beads
6. **Comment Sync**: Import and sync comments bi-directionally
7. **Attachment Sync**: Download and attach files from Linear
8. **Epic Mapping**: Map Linear projects to beads epics

---

**Last Updated**: 2026-01-16
**Design Completed For**: Bead ac-kqy9.2 - Design Linear import command interface
