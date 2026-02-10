# Graph Property Type System

## Overview

The graph database now enforces property types defined in template pack schemas. Properties are automatically coerced to the correct type at runtime when creating or updating graph entities.

## Supported Types

| Type      | Description         | Examples                                      |
| --------- | ------------------- | --------------------------------------------- |
| `string`  | Text data           | `"hello"`, `"user@example.com"`               |
| `number`  | Integers or floats  | `42`, `3.14`, `"25"` → `25.0`                 |
| `boolean` | True/false values   | `true`, `"false"` → `false`, `"yes"` → `true` |
| `date`    | ISO 8601 timestamps | `"2024-02-10"` → `"2024-02-10T00:00:00Z"`     |
| `array`   | List of values      | `["a", "b", "c"]`                             |
| `object`  | Nested structure    | `{"key": "value"}`                            |

## Type Coercion

### Numbers

Strings are automatically converted to numbers when possible:

```json
{
  "age": "25"
}
```

Becomes:

```json
{
  "age": 25.0
}
```

**Supported inputs:**

- Numeric strings: `"123"`, `"3.14"`
- Booleans: `true` → `1.0`, `false` → `0.0`
- Numbers: `42` → `42.0`

**Errors:**

- Empty strings
- Non-numeric strings (`"abc"`)

### Booleans

Various string representations are accepted:

```json
{
  "active": "yes"
}
```

Becomes:

```json
{
  "active": true
}
```

**True values:** `"true"`, `"t"`, `"yes"`, `"y"`, `"1"`  
**False values:** `"false"`, `"f"`, `"no"`, `"n"`, `"0"`, `""` (empty string)

### Dates

Multiple date formats are accepted, all normalized to ISO 8601:

```json
{
  "birthday": "01/15/2000"
}
```

Becomes:

```json
{
  "birthday": "2000-01-15T00:00:00Z"
}
```

**Accepted formats:**

- ISO 8601: `"2024-02-10T15:30:00Z"`
- Date only: `"2024-02-10"`
- Date + time: `"2024-02-10 15:30:00"`
- US format: `"01/15/2024"`
- EU format: `"15-01-2024"`

**Output format:** Always ISO 8601 (`YYYY-MM-DDTHH:MM:SSZ`)

## Schema Definition

Template packs define property types in their `object_type_schemas`:

```json
{
  "Person": {
    "description": "A human individual",
    "properties": {
      "name": {
        "type": "string",
        "description": "Full name"
      },
      "age": {
        "type": "number",
        "description": "Age in years"
      },
      "birthday": {
        "type": "date",
        "description": "Date of birth"
      },
      "active": {
        "type": "boolean",
        "description": "Account status"
      }
    },
    "required": ["name"]
  }
}
```

## Validation Behavior

### At Entity Creation

When creating a graph entity with `POST /api/projects/{id}/graph/objects`:

1. Schema is loaded for the entity type
2. Properties are validated against the schema
3. Values are coerced to the correct type
4. Required fields are checked
5. Entity is created with typed properties

### At Entity Update

When patching an entity with `PATCH /api/projects/{id}/graph/objects/{id}`:

1. Properties are merged (existing + new)
2. Merged properties are validated
3. Values are coerced
4. New version is created with typed properties

### Graceful Degradation

If schema loading fails:

- Validation is skipped (logged as warning)
- Entity creation/update proceeds normally
- Properties are stored as-is

### Unknown Properties

Properties not defined in the schema are allowed and passed through unchanged.

## Error Messages

Validation errors return `400 Bad Request` with detailed messages:

```json
{
  "error": {
    "code": "bad_request",
    "message": "property validation failed: Property 'age': invalid number format: abc"
  }
}
```

**Common errors:**

- `Property 'X': invalid number format: Y`
- `Property 'X': invalid boolean value: Y`
- `Property 'X': invalid date format: Y`
- `Required property 'X' is missing`

## Migration Guide

### Existing Data

Existing entities with string-only properties **are not automatically migrated**. They will be validated on the next update.

To migrate existing data:

1. **Option A: Manual update** - PATCH each entity to trigger validation
2. **Option B: Database migration** - Write SQL to cast columns
3. **Option C: Application code** - Batch update script

### Template Pack Updates

When updating template packs to add type information:

```json
{
  "Person": {
    "properties": {
      "age": {
        "type": "string" // OLD - untyped
      }
    }
  }
}
```

Becomes:

```json
{
  "Person": {
    "properties": {
      "age": {
        "type": "number", // NEW - typed
        "description": "Age in years"
      }
    }
  }
}
```

New entities will be validated, existing entities remain unchanged until updated.

## Examples

### Before (String-only)

```json
{
  "type": "Person",
  "properties": {
    "name": "John Doe",
    "age": "25",
    "birthday": "Jan 1, 2000",
    "active": "true"
  }
}
```

### After (Typed with Schema)

Template pack schema:

```json
{
  "Person": {
    "properties": {
      "name": { "type": "string" },
      "age": { "type": "number" },
      "birthday": { "type": "date" },
      "active": { "type": "boolean" }
    }
  }
}
```

Stored entity:

```json
{
  "type": "Person",
  "properties": {
    "name": "John Doe",
    "age": 25.0,
    "birthday": "2000-01-01T00:00:00Z",
    "active": true
  }
}
```

## Implementation Details

### Validation Flow

```
CreateEntity/PatchEntity
  ↓
Load template pack schemas
  ↓
Find schema for entity type
  ↓
Validate each property:
  - Check required fields
  - Coerce to correct type
  - Collect errors
  ↓
Return validated properties or error
```

### Code Structure

- **Validation logic**: `/apps/server-go/domain/graph/validation.go`
- **Service integration**: `/apps/server-go/domain/graph/service.go`
- **Schema provider**: `/apps/server-go/domain/graph/module.go`
- **Template packs**: `/apps/server-go/domain/extraction/template_pack_schema_provider.go`

## Performance Considerations

### Schema Caching

Schemas are cached in memory to reduce database queries:

- **Cache duration**: 5 minutes (TTL)
- **Cache scope**: Per project
- **Thread safety**: sync.RWMutex for concurrent access
- **Cache behavior**: Lazy eviction (checked on access)

**Metrics tracking**:

- Cache hits/misses
- Database load success/errors
- Validation success/errors
- Validation duration

View metrics via service interfaces (for monitoring integration).

### Validation Performance

- Validation overhead: ~1-2ms per entity
- No impact if no template packs are active
- Graceful degradation if schema service is slow
- Metrics track validation timing for monitoring

## Migration Tool

A CLI tool is available to bulk validate and convert existing graph entities to typed properties:

```bash
# Preview changes (dry run)
./bin/validate-properties -project <uuid>

# Apply changes
./bin/validate-properties -project <uuid> -dry-run=false

# Process in batches
./bin/validate-properties -project <uuid> -batch-size=50
```

**Features**:

- Scans all graph objects in a project
- Validates properties against schema
- Shows preview of changes before applying
- Batch processing for large datasets
- Progress reporting
- Error summary

See CLI help for full usage: `./bin/validate-properties -h`

## Future Enhancements

1. **Custom validators** - Regex patterns, min/max, enums
2. **Array item types** - Validate items in arrays
3. **Nested object schemas** - Recursive validation
