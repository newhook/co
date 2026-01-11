# SQL Null Fields Analysis

This document analyzes the use of nullable fields in the SQL database schema and Go code, examining whether simplifications are possible.

## Current State

### Schema Overview

The database has 5 main tables with nullable fields:

| Table | Total Fields | Nullable Fields | Required Fields |
|-------|--------------|-----------------|-----------------|
| works | 12 | 9 | 3 (id, status, created_at) |
| beads | 12 | 10 | 2 (id, status) |
| tasks | 14 | 11 | 3 (id, status, task_type) |
| work_tasks | 3 | 1 | 2 (work_id, task_id) |
| complexity_cache | 5 | 1 | 4 |

### Go Code Complexity

The nullable fields generate significant code complexity:

1. **Conversion functions** in `internal/db/bead.go`:
   - `nullString(s string) sql.NullString` - converts empty string to invalid NullString
   - `nullTime(t time.Time) sql.NullTime` - converts zero time to invalid NullTime

2. **Mapping functions** to convert between sqlc models and local types:
   - `workToLocal(w *sqlc.Work) *Work`
   - `taskToLocal(t *sqlc.Task) *Task`
   - `beadToTracked(b *sqlc.Bead) *TrackedBead`

3. **Field access patterns** requiring `.String`, `.Int64`, `.Time`, `.Valid` checks everywhere

### Lines of Conversion Code

| File | Conversion Lines |
|------|-----------------|
| work.go | ~25 lines (workToLocal + nullString/nullTime calls) |
| task.go | ~25 lines (taskToLocal + nullString/nullTime calls) |
| bead.go | ~30 lines (beadToTracked + helper functions) |
| **Total** | **~80 lines** |

## Field Categories

### Timestamps (sql.NullTime)

| Field | Tables | Nullable Required? |
|-------|--------|-------------------|
| started_at | works, beads, tasks | **Yes** - null means "not started" |
| completed_at | works, beads, tasks | **Yes** - null means "not completed" |
| created_at | all | **No** - always set via DEFAULT |
| updated_at | beads | **No** - always set via DEFAULT |

**Verdict**: Timestamps like `started_at` and `completed_at` genuinely need nullability. Using a sentinel value (e.g., `0001-01-01`) would be semantically incorrect and error-prone.

### String Fields (sql.NullString)

| Field | Tables | Could Use Empty String? |
|-------|--------|------------------------|
| zellij_session | works, beads, tasks | Yes - empty = not set |
| zellij_tab/pane | works, beads, tasks | Yes - empty = not set |
| worktree_path | works, beads, tasks | Yes - empty = not set |
| branch_name | works | Yes - empty = not set |
| base_branch | works | Probably - has DEFAULT |
| pr_url | works, beads, tasks | Yes - empty = not set |
| error_message | works, beads, tasks | Yes - empty = no error |
| title | beads | Yes - empty = no title |

**Verdict**: All string fields could use `NOT NULL DEFAULT ''` instead of nullable. Empty string is a valid sentinel for "not set" in all these cases.

### Integer Fields (sql.NullInt64)

| Field | Tables | Could Use Zero? |
|-------|--------|-----------------|
| complexity_budget | tasks | Yes - 0 = no budget set |
| actual_complexity | tasks | Yes - 0 = not measured |
| position | work_tasks | Yes - 0 = first position |

**Verdict**: Integer fields could use `NOT NULL DEFAULT 0`. Zero is semantically valid as "not set" or "first position".

## Simplification Options

### Option 1: Keep Current Design

**Pros**:
- Clear semantic distinction between "not set" (NULL) and "empty" ("")
- Explicit handling of optional values
- Standard SQLite/Go idiom

**Cons**:
- Significant boilerplate code (~80 lines)
- Every field access requires `.String`, `.Valid` checks
- Conversion functions between sqlc and local types

### Option 2: Use Non-Nullable with Defaults

Change schema to:
```sql
-- Example: works table
CREATE TABLE works (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending',
    zellij_session TEXT NOT NULL DEFAULT '',
    zellij_tab TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    base_branch TEXT NOT NULL DEFAULT 'main',
    pr_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at DATETIME,      -- Keep nullable
    completed_at DATETIME,    -- Keep nullable
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Impact**:
- String fields: `sql.NullString` → `string` (direct access)
- Integer fields: `sql.NullInt64` → `int64` (direct access)
- Time fields: Still need `sql.NullTime` for started_at/completed_at

**Pros**:
- Eliminates ~50% of conversion code
- Simpler field access (no `.String`, `.Valid`)
- sqlc generates plain Go types

**Cons**:
- Loses NULL vs empty string distinction
- Requires migration with data transformation
- created_at/updated_at already use DEFAULT, no benefit

### Option 3: sqlc Pointer Types

Configure sqlc to emit pointer types for nullable fields:
```yaml
# sqlc.yaml
gen:
  go:
    emit_pointers_for_null_types: true
```

This generates `*string`, `*time.Time`, `*int64` instead of `sql.NullString`, etc.

**Pros**:
- More idiomatic Go
- Easier nil checks vs `.Valid`
- No conversion functions needed

**Cons**:
- Requires sqlc configuration change
- Pointer dereferencing needed everywhere
- Memory allocation for each field

### Option 4: Hybrid Approach

Keep timestamps nullable, make strings/integers non-nullable:

| Type | Current | Proposed |
|------|---------|----------|
| Timestamps (started_at, completed_at) | sql.NullTime | sql.NullTime (keep) |
| Strings (session, path, url, etc.) | sql.NullString | string (NOT NULL DEFAULT '') |
| Integers (complexity, position) | sql.NullInt64 | int64 (NOT NULL DEFAULT 0) |

**Reduction**:
- Removes 8 NullString conversions per table
- Removes 2-3 NullInt64 conversions per table
- Keeps 2 NullTime conversions for meaningful timestamps
- Estimated code reduction: ~60 lines

## Recommendation

**Option 4 (Hybrid Approach)** provides the best balance:

1. **Timestamps remain nullable** - They have genuine null semantics (started_at = null means "never started")

2. **Strings become non-nullable with defaults** - Empty string is semantically equivalent to "not set" for all current use cases

3. **Integers become non-nullable with defaults** - Zero is valid for "no budget" or "first position"

### Implementation Steps

1. Create migration `005_nullable_to_defaults.sql`:
   ```sql
   -- +up
   -- Update NULL strings to empty strings
   UPDATE works SET zellij_session = '' WHERE zellij_session IS NULL;
   -- ... repeat for other columns

   -- Note: SQLite doesn't support ALTER COLUMN, would need table recreation

   -- +down
   -- Reverse is not meaningful since '' != NULL semantically
   ```

2. Update schema.sql with new column definitions

3. Regenerate sqlc code: `mise run sqlc-generate`

4. Simplify Go code:
   - Remove `nullString()` helper
   - Update `workToLocal`, `taskToLocal`, `beadToTracked` to remove NullString handling
   - Keep `nullTime()` helper for timestamp fields

### Estimated Effort

- Schema migration: Low complexity (data transformation)
- Code changes: ~60 lines removed, ~20 lines modified
- Testing: Moderate (verify all queries still work)

### Risk Assessment

- **Low risk**: Empty string behaves identically to NULL in all current queries
- **Medium risk**: SQLite table recreation needed for column changes
- **Mitigation**: Comprehensive testing, staged rollout

## Files Affected

- `internal/db/schema.sql` - Schema changes
- `internal/db/migrations/005_*.sql` - New migration
- `internal/db/bead.go` - Remove nullString, simplify beadToTracked
- `internal/db/work.go` - Simplify workToLocal
- `internal/db/task.go` - Simplify taskToLocal
- `internal/db/sqlc/*.go` - Regenerated (automatic)

## Conclusion

The nullable fields add approximately 80 lines of conversion code. About 75% of this complexity (string and integer fields) could be eliminated by using non-nullable columns with sensible defaults. Timestamps should remain nullable as they have genuine null semantics.

The recommended hybrid approach would reduce code complexity while preserving meaningful null semantics for timestamps.
