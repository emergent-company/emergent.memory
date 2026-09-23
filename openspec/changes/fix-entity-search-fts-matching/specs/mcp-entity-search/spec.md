## Purpose

The `entity-search` MCP tool matches entities through the GIN-indexed full-text vector on `kb.graph_objects.fts` rather than leading-wildcard `ILIKE` scans, so matching is lexeme-based rather than substring-based.

## ADDED Requirements

### Requirement: Entity search matches the indexed full-text vector

The `entity-search` MCP tool SHALL match entities with a single full-text predicate over `kb.graph_objects.fts` using both the `simple` and `norwegian` query configurations, and MUST NOT use a leading-wildcard `ILIKE` predicate or a separate exact-key predicate. The predicate is the only match condition, so the planner can serve it with `idx_graph_objects_fts` (GIN). Type, namespace, branch and system-exclusion filters, ordering (`created_at DESC`) and the `SearchEntitiesResult` response shape are unchanged.

#### Scenario: Name match through the index
- **WHEN** a client searches for a term present in an entity's `name`
- **THEN** the entity is returned
- **AND** the query plan uses `Bitmap Index Scan on idx_graph_objects_fts` rather than a sequential scan

#### Scenario: Response shape preserved
- **WHEN** entities match
- **THEN** each result carries `id`, `key`, `name`, `type`, `created_at` and branch information exactly as before

### Requirement: Searchable fields are key, type, title, name and description

The indexed vector SHALL contain the entity's raw and separator-normalised `key`, its `type`, and a bounded `norwegian` prose space over `title`, `name` and `description`. A matching term in any of those fields SHALL return the entity.

#### Scenario: Title is searchable
- **WHEN** a client searches for a term present only in an entity's `title` property
- **THEN** the entity is returned

#### Scenario: Type is an implicit index field
- **WHEN** a client searches for an entity's type name as free text
- **THEN** entities of that type are returned

### Requirement: Composite keys are searchable whole and by component

A composite key such as `lov/1997-06-13-44` SHALL be reachable both by its exact form and by its space-separated components (`lov 1997 06 13 44`).

#### Scenario: Exact composite key
- **WHEN** a client searches for the exact key `lov/1997-06-13-44`
- **THEN** the entity with that key is returned

#### Scenario: Component tokens
- **WHEN** a client searches for `lov 1997 06 13 44`
- **THEN** the entity with key `lov/1997-06-13-44` is returned

### Requirement: Matching is lexeme-based, not substring-based

A query term SHALL match only an indexed lexeme. Substring, mid-word, prefix and numeric-substring matches SHALL NOT be returned.

#### Scenario: Substring does not match
- **WHEN** a client searches for `ali`
- **THEN** an entity named "Alice Anderson" MUST NOT be returned

#### Scenario: Numeric substring does not match
- **WHEN** a client searches for `1997`
- **THEN** an entity named "Paragraf 11997" MUST NOT be returned

### Requirement: Strict query falls back to a relaxed form once

The tool SHALL run the strict full-text query first and, only when it returns no rows, retry once with `ftsquery.Relax` (dropping phrases and purely numeric terms) before reporting no results. The relaxed form MUST NOT be used when it would strip or invert `websearch_to_tsquery` operator syntax.

#### Scenario: Relaxed retry rescues an unsatisfiable identifier
- **WHEN** a client searches for `aksjeloven 9999-99-99-99`, a term set the strict query cannot satisfy
- **THEN** the entity matching `aksjeloven` is returned by the relaxed retry

#### Scenario: Strict match is not relaxed
- **WHEN** the strict query returns at least one row
- **THEN** no relaxed query is run

### Requirement: Queries with no lexemes return no results

A query containing only stop words, punctuation, whitespace, or nothing SHALL be treated as matching no entities rather than every entity.

#### Scenario: Stop-word-only query
- **WHEN** a client searches for `og`
- **THEN** no entities are returned

#### Scenario: Whitespace-only query
- **WHEN** a client searches for whitespace
- **THEN** no entities are returned

### Requirement: Arbitrary query text is safe

Query text containing `tsquery` metacharacters (`&`, `|`, `!`, `:`, `*`, parentheses) SHALL NOT raise a syntax error, and SHALL NOT be interpreted as executable query syntax beyond what `websearch_to_tsquery` defines.

#### Scenario: Metacharacters do not error
- **WHEN** a client searches for `foo & bar` or `(((` or `foo:bar`
- **THEN** the query executes without error and returns a normal result set
