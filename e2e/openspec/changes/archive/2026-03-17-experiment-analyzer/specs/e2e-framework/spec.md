## ADDED Requirements

### Requirement: experiment_suggestions table in RunDB (migration 5)
`RunDB` SHALL support a fifth migration that creates an `experiment_suggestions` table with columns: `id` (INTEGER PK AUTOINCREMENT), `experiment` (TEXT NOT NULL), `generated_at` (TEXT NOT NULL, RFC3339), `category` (TEXT), `priority` (TEXT), `title` (TEXT NOT NULL), `body` (TEXT NOT NULL), `run_ids` (TEXT, JSON array of integers). An index SHALL be created on `experiment`. The migration SHALL be applied automatically at `SharedDB()` open time and SHALL be idempotent.

#### Scenario: Migration applied on first open
- **WHEN** `SharedDB()` is called on a DB that only has migrations 1–4 applied
- **THEN** migration 5 is applied, `experiment_suggestions` table exists, and `schema_migrations` contains version 5

#### Scenario: Migration idempotent on second open
- **WHEN** `SharedDB()` is called on a DB that already has all 5 migrations applied
- **THEN** no error occurs and the DB schema is unchanged

### Requirement: InsertSuggestion writes to experiment_suggestions
`RunDB` SHALL expose `InsertSuggestion(experiment, title, body, category, priority string, runIDs []int64) (int64, error)` that inserts one row and returns its auto-assigned ID.

#### Scenario: Insert returns new row ID
- **WHEN** `InsertSuggestion` is called with valid arguments
- **THEN** the returned int64 is positive and a row with those values exists in `experiment_suggestions`

### Requirement: ListSuggestions reads suggestions for an experiment
`RunDB` SHALL expose `ListSuggestions(experiment string) ([]SuggestionRow, error)` returning all suggestions for the given experiment, ordered by `id` ascending. `SuggestionRow` SHALL have fields matching the table columns with `RunIDs []int64` decoded from the JSON `run_ids` column.

#### Scenario: Returns empty slice for unknown experiment
- **WHEN** `ListSuggestions` is called with an experiment name that has no suggestions
- **THEN** an empty (non-nil) slice is returned with no error

#### Scenario: Returns all rows ordered by id
- **WHEN** multiple suggestions exist for an experiment
- **THEN** they are returned in ascending `id` order

### Requirement: DeleteSuggestions removes all suggestions for an experiment
`RunDB` SHALL expose `DeleteSuggestions(experiment string) error` that deletes all rows in `experiment_suggestions` where `experiment` matches the argument.

#### Scenario: All rows deleted for experiment
- **WHEN** `DeleteSuggestions("my-exp")` is called and 3 suggestions exist for that experiment
- **THEN** `ListSuggestions("my-exp")` returns an empty slice afterward

#### Scenario: No error when no rows exist
- **WHEN** `DeleteSuggestions` is called for an experiment with no suggestions
- **THEN** no error is returned
