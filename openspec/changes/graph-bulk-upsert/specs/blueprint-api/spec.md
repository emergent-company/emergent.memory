## ADDED Requirements

### Requirement: Blueprint apply reconciles seed data through batched bulk upsert

The blueprint/seed apply path SHALL resolve every seed object key in one batched
multi-key lookup and apply objects and relationships through the bulk object and
relationship endpoints rather than per-key lookups and per-record upserts, so
re-applying a blueprint or seed pack scales with the batch count rather than the
object count.

#### Scenario: All seed keys resolved in one lookup

- **WHEN** the apply path applies a seed pack containing many keyed objects
- **THEN** it issues a batched multi-key lookup for the seed keys instead of one
  lookup per object

#### Scenario: Objects applied via bulk endpoints

- **WHEN** the apply path applies a seed pack's objects
- **THEN** new objects are inserted via the objects bulk-create endpoint and
  already-existing objects (under `--upgrade`) are updated via the objects
  bulk-upsert endpoint

#### Scenario: Relationships applied via bulk upsert

- **WHEN** the apply path applies a seed pack's relationships
- **THEN** the relationships are idempotently applied via the relationships
  bulk-upsert endpoint

### Requirement: Seed reconciliation preserves per-item errors and counters

The apply path SHALL preserve per-item error reporting and the existing
created/updated/skipped/failed counters when switching to the batched bulk-upsert
path, so a bad seed record is reported without aborting the rest of the apply.

#### Scenario: Per-item failure reported without aborting

- **WHEN** one seed object or relationship fails to apply while the rest succeed
- **THEN** the apply reports that record as failed, continues applying the
  remaining records, and returns a summary with the correct created/updated/
  skipped/failed counts

### Requirement: Seed objects forward assignee and status through the bulk path

Seed objects SHALL continue to carry their `assignee` and `status` values, now
forwarded through the bulk-upsert item shape, so board-enabled seed objects still
route to the matching listener on apply.

#### Scenario: Assignee and status forwarded

- **WHEN** a keyed, board-enabled seed object declares `assignee` and `status`
- **THEN** the bulk-upsert item forwards both fields and the applied object
  carries them unchanged
