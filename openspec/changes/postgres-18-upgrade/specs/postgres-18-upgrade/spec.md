## ADDED Requirements

### Requirement: Automatic pg17-to-pg18 upgrade on `memory server upgrade`
When the installer detects a pg17 data volume during `memory server upgrade`, it SHALL automatically perform an in-place major-version upgrade to pg18 using `pgautoupgrade` before restarting services.

#### Scenario: Existing pg17 installation is upgraded
- **WHEN** a user runs `memory server upgrade`
- **AND** the postgres data volume contains a pg17 data directory (PG_VERSION = 17)
- **THEN** the installer SHALL stop the `db` service
- **AND** pull and run `pgautoupgrade/pgautoupgrade:18-bookworm` with `PGAUTO_ONESHOT=yes` against the data volume
- **AND** on success, proceed to pull `pgvector/pgvector:pg18` and restart all services
- **AND** print a success message confirming upgrade to PostgreSQL 18

#### Scenario: Existing pg16 installation is upgraded (legacy)
- **WHEN** a user runs `memory server upgrade`
- **AND** the postgres data volume contains a pg16 data directory (PG_VERSION = 16)
- **THEN** the installer SHALL trigger the same pgautoupgrade flow
- **AND** pgautoupgrade SHALL handle the multi-hop upgrade (pg16→pg18) internally in a single container run

#### Scenario: Already on pg18 — no upgrade needed
- **WHEN** a user runs `memory server upgrade`
- **AND** the postgres data volume contains a pg18 data directory (PG_VERSION = 18)
- **THEN** the installer SHALL skip the upgrade step
- **AND** print a message confirming PostgreSQL 18 is already up to date

#### Scenario: Fresh install — no existing data volume
- **WHEN** a user runs `memory install` on a machine with no existing postgres data volume
- **THEN** the installer SHALL create a new pg18 database from scratch
- **AND** no upgrade step SHALL be triggered

#### Scenario: Upgrade fails — original data preserved
- **WHEN** `pgautoupgrade` exits with a non-zero status
- **THEN** the installer SHALL restore the backed-up `docker-compose.yml`
- **AND** return an error to the user with instructions to check the output
- **AND** the original data volume SHALL remain intact and usable by the old pg17 container

#### Scenario: Stale lock file from interrupted upgrade
- **WHEN** a previous `pgautoupgrade` run was interrupted and left `upgrade_in_progress.lock` in the data directory
- **AND** the user runs `memory server upgrade` again
- **THEN** the installer SHALL remove the stale lock file before running pgautoupgrade
- **AND** the upgrade SHALL proceed normally

### Requirement: `PostgresMajorVersion` constant drives all version-gated logic
The constant `PostgresMajorVersion` in `tools/cli/internal/installer/templates.go` SHALL be the single authoritative source for the target PostgreSQL major version.

#### Scenario: Version comparison uses the constant
- **WHEN** `detectPostgresVersion` returns a version less than `PostgresMajorVersion`
- **THEN** `RunPostgresUpgrade` SHALL be called
- **AND** no hardcoded version numbers SHALL appear in upgrade decision logic outside of `pg_upgrade.go` and `templates.go`
