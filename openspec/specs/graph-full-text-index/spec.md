# graph-full-text-index Specification

## Purpose
Defines how the graph-object full-text index (`kb.graph_objects.fts`) is built and how the
graph-object full-text search matches against it, so composite identifiers are searchable
and large objects cannot exhaust the tsvector position budget.

## Requirements

### Requirement: Composite keys are indexed in component form

The index builder SHALL index, alongside the raw `key`:
a separator-normalised form with `/`, `-` and `#` replaced by spaces, and a signed form
with `-` replaced by `" -"` so a numeric component keeps its leading minus.

#### Scenario: Component terms match a composite key

- **WHEN** an object exists with key `lov/1997-06-13-44`
- **THEN** a full-text search for `lov 1997 06 13 44` MUST return that object

#### Scenario: A hyphenated identifier in a larger query matches

- **WHEN** an object exists with key `lov/1997-06-13-44` and prose about `aksjeloven`
- **THEN** the query `aksjeloven lov 1997-06-13-44` MUST return that object
- **AND** it MUST be returned by the strict query, without the relaxed fallback

#### Scenario: The raw identifier still matches

- **WHEN** an object exists with key `forskrift/2007-06-29-876#kapittel-1-paragraf-2`
- **THEN** a full-text search for that exact key MUST return that object

### Requirement: Only a bounded whitelist of fields is indexed

The index builder SHALL derive `fts` from the object key, type, and the `title`, `name` and
`description` properties only, with each prose field bounded, and MUST NOT index the whole
`properties` blob.

#### Scenario: Large objects do not saturate positions

- **WHEN** an object's `description` contains far more distinct tokens than MAXENTRYPOS
  (16383) positions
- **THEN** the resulting `fts` MUST NOT contain a position at the tsvector maximum

#### Scenario: Non-whitelisted property values are not lexical matches

- **WHEN** an object stores a distinctive value under a property that is not
  `title`, `name` or `description`
- **THEN** a full-text search for that value alone MUST NOT match the object

### Requirement: Prose is stemmed, identifiers are literal

The index builder SHALL use the `norwegian` text search configuration for the whitelisted
prose fields and the `simple` configuration for the key and type, concatenated as two
weighted vectors.

#### Scenario: Morphological variants match

- **WHEN** an object's prose contains `aksjeselskaper`
- **THEN** a search for `aksjeselskap` MUST return that object

#### Scenario: Identifier components survive verbatim

- **WHEN** an object has a numeric or punctuated key
- **THEN** its components MUST be present as unstemmed lexemes

### Requirement: Search matches both configurations

The graph-object full-text search SHALL match a row when either the `simple` or the
`norwegian` interpretation of the query matches the stored vector, and SHALL rank by the
greater of the two cover-density scores.

#### Scenario: Either configuration can satisfy the query

- **WHEN** a query's prose terms only match after norwegian stemming
- **THEN** the row MUST still be returned

#### Scenario: The index is used for the match

- **WHEN** the search predicate is evaluated
- **THEN** it MUST be satisfiable from `kb.idx_graph_objects_fts` (a bitmap index scan),
  not only a sequential scan

### Requirement: The migration is reversible and bounded in locking

The migration SHALL backfill existing rows and rebuild the GIN index using
`CREATE INDEX CONCURRENTLY`, and its Down migration SHALL restore the previous trigger
definition and rebuild the index.

#### Scenario: Forward then backward

- **WHEN** the migration is applied and then rolled back on a database
- **THEN** the previous trigger behaviour MUST be restored, the helper function dropped, and
  the GIN index present at each step
