## ADDED Requirements

### Requirement: Rollback consumes only the matched archive entry and preserves newer entries
The rollback operation SHALL remove only the archive entry it matches for the requested `to_version`. Newer archive entries SHALL remain intact, so a multi-hop history stays independently rollback-able.

#### Scenario: Out-of-order rollback preserves newer archive entries
- **GIVEN** an object with archive entries for `to_version: "2.0.0"` and `to_version: "3.0.0"`
- **WHEN** rollback is requested with `to_version: "2.0.0"`
- **THEN** the `to_version: "2.0.0"` entry SHALL be removed from `migration_archive`
- **THEN** the `to_version: "3.0.0"` entry SHALL remain, including its `dropped_data`
