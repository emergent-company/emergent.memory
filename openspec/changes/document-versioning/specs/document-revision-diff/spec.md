## ADDED Requirements

### Requirement: Content-level diff between revisions
The API SHALL return a content diff between two revisions of the same logical document via `GET /api/documents/:id/revisions/diff?from=<version|revisionId>&to=<version|revisionId>`. The diff SHALL be computed over the parsed `content` of both revisions and SHALL include a unified, line-oriented diff with added, removed, and unchanged segments plus counts of added and removed lines. When `from` or `to` is omitted, the API SHALL default `to` to the group's newest revision (the pending revision if one exists, otherwise the current revision) and `from` to the revision immediately preceding it (for a pending revision, its predecessor — normally the current applied revision). Both revisions MUST belong to the same `document_group_id`; a cross-group request SHALL be rejected.

#### Scenario: Diff two parsed revisions
- **WHEN** a client requests a diff between two revisions whose content has been parsed
- **THEN** the API returns a unified line diff and added/removed line counts for that pair

#### Scenario: Default revision pair
- **WHEN** a client requests a diff without specifying revisions and the group has a pending revision
- **THEN** the API diffs the current applied revision against the pending revision

#### Scenario: Cross-group diff rejected
- **WHEN** a client requests a diff where one revision does not belong to the target document's group
- **THEN** the API returns a validation error

#### Scenario: Content not yet parsed
- **WHEN** a client requests a diff where one revision's conversion has not completed
- **THEN** the API returns a conflict-style error explaining that content is not yet available, and no partial diff is returned

### Requirement: Entity delta between revisions
The same diff response SHALL include a structured entity delta derived from the target revision's staged extraction and the main graph. The delta SHALL classify each graph object as `added`, `updated`, or `removed`, matched by `(type, key)` within the project, and SHALL likewise report added and removed relationships. Classification SHALL be computed by comparing the staged extraction's `(type, key)` set and content against the main graph's current heads — it SHALL NOT rely on `canonical_id` equality, because staged objects on a fresh branch carry canonical ids unrelated to the main graph's.

#### Scenario: Added entity in the newer revision
- **WHEN** staged extraction produces a graph object whose `(type, key)` is not present on the main graph
- **THEN** the delta lists that object under `added`

#### Scenario: Updated entity between revisions
- **WHEN** the same `(type, key)` exists on the main graph and in the staged extraction but with different content
- **THEN** the delta lists that object under `updated` and includes the changed properties

#### Scenario: Removed entity
- **WHEN** a main-graph object has provenance from an earlier revision of this group but no provenance from the target revision's chunks, and appears in no staged object
- **THEN** the delta lists that object under `removed`

#### Scenario: No extraction available for a revision
- **WHEN** a diff is requested for a revision that has no staged extraction
- **THEN** the response reports the content diff and an empty entity delta rather than failing
