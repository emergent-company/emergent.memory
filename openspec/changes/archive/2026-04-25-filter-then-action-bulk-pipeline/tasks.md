## 1. DTO & Request Types

- [x] 1.1 Add `BulkActionRequest` struct to `domain/graph/dto.go`: filter (types, property_filters, labels), action (enum), value, properties, labels, limit, dry_run
- [x] 1.2 Add `BulkActionResponse` struct: matched, affected, errors, dry_run bool
- [x] 1.3 Define action enum constants: update_status, soft_delete, hard_delete, merge_properties, replace_properties, add_labels, remove_labels, set_labels
- [x] 1.4 Add time-relative shorthand parser: `parseRelativeTime("90d")` → `time.Now().UTC().Add(-90 * 24 * time.Hour)` supporting d/h/M units

## 2. Repository Layer

- [x] 2.1 Add `BulkActionByFilter` method to `domain/graph/repository.go` that builds `UPDATE ... WHERE` / `DELETE ... WHERE` from BulkActionRequest filter
- [x] 2.2 Reuse existing PropertyFilter → SQL translation function; extend with relative time pre-processing
- [x] 2.3 Implement dry_run path: `SELECT COUNT(*) WHERE <filter>` using same WHERE clause builder
- [x] 2.4 Implement each action variant: update_status (SET status=?), soft_delete (SET deleted_at=now()), hard_delete (DELETE), merge_properties (SET properties = properties || ?::jsonb), replace_properties, add_labels, remove_labels, set_labels

## 3. Service & Handler

- [x] 3.1 Add `BulkAction` method to `domain/graph/service.go`: validate limit <= 100000, apply default limit=1000, call repository
- [x] 3.2 Add `BulkAction` handler to `domain/graph/handler.go` with Swagger annotations
- [x] 3.3 Register route `POST /api/graph/objects/bulk-action` in `routes.go`
- [x] 3.4 Write audit journal entry after successful non-dry-run execution (action, filter, matched, affected, actor, timestamp)

## 4. CLI

- [x] 4.1 Add `memory graph objects bulk-update` command with `--type`, `--filter`, `--action`, `--value`, `--properties`, `--limit`, `--dry-run` flags
- [x] 4.2 Add `memory graph objects bulk-delete` command as shorthand for action=hard_delete
- [x] 4.3 Print matched/affected counts; print warning if limit was applied

## 5. Tests

- [x] 5.1 Unit test: relative time parser for d/h/M shorthands
- [x] 5.2 Unit test: limit enforcement (default 1000, max 100000)
- [x] 5.3 Integration test: bulk update_status modifies correct objects and leaves others untouched
- [x] 5.4 Integration test: dry_run returns correct count with zero mutations
- [x] 5.5 Integration test: hard_delete removes matched objects permanently
- [x] 5.6 Integration test: audit log entry written after bulk operation
