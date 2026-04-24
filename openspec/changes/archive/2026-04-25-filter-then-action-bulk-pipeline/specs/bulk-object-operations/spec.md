## ADDED Requirements

### Requirement: Bulk action endpoint
The system SHALL expose `POST /api/v1/projects/:project/graph/objects/bulk-action` accepting: `filter` (object with `types[]`, `property_filters[]`, `labels[]`), `action` (enum), `value` (string, for update_status), `properties` (object, for merge/replace), `labels` (array, for label actions), `limit` (integer, default 1000, max 100000), `dry_run` (boolean, default false). Response: `{ matched, affected, errors, dry_run }`.

#### Scenario: Bulk status update
- **WHEN** a bulk-action request is sent with action=update_status, value=archived, and a filter matching 50 objects
- **THEN** all 50 objects MUST have their status updated to "archived" and response MUST show matched=50, affected=50

#### Scenario: Dry run returns count without mutation
- **WHEN** a bulk-action request is sent with dry_run=true
- **THEN** the response MUST return the matched count and affected=0, with no objects modified

#### Scenario: Limit enforced
- **WHEN** a filter matches 2000 objects and limit is not specified
- **THEN** only 1000 objects MUST be affected and the response MUST indicate the limit was applied

#### Scenario: Hard delete action
- **WHEN** action=hard_delete is specified with a valid filter
- **THEN** all matched objects MUST be permanently removed from the graph

#### Scenario: Merge properties action
- **WHEN** action=merge_properties with properties={verified: true} is sent
- **THEN** all matched objects MUST have `verified: true` deep-merged into their JSONB properties

### Requirement: Time-relative filter shorthand
Property filters on datetime fields SHALL accept relative time values: `"Nd"` (N days ago), `"Nh"` (N hours ago), `"NM"` (N months ago). These MUST be evaluated server-side against the current UTC time at request receipt.

#### Scenario: 90d shorthand evaluated correctly
- **WHEN** a filter with `{"path": "created_at", "op": "lt", "value": "90d"}` is submitted
- **THEN** it MUST match all objects created more than 90 days ago

### Requirement: Audit log for bulk operations
The system SHALL write one journal/audit entry per bulk-action request (non-dry-run) containing: action, filter, matched count, affected count, actor ID, timestamp.

#### Scenario: Audit entry created on bulk delete
- **WHEN** a bulk hard_delete affecting 20 objects is executed
- **THEN** one audit log entry MUST be written recording the operation details

### Requirement: Bulk CLI commands
The CLI SHALL expose `memory graph objects bulk-update --type T --filter "expr" --action A --value V [--dry-run]` and `memory graph objects bulk-delete --type T --filter "expr" [--dry-run]` commands.

#### Scenario: CLI dry-run flag
- **WHEN** `memory graph objects bulk-delete --type Message --filter "days_since_access>365" --dry-run` is run
- **THEN** the matched count MUST be printed without deleting any objects
