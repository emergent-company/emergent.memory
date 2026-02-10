# Schema Migration Guide

## Overview

The Emergent graph system supports automatic migration of objects from one schema version to another. Objects using different schema versions can coexist without issues, and migration is only required when you want to standardize all objects to a new schema version.

## Architecture

### Multi-Version Coexistence

Objects in different schema versions live together harmoniously:

```
Project: "Acme Corp"
├── Person objects on schema v1.0.0
│   └── properties: {name, age}
├── Person objects on schema v2.0.0
│   └── properties: {name, age, email, department}
└── Person objects on schema v3.0.0
    └── properties: {name, age, email, department, hire_date}
```

Each object tracks its schema version via the `schema_version` field.

### Migration Strategy

The migrator analyzes schema differences and:

1. **Preserves compatible fields** - Fields with matching names and types migrate automatically
2. **Coerces type changes** - Automatic conversion when possible (e.g., string "30" → number 30)
3. **Flags incompatibilities** - Reports issues requiring manual intervention
4. **Suggests solutions** - Provides actionable guidance for each issue

## Migration Issue Types

| Type                 | Severity | Description                            | Auto-Handled             |
| -------------------- | -------- | -------------------------------------- | ------------------------ |
| `field_renamed`      | warning  | Field exists in old schema but not new | No - data dropped        |
| `field_removed`      | warning  | Field no longer exists in new schema   | Yes - field dropped      |
| `field_type_changed` | varies   | Type changed (e.g., string → number)   | If coercible             |
| `new_required_field` | error    | New required field added               | No - needs default value |
| `coercion_failed`    | error    | Cannot convert type                    | No - manual fix required |
| `validation_failed`  | error    | New schema validation failed           | No - fix data first      |
| `incompatible_type`  | error    | Fundamentally incompatible types       | No - manual migration    |

## Migration CLI Tool

### Overview

The migration CLI provides a comprehensive data safety system that prevents accidental data loss during schema migrations. It implements multi-level risk assessment and blocks potentially destructive operations unless explicitly authorized.

### Safety Features

#### Data Preservation

- **Automatic Archival**: Dropped fields are automatically archived before removal
- **Archive Format**: Full audit trail with version history and timestamps
- **No Data Loss**: All dropped data preserved in `migration_archive` column
- **Rollback Support**: Can restore archived data if migration was incorrect

#### Risk Assessment

Every migration is automatically classified into one of four risk levels:

| Risk Level      | Conditions                                | Behavior                       | Flags Required                |
| --------------- | ----------------------------------------- | ------------------------------ | ----------------------------- |
| **SAFE** ✓      | No changes or only adding optional fields | Auto-proceed                   | None                          |
| **CAUTIOUS** ⚠  | Type coercion happening                   | Auto-proceed with notification | None                          |
| **RISKY** ⚠⚠    | 1-2 fields dropped                        | **Blocked**                    | `--force`                     |
| **DANGEROUS** ✗ | 3+ fields dropped OR validation errors    | **Blocked**                    | `--force --confirm-data-loss` |

#### Safety Flags

```bash
--force                # Allow risky migrations (1-2 fields dropped)
--confirm-data-loss    # Allow dangerous migrations (3+ fields dropped)
--skip-archive         # Skip archiving dropped fields (NOT RECOMMENDED)
```

## Data Safety System

### Automatic Archival

Every time a migration drops fields from an object, those fields are automatically preserved in the `migration_archive` column before removal:

**Archive Entry Format** (JSONB):

```json
{
  "from_version": "1.0.0",
  "to_version": "2.0.0",
  "timestamp": "2025-01-15T10:30:00Z",
  "dropped_data": {
    "old_field": "important data",
    "deprecated_field": "more data",
    "legacy_status": "active"
  }
}
```

### Archive Storage

- **Location**: `kb.graph_objects.migration_archive` column (JSONB array)
- **Retention**: Archives persist indefinitely (manual cleanup required if needed)
- **Structure**: Array of archive entries, one per migration that dropped fields
- **Size**: Indexed for efficient queries (`idx_graph_objects_has_archive`)
- **Audit Trail**: Complete history of all data loss events with timestamps

### Data Preservation Guarantees

✅ **Always Archived** (unless `--skip-archive` used):

- Fields removed from schema
- Fields renamed (seen as dropped + new field)
- Fields present in old schema but not in new schema

❌ **Never Archived**:

- Fields that migrated successfully
- Fields added in new schema
- Optional fields with null values

### Querying Archives

```sql
-- Find all objects with archived data
SELECT id, name, migration_archive
FROM kb.graph_objects
WHERE migration_archive IS NOT NULL;

-- Find objects that lost specific field
SELECT id, name, archive_entry->>'from_version' as from_ver
FROM kb.graph_objects,
     jsonb_array_elements(migration_archive) as archive_entry
WHERE archive_entry->'dropped_data' ? 'old_field';

-- Get archive entry for specific migration
SELECT archive_entry
FROM kb.graph_objects,
     jsonb_array_elements(migration_archive) as archive_entry
WHERE id = 'object-uuid'
  AND archive_entry->>'from_version' = '1.0.0'
  AND archive_entry->>'to_version' = '2.0.0';
```

## Risk Levels

The migration system automatically classifies every migration into four risk levels. Each level has different behaviors and requirements:

### SAFE ✓

**Conditions**:

- No errors
- No warnings (no fields dropped)
- Only type coercions OR no changes at all

**Examples**:

- Adding optional fields
- Compatible type changes (number → string)
- No schema changes

**Behavior**: Auto-proceeds without user intervention

**Flags Required**: None

### CAUTIOUS ⚠

**Conditions**:

- No errors
- Type coercion required (auto-convertible changes)
- No fields dropped

**Examples**:

```json
// v1.0.0
{ "age": "30", "score": "95.5" }

// v2.0.0 (age: string → number, score: string → number)
{ "age": 30, "score": 95.5 }
```

**Behavior**: Auto-proceeds with notification

**Flags Required**: None

**Warning Messages**:

```
⚠ CAUTIOUS migration object_id=abc123
  Coerced 2 fields: age (string→number), score (string→number)
```

### RISKY ⚠⚠

**Conditions**:

- No errors
- 1-2 fields dropped from schema
- Data will be archived then removed

**Examples**:

```json
// v1.0.0
{ "name": "John", "old_status": "active", "legacy_flag": true }

// v2.0.0 (drops old_status, legacy_flag)
{ "name": "John" }

// Archive created:
{
  "from_version": "1.0.0",
  "to_version": "2.0.0",
  "timestamp": "...",
  "dropped_data": {
    "old_status": "active",
    "legacy_flag": true
  }
}
```

**Behavior**: **BLOCKED** - requires explicit user authorization

**Flags Required**: `--force`

**Error Message**:

```
✗ Migration blocked (RISKY)
  Reason: This migration will drop 2 fields
  Use --force to proceed with archival
```

### DANGEROUS ✗

**Conditions**:

- Has validation errors OR
- Drops 3+ fields from schema

**Examples**:

**Example 1: Validation Errors**

```json
// v1.0.0
{ "name": "John", "age": "thirty" }

// v2.0.0 (age: string → number, email required)
// ERROR: Cannot coerce "thirty" to number
// ERROR: Missing required field "email"
```

**Example 2: Many Fields Dropped**

```json
// v1.0.0
{
  "name": "John",
  "field1": "data1",
  "field2": "data2",
  "field3": "data3",
  "field4": "data4"
}

// v2.0.0 (drops field1, field2, field3, field4)
{ "name": "John" }
// Drops 4 fields = DANGEROUS
```

**Behavior**: **BLOCKED** - requires strongest authorization

**Flags Required**: `--force --confirm-data-loss`

**Error Messages**:

```
✗ Migration blocked (DANGEROUS)
  Reason: This migration has 2 validation errors and will drop 4 fields
  Errors:
    - Cannot coerce field 'age': "thirty" is not a valid number
    - Missing required field 'email'
  Use --force --confirm-data-loss to proceed
```

### Risk Level Decision Tree

```
Migration Analysis
├─ Has errors?
│  ├─ YES → DANGEROUS ✗
│  └─ NO → Continue
├─ Fields dropped?
│  ├─ YES → How many?
│  │   ├─ 3+ fields → DANGEROUS ✗
│  │   └─ 1-2 fields → RISKY ⚠⚠
│  └─ NO → Continue
├─ Type coercion needed?
│  ├─ YES → CAUTIOUS ⚠
│  └─ NO → SAFE ✓
```

## CLI Safety Flags Usage

### --force

**Purpose**: Authorize RISKY migrations that drop 1-2 fields

**When Required**:

- Migration classified as RISKY
- Will drop 1-2 fields from objects
- No validation errors present

**Example**:

```bash
# Dry run shows RISKY classification
./bin/migrate-schema -project $PROJECT -from 1.0.0 -to 2.0.0

# Output:
# ⚠⚠ RISKY: Will drop 2 fields (old_status, legacy_flag)
# Migration blocked. Use --force to proceed.

# Apply with --force
./bin/migrate-schema \
  -project $PROJECT \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --force
```

**Data Protection**:

- Dropped fields are archived before removal
- Can be rolled back using rollback procedure
- Archive preserved indefinitely

### --confirm-data-loss

**Purpose**: Authorize DANGEROUS migrations with validation errors or 3+ dropped fields

**When Required**:

- Migration classified as DANGEROUS
- Has validation errors OR drops 3+ fields
- Must be used WITH --force flag

**Example 1: Validation Errors**

```bash
# Dry run shows DANGEROUS classification
./bin/migrate-schema -project $PROJECT -from 1.0.0 -to 2.0.0

# Output:
# ✗ DANGEROUS: 2 validation errors
#   - Cannot coerce age: "thirty" → number
#   - Missing required field: email
# Migration blocked. Use --force --confirm-data-loss to proceed.

# WRONG: Missing --confirm-data-loss
./bin/migrate-schema -project $PROJECT -from 1.0.0 -to 2.0.0 -dry-run=false --force
# Error: DANGEROUS migration requires --confirm-data-loss flag

# CORRECT: Both flags required
./bin/migrate-schema \
  -project $PROJECT \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --force \
  --confirm-data-loss
```

**Example 2: Many Fields Dropped**

```bash
# Migration drops 4 fields
./bin/migrate-schema -project $PROJECT -from 2.0.0 -to 3.0.0

# Output:
# ✗ DANGEROUS: Will drop 4 fields
# Migration blocked. Use --force --confirm-data-loss to proceed.

./bin/migrate-schema \
  -project $PROJECT \
  -from 2.0.0 \
  -to 3.0.0 \
  -dry-run=false \
  --force \
  --confirm-data-loss
```

### --skip-archive

**Purpose**: Skip automatic archival of dropped fields (NOT RECOMMENDED)

**When To Use**: Almost never - only in specific scenarios:

- Dropping fields known to contain only null values
- Dropping temporary test fields
- Migration in test/dev environment only
- Storage constraints require immediate cleanup

**Risk**: Permanent data loss - dropped fields cannot be recovered

**Example**:

```bash
# DANGEROUS: No archive created, data permanently lost
./bin/migrate-schema \
  -project $PROJECT \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --force \
  --skip-archive
```

**Recommendation**: Always create archives. Disk space is cheap, data recovery is expensive.

### Flag Combinations

| Migration   | --force | --confirm-data-loss | --skip-archive | Result                           |
| ----------- | ------- | ------------------- | -------------- | -------------------------------- |
| SAFE ✓      | No      | No                  | No             | ✅ Proceeds                      |
| CAUTIOUS ⚠  | No      | No                  | No             | ✅ Proceeds                      |
| RISKY ⚠⚠    | No      | No                  | No             | ❌ Blocked                       |
| RISKY ⚠⚠    | Yes     | No                  | No             | ✅ Proceeds + archive            |
| DANGEROUS ✗ | No      | No                  | No             | ❌ Blocked                       |
| DANGEROUS ✗ | Yes     | No                  | No             | ❌ Blocked                       |
| DANGEROUS ✗ | Yes     | Yes                 | No             | ✅ Proceeds + archive            |
| Any ⚠⚠/✗    | Yes     | \*                  | Yes            | ✅ Proceeds + NO archive (risky) |

## Rollback Procedures

### When Rollback Is Needed

- Migration produced unexpected results
- Data loss was more extensive than anticipated
- Schema change broke dependent systems
- Business requirements changed after migration

### Automatic Rollback (Using Archive)

The `RollbackObject()` method can restore dropped fields from the migration archive:

**Method Signature**:

```go
func (sm *SchemaMigrator) RollbackObject(
    ctx context.Context,
    obj *GraphObject,
    targetVersion string,
) (*RollbackResult, error)
```

**What It Does**:

1. Searches `migration_archive` for entry with `to_version` matching `targetVersion`
2. Extracts `dropped_data` from that archive entry
3. Merges dropped fields back into object's `properties`
4. Removes the archive entry (migration was "undone")
5. Updates object's `schema_version` to `from_version` of the archive

**Example**:

```go
// Object after migration 1.0.0 → 2.0.0 (dropped old_status)
obj := graphObjects[0] // schema_version: "2.0.0"
// properties: { "name": "John", "age": 30 }
// migration_archive: [{
//   "from_version": "1.0.0",
//   "to_version": "2.0.0",
//   "dropped_data": { "old_status": "active" }
// }]

// Rollback to version 1.0.0
result, err := migrator.RollbackObject(ctx, obj, "2.0.0")

// Object after rollback
// schema_version: "1.0.0"
// properties: { "name": "John", "age": 30, "old_status": "active" }
// migration_archive: [] (archive entry removed)
```

**Rollback Result**:

```go
type RollbackResult struct {
    Success        bool
    RestoredFields []string  // ["old_status"]
    NewVersion     string    // "1.0.0"
    ErrorMessage   string
}
```

### Rollback Limitations

**Can Roll Back**:

- Field drops (data archived)
- Schema version changes
- Single migration at a time

**Cannot Roll Back**:

- Type coercions (data already converted)
- Validation fixes (data already modified)
- Multiple migrations in one step (must rollback sequentially)
- Migrations where `--skip-archive` was used (no archive exists)

### Manual Rollback (SQL)

If archive doesn't exist or for complex scenarios:

```sql
-- 1. Restore from backup table (if you created one)
UPDATE kb.graph_objects
SET
  schema_version = '1.0.0',
  properties = backup_table.properties
FROM backup_graph_objects AS backup_table
WHERE kb.graph_objects.id = backup_table.id
  AND kb.graph_objects.project_id = 'a1b2c3d4-...';

-- 2. Or restore from database backup file
psql emergent < backup_20260210_143022.sql

-- 3. Verify restoration
SELECT schema_version, COUNT(*) as count
FROM kb.graph_objects
WHERE project_id = 'a1b2c3d4-...'
GROUP BY schema_version;
```

### Rollback Strategy

**Best Practices**:

1. **Always Backup Before Migration**:

   ```bash
   pg_dump -h localhost -U emergent emergent \
     --table=kb.graph_objects \
     --table=kb.graph_relationships \
     > backup_$(date +%Y%m%d_%H%M%S).sql
   ```

2. **Test Rollback In Dry-Run**:

   - Run migration with `-dry-run=true`
   - Review archive entries that would be created
   - Understand which fields would be dropped
   - Confirm rollback would restore expected data

3. **Incremental Rollback**:

   - If migrated 1.0.0 → 1.1.0 → 2.0.0
   - Rollback in reverse order: 2.0.0 → 1.1.0 → 1.0.0
   - Each rollback uses its specific archive entry

4. **Archive Verification**:

   ```sql
   -- Check if rollback is possible
   SELECT
     id,
     name,
     schema_version,
     jsonb_array_length(migration_archive) as archive_count,
     migration_archive
   FROM kb.graph_objects
   WHERE migration_archive IS NOT NULL
     AND id = 'object-uuid';
   ```

5. **Post-Rollback Validation**:

   ```sql
   -- Verify version distribution
   SELECT schema_version, COUNT(*) as count
   FROM kb.graph_objects
   WHERE project_id = 'project-uuid'
   GROUP BY schema_version;

   -- Verify data integrity
   SELECT id, properties
   FROM kb.graph_objects
   WHERE schema_version = '1.0.0'
   LIMIT 10;
   ```

### Rollback Scenarios

**Scenario 1: Single Migration Rollback (CLI)**

```bash
# Migrated 1.0.0 → 2.0.0, want to go back

# Step 1: Preview rollback (dry-run)
./bin/migrate-schema \
  -project $PROJECT \
  --rollback \
  --rollback-version 2.0.0

# Review output - shows which objects will be rolled back

# Step 2: Execute rollback
./bin/migrate-schema \
  -project $PROJECT \
  --rollback \
  --rollback-version 2.0.0 \
  -dry-run=false

# Outcome:
# - All objects migrated to 2.0.0 are restored to their previous version
# - Dropped fields are restored from archive
# - Archive entry for 2.0.0 migration is removed
```

**Scenario 2: Manual SQL Rollback**

```sql
# If you prefer manual SQL or CLI rollback failed
UPDATE kb.graph_objects
SET
  schema_version = '1.0.0',
  properties = properties || (
    SELECT archive_entry->'dropped_data'
    FROM jsonb_array_elements(migration_archive) archive_entry
    WHERE archive_entry->>'to_version' = '2.0.0'
  ),
  migration_archive = (
    SELECT jsonb_agg(elem)
    FROM jsonb_array_elements(migration_archive) elem
    WHERE elem->>'to_version' != '2.0.0'
  )
WHERE schema_version = '2.0.0'
  AND project_id = 'project-uuid';
```

**Scenario 3: Chain Rollback**

```bash
# Migrated 1.0.0 → 1.1.0 → 2.0.0
# Rollback chain in reverse:

# Step 1: Rollback 2.0.0 → 1.1.0
./bin/migrate-schema -project $PROJECT --rollback --rollback-version 2.0.0 -dry-run=false

# Step 2: Rollback 1.1.0 → 1.0.0
./bin/migrate-schema -project $PROJECT --rollback --rollback-version 1.1.0 -dry-run=false
```

**Scenario 4: Restore From Backup**

```bash
# Migration had errors or --skip-archive was used
# Restore from database backup
psql emergent < backup_20260210_143022.sql
```

**Scenario 5: Partial Rollback**

```sql
# Rollback only specific objects (e.g., objects that failed validation)
UPDATE kb.graph_objects
SET
  schema_version = '1.0.0',
  properties = properties || (
    SELECT archive_entry->'dropped_data'
    FROM jsonb_array_elements(migration_archive) archive_entry
    WHERE archive_entry->>'to_version' = '2.0.0'
  )
WHERE id IN (
  SELECT id FROM failed_migration_objects
);
```

**Scenario 2: Chain Rollback**

```bash
# Migrated 1.0.0 → 1.1.0 → 2.0.0
# Rollback chain in reverse:

# Step 1: Rollback 2.0.0 → 1.1.0
./bin/migrate-schema -rollback -target-version 2.0.0

# Step 2: Rollback 1.1.0 → 1.0.0
./bin/migrate-schema -rollback -target-version 1.1.0
```

**Scenario 3: Restore From Backup**

```bash
# Migration had errors or --skip-archive was used
# Restore from database backup
psql emergent < backup_20260210_143022.sql
```

**Scenario 4: Partial Rollback**

```sql
-- Rollback only objects that failed validation
UPDATE kb.graph_objects
SET
  schema_version = '1.0.0',
  properties = properties || (
    SELECT archive_entry->'dropped_data'
    FROM jsonb_array_elements(migration_archive) archive_entry
    WHERE archive_entry->>'to_version' = '2.0.0'
  )
WHERE id IN (
  SELECT id FROM failed_migration_objects
);
```

### Installation

Built automatically with the server:

```bash
cd apps/server-go
go build -o ./bin/migrate-schema ./cmd/migrate-schema
```

### Usage

#### Safe Migration (Preview)

```bash
# Dry run - shows what would happen without changing data
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0
```

#### Risky Migration (Requires --force)

```bash
# Migration drops 1-2 fields (RISKY)
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --force
```

#### Dangerous Migration (Requires --force --confirm-data-loss)

```bash
# Migration drops 3+ fields or has validation errors (DANGEROUS)
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --force \
  --confirm-data-loss
```

#### Skip Archive (NOT RECOMMENDED)

```bash
# Skip archiving dropped fields (use with extreme caution)
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false \
  --skip-archive
```

### Output

```
INFO Batch processed batch_size=100 total_processed=100

✓ SAFE migration object_id=abc123 (no data loss)
⚠ CAUTIOUS migration object_id=def456 (type coercion: age string→number)
⚠⚠ RISKY migration object_id=ghi789 (dropped field: old_field)
✗ DANGEROUS migration object_id=jkl012 (dropped 3 fields + validation error)

=== Migration Summary ===
Mode: DRY RUN (no changes applied)

Total objects:     250
Successful:        200
Failed:            30
Skipped:           20

Risk Distribution:
  ✓ SAFE:          150 (60%)
  ⚠ CAUTIOUS:       30 (12%)
  ⚠⚠ RISKY:          15 (6%)
  ✗ DANGEROUS:       5 (2%)

Blocked migrations: 20 (requires --force or --confirm-data-loss)
========================
```

## Programmatic Usage

### Basic Migration

```go
import (
    "github.com/emergent/emergent-core/domain/graph"
    "github.com/emergent/emergent-core/domain/extraction/agents"
)

// Create migrator
validator := graph.NewPropertyValidator()
migrator := graph.NewSchemaMigrator(validator, logger)

// Define schemas
v1Schema := &agents.ObjectSchema{
    Name: "Person",
    Properties: map[string]agents.PropertyDef{
        "name": {Type: "string"},
        "age":  {Type: "number"},
    },
    Required: []string{"name"},
}

v2Schema := &agents.ObjectSchema{
    Name: "Person",
    Properties: map[string]agents.PropertyDef{
        "name":  {Type: "string"},
        "age":   {Type: "number"},
        "email": {Type: "string"},
    },
    Required: []string{"name", "email"},
}

// Migrate object
result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

if result.Success {
    // Update database
    obj.Properties = result.NewProperties
    obj.SchemaVersion = stringPtr("2.0.0")
    db.NewUpdate().Model(obj).WherePK().Exec(ctx)
} else {
    // Handle issues
    for _, issue := range result.Issues {
        if issue.Severity == "error" {
            log.Printf("ERROR: %s - %s", issue.Description, issue.Suggestion)
        }
    }
}
```

### Analyzing Migration Results

```go
type MigrationResult struct {
    ObjectID         uuid.UUID         // Object being migrated
    FromVersion      string            // Source schema version
    ToVersion        string            // Target schema version
    Success          bool              // Overall success flag
    MigratedProps    []string          // Fields successfully migrated
    DroppedProps     []string          // Fields removed from schema
    AddedProps       []string          // New optional fields added
    CoercedProps     []string          // Fields requiring type conversion
    Issues           []MigrationIssue  // Problems encountered
    NewProperties    map[string]any    // Migrated property values
}

// Check migration outcome
if result.Success {
    fmt.Printf("✅ Migration successful\n")
    fmt.Printf("   Migrated: %d fields\n", len(result.MigratedProps))
    fmt.Printf("   Coerced: %d fields\n", len(result.CoercedProps))
    fmt.Printf("   Dropped: %d fields\n", len(result.DroppedProps))
} else {
    fmt.Printf("❌ Migration failed\n")
    for _, issue := range result.Issues {
        fmt.Printf("   %s: %s\n", issue.Type, issue.Description)
        fmt.Printf("   💡 %s\n", issue.Suggestion)
    }
}
```

## Common Migration Scenarios

### 1. Adding Optional Fields

**Safe** - No action required

```json
// v1.0.0
{"name": "John", "age": 30}

// v2.0.0 adds optional "department"
{"name": "John", "age": 30}  // ✅ Still valid
```

### 2. Adding Required Fields

**Requires action** - Provide default values

```json
// v1.0.0
{"name": "John", "age": 30}

// v2.0.0 makes "email" required
{"name": "John", "age": 30}  // ❌ Missing required field
```

**Solution**: Update schema or provide defaults:

```go
// Option 1: Populate before migration
for _, obj := range objects {
    if obj.Properties["email"] == nil {
        obj.Properties["email"] = "unknown@example.com"
    }
}

// Option 2: Make field optional temporarily
v2Schema.Required = []string{"name"}  // Remove "email"
```

### 3. Type Coercion

**Auto-handled** when conversion is possible

```json
// v1.0.0: age as string
{"name": "John", "age": "30"}

// v2.0.0: age as number
{"name": "John", "age": 30}  // ✅ Auto-coerced
```

**Fails** when conversion impossible:

```json
// v1.0.0
{ "name": "John", "age": "thirty" }

// v2.0.0: age as number
// ❌ Cannot coerce "thirty" to number
```

### 4. Removing Fields

**Auto-handled** - Data dropped with warning

```json
// v1.0.0
{"name": "John", "age": 30, "old_field": "deprecated"}

// v2.0.0: "old_field" removed
{"name": "John", "age": 30}  // ⚠️ old_field dropped
```

### 5. Renaming Fields

**Requires manual migration**

```json
// v1.0.0
{"full_name": "John Doe"}

// v2.0.0: renamed to "name"
{"name": "John Doe"}  // ❌ Need custom logic
```

**Solution**: Pre-migration script:

```go
for _, obj := range objects {
    if fullName, ok := obj.Properties["full_name"]; ok {
        obj.Properties["name"] = fullName
        delete(obj.Properties, "full_name")
    }
}
```

## Best Practices

### 1. Always Dry Run First

```bash
# Preview changes
./bin/migrate-schema -project ... -from 1.0.0 -to 2.0.0

# Review output carefully
# Then run live migration
./bin/migrate-schema -project ... -from 1.0.0 -to 2.0.0 -dry-run=false
```

### 2. Backup Before Migration

```bash
# Backup project data
pg_dump -h localhost -U emergent emergent \
  --table=kb.graph_objects \
  --table=kb.graph_relationships \
  > backup_$(date +%Y%m%d_%H%M%S).sql
```

### 3. Incremental Migrations

Migrate one version at a time:

```bash
# ✅ Good: Step-by-step
./bin/migrate-schema ... -from 1.0.0 -to 1.1.0
./bin/migrate-schema ... -from 1.1.0 -to 2.0.0

# ❌ Risky: Skip versions
./bin/migrate-schema ... -from 1.0.0 -to 3.0.0
```

### 4. Schema Design for Migration

**Backwards-compatible changes:**

- ✅ Add optional fields
- ✅ Remove optional fields
- ✅ Widen types (string → number often works)

**Breaking changes:**

- ❌ Add required fields without defaults
- ❌ Remove required fields
- ❌ Narrow types (number → string may lose precision)
- ❌ Rename fields

### 5. Monitoring Migration Health

```bash
# Check schema version distribution
SELECT schema_version, COUNT(*) as count
FROM kb.graph_objects
WHERE project_id = 'a1b2c3d4-...'
GROUP BY schema_version;

# Output:
# schema_version | count
# ---------------+-------
# 1.0.0          | 150
# 2.0.0          | 200
# 3.0.0          | 50
```

## Rollback Strategy

If migration fails:

```sql
-- Restore from backup
psql emergent < backup_20260210_143022.sql

-- Or manual rollback
UPDATE kb.graph_objects
SET
  schema_version = '1.0.0',
  properties = (SELECT properties FROM backup_table WHERE id = graph_objects.id)
WHERE project_id = 'a1b2c3d4-...'
  AND schema_version = '2.0.0';
```

## Testing Migrations

```go
func TestMigration_v1_to_v2(t *testing.T) {
    migrator := graph.NewSchemaMigrator(validator, logger)

    // Test successful migration
    obj := createTestObject("1.0.0")
    result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

    assert.True(t, result.Success)
    assert.Equal(t, expectedProps, result.NewProperties)

    // Test failure cases
    objWithBadData := createObjectWithInvalidAge()
    result = migrator.MigrateObject(ctx, objWithBadData, v1Schema, v2Schema, "1.0.0", "2.0.0")

    assert.False(t, result.Success)
    assert.Contains(t, result.Issues[0].Description, "coercion failed")
}
```

## Troubleshooting

### Issue: "New required field" error

**Cause**: New schema adds required field that doesn't exist in old objects

**Solution**:

```go
// Add default values before migration
UPDATE kb.graph_objects
SET properties = properties || '{"email": "unknown@example.com"}'::jsonb
WHERE schema_version = '1.0.0'
  AND NOT (properties ? 'email');
```

### Issue: "Coercion failed" error

**Cause**: Cannot auto-convert incompatible types

**Solution**:

```go
// Fix data manually
UPDATE kb.graph_objects
SET properties = jsonb_set(
  properties,
  '{age}',
  to_jsonb(CAST(properties->>'age' AS integer))
)
WHERE schema_version = '1.0.0'
  AND properties->>'age' ~ '^[0-9]+$';
```

### Issue: Migration tool hangs

**Cause**: Large dataset, default batch size too big

**Solution**:

```bash
# Reduce batch size
./bin/migrate-schema ... -batch 10
```

## API Reference

See `apps/server-go/domain/graph/migration.go` for full API documentation.
