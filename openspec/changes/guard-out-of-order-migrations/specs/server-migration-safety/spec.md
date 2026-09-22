## ADDED Requirements

### Requirement: A PR must not add a migration below the base branch maximum

CI SHALL fail a pull request that adds a migration file whose version number is lower than the maximum migration version present on the pull request's base branch, because a lower-numbered gap hard-blocks Goose startup once the higher-numbered migration has been applied anywhere. The check SHALL consider only migration files added by the pull request and SHALL NOT flag a pull request that adds no migrations, nor one that only renames, reformats, or deletes migration files. The failure message SHALL state the base maximum, each offending file, and the exact remediation: renumber the added migration above the current maximum and state the next free version.

#### Scenario: A low-numbered migration is rejected
- **WHEN** the base branch's highest migration is `00176` and a pull request adds `00170_embedding_indexes_hnsw.sql`
- **THEN** the guard MUST exit non-zero and its message MUST name the offending file and say to renumber above `00176`

#### Scenario: A forward migration is accepted
- **WHEN** the base branch's highest migration is `00176` and a pull request adds `00177_forward_ok.sql`
- **THEN** the guard MUST exit zero

#### Scenario: A pull request with no added migrations is accepted
- **WHEN** a pull request adds no migration files
- **THEN** the guard MUST exit zero with no violations

#### Scenario: A rename or reformat is not treated as an addition
- **WHEN** a pull request only renames or reformats an existing migration file without adding a new one
- **THEN** the order guard MUST NOT report a violation (migration immutability remains a separate check)

### Requirement: Deliberate gap fills have a documented escape hatch

A migration added below the base maximum SHALL be exempt from the order rule when the file itself carries an explicit marker line `-- out-of-order-migration-allowed: <reason>` with a non-empty reason. The marker SHALL be matched case-insensitively. A marker with an empty reason SHALL NOT exempt the file. The failure message SHALL document this escape hatch.

#### Scenario: An exempted gap fill is accepted
- **WHEN** a pull request adds a migration below the base maximum and the file contains `-- out-of-order-migration-allowed: filling the 00170/00171 gap`
- **THEN** the guard MUST exit zero

#### Scenario: A bare marker does not exempt
- **WHEN** an added low migration contains `-- out-of-order-migration-allowed:` with no reason
- **THEN** the guard MUST still report a violation

### Requirement: Goose gap failures report the explicit remediation

When the migration runner encounters Goose's `missing migrations before current version` failure, it SHALL report the missing migration versions and filenames, the database's current version, and the explicit remediation, rather than only the raw error. The remediation SHALL name the out-of-order apply path (`-allow-missing`) and the alternative of renumbering new migrations above the current maximum. The `emergent-migrate` CLI SHALL provide the `-allow-missing` flag for `up` and `up-to`.

#### Scenario: A gap failure names the missing files and the remedy
- **WHEN** `emergent-migrate -c up` fails with `found 2 missing migrations before current version 172` listing `00170` and `00171`
- **THEN** the output MUST list those two versions and files and MUST mention `-allow-missing`

#### Scenario: The remediation flag exists and applies the gap
- **WHEN** `emergent-migrate -c up -allow-missing` is run against a database with a missing lower-numbered migration
- **THEN** Goose MUST be invoked with missing-migration support enabled rather than failing the gap

### Requirement: A version recorded out-of-band is detectable

Recording a migration version without running it (`mark-applied` or a manual `INSERT INTO goose_db_version`) SHALL emit a loud warning stating that post-conditions are not verified and giving the verification query and remediation. The server migration package SHALL provide a read-only verification that detects indexes which are not both valid and ready in the `kb` and `core` schemas — the residue a killed `CREATE INDEX CONCURRENTLY` leaves, which is how a recorded-but-not-applied version manifests. Historical migrations SHALL NOT be rewritten to add this check; verification is additive and operator-runnable.

#### Scenario: mark-applied warns that post-conditions are unchecked
- **WHEN** `mark-applied` records a version
- **THEN** it MUST emit a warning that the version was recorded without running its migration and that post-conditions are NOT verified, including the inspection query and remediation

#### Scenario: An invalid index is detected by verification
- **WHEN** verification runs and an index in `kb` or `core` is not both `indisvalid` and `indisready`
- **THEN** verification MUST report the index and its relation and MUST return an error / non-zero status

#### Scenario: A healthy catalog verifies clean
- **WHEN** verification runs and every index in `kb` and `core` is valid and ready
- **THEN** verification MUST report success and exit zero
