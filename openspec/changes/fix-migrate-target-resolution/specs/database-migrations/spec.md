## Purpose

The `emergent-migrate` command (built from `apps/server/cmd/migrate`, embedded in the server image, and wrapped by `task migrate:*`) must resolve its PostgreSQL target unambiguously, state that target before it acts, and refuse to mutate a database it was not explicitly pointed at. This capability defines the environment-variable precedence, the startup target log, and the fail-closed rule that prevents an unset host from silently applying migrations to `localhost:5432` (issue #754).

## ADDED Requirements

### Requirement: Migration target resolution precedence

The migrate command SHALL resolve its PostgreSQL target from the environment using a single documented precedence order. `DATABASE_URL`, when non-empty, SHALL be a complete override for host, port, user, password, database and sslmode. Otherwise the host SHALL be the first non-empty of `DB_HOST`, `POSTGRES_HOST`, `MEMORY_PG_HOST`; the port the first non-empty of `DB_PORT`, `POSTGRES_PORT`, `MEMORY_PG_PORT`; the user the first non-empty of `POSTGRES_USER`, `MEMORY_PG_USER`; the database the first non-empty of `POSTGRES_DATABASE`, `POSTGRES_DB`, `MEMORY_PG_DB`; and the sslmode the first non-empty of `DB_SSL_MODE`, `POSTGRES_SSL_MODE`. Unset values SHALL fall back to `localhost`, `5432`, `emergent`, `emergent` and `disable` respectively. The password SHALL come from `POSTGRES_PASSWORD` or `DATABASE_URL`, and the command SHALL fail with an error when neither provides one.

#### Scenario: POSTGRES_* aliases are honoured

- **WHEN** `POSTGRES_HOST=db` and `POSTGRES_PORT=6543` are set and `DB_HOST`/`DB_PORT` are unset
- **THEN** the resolved target host SHALL be `db` and port `6543`

#### Scenario: DB_* takes precedence over POSTGRES_*

- **WHEN** `DB_HOST=db-primary` and `POSTGRES_HOST=db` are both set
- **THEN** the resolved target host SHALL be `db-primary`

#### Scenario: DATABASE_URL is a complete override

- **WHEN** `DATABASE_URL` is set alongside `DB_HOST`/`POSTGRES_HOST`
- **THEN** the host, port, user, database and sslmode SHALL be parsed from `DATABASE_URL` and the component variables SHALL be ignored

#### Scenario: Missing password

- **WHEN** no `DATABASE_URL` is set and `POSTGRES_PASSWORD` is empty
- **THEN** the command SHALL exit non-zero with an error naming `POSTGRES_PASSWORD` and `DATABASE_URL` before opening a connection

### Requirement: Resolved target is logged before connecting

The migrate command SHALL print the resolved target to stdout before opening a database connection and before running any migration. The line SHALL include host, port, user, database, sslmode and the name of the environment source that supplied the host (or `built-in default`), and SHALL NOT include the password.

#### Scenario: Target line on every invocation

- **WHEN** any command is run
- **THEN** a line of the form `target: host=<host> port=<port> user=<user> database=<db> sslmode=<ssl> (host source: <source>)` SHALL be printed before the connection is opened

#### Scenario: Password never logged

- **WHEN** `POSTGRES_PASSWORD` is set
- **THEN** the target line SHALL NOT contain the password value or a `password` field

### Requirement: Mutating commands fail closed on ambiguous loopback targets

A mutating command (`up`, `up-to`, `down`, `mark-applied`) SHALL refuse to run when the resolved host is loopback (`localhost`, `127.0.0.1`, `::1`, `0.0.0.0`) and the target is ambiguous, meaning either another host variable (`DB_HOST`, `POSTGRES_HOST` or `MEMORY_PG_HOST`) is set to a non-loopback value that differs from the resolved host, or no host variable was configured at all so the loopback value is the built-in default. The refusal SHALL exit non-zero before opening a connection, name the resolved loopback target and the conflicting variable (when present), and mention the `-allow-localhost` flag. Read-only commands (`status`, `version`, `create`) SHALL NOT be refused. An explicitly and unambiguously configured loopback target SHALL be allowed with a warning, and explicit non-loopback targets SHALL never be refused.

#### Scenario: Unset host no longer silently targets localhost

- **WHEN** `-c up` is run with no `DATABASE_URL`, `DB_HOST`, `POSTGRES_HOST` or `MEMORY_PG_HOST` set
- **THEN** the command SHALL print the resolved loopback target, exit non-zero with a message that no database host was configured, and SHALL NOT open a database connection

#### Scenario: Conflicting host variable is refused

- **WHEN** `DB_HOST=localhost` and `POSTGRES_HOST=db` are set and `-c up` is run
- **THEN** the command SHALL refuse, naming `POSTGRES_HOST=db` and the loopback target

#### Scenario: Explicit loopback is allowed with a warning

- **WHEN** `POSTGRES_HOST=localhost` is set, no other host variable conflicts, and `-c up` is run
- **THEN** the command SHALL proceed and print a warning that the target is loopback and was set explicitly

#### Scenario: Read-only commands are never refused

- **WHEN** `-c status` is run with no host variable set
- **THEN** the command SHALL print the resolved target and proceed to connect rather than refusing

### Requirement: `-allow-localhost` opt-in

The migrate command SHALL provide an `-allow-localhost` boolean flag that permits a mutating command to run against a loopback target regardless of the ambiguity checks, and SHALL print a warning when it is used.

#### Scenario: Opt-in overrides the refusal

- **WHEN** `-c up -allow-localhost` is run with no host variable set
- **THEN** the command SHALL proceed to connect to the loopback target and print a warning that `-allow-localhost` was used
