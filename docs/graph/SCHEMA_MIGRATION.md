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

### Installation

Built automatically with the server:

```bash
cd apps/server-go
go build -o ./bin/migrate-schema ./cmd/migrate-schema
```

### Usage

```bash
# Dry run (default - shows what would happen)
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0

# Live migration
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0 \
  -dry-run=false

# Custom batch size (default: 100)
./bin/migrate-schema \
  -project a1b2c3d4-5678-90ab-cdef-1234567890ab \
  -from 1.0.0 \
  -to 2.0.0 \
  -batch 50
```

### Output

```
INFO Batch processed batch_size=100 total_processed=100
WARN Migration warning object_id=... field=old_field type=field_removed
     suggestion="Review if data should be migrated to another field..."
ERROR Migration error field=email type=new_required_field
      suggestion="Provide a default value or manually populate..."

=== Migration Summary ===
Mode: DRY RUN (no changes applied)
Total objects:     250
Successful:        200
Failed:            30
Skipped:           20
With warnings:     45
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
