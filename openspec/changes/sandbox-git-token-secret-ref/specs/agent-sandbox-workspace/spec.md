## Purpose

Defines the authentication of agent sandbox repository checkout and the storage of the git credential, so private repositories can be cloned without persisting a plaintext token in the database or injecting it into the sandbox container environment.

## MODIFIED Requirements

### Requirement: Repository checkout SHALL authenticate with the configured git token

When a sandbox is provisioned with a repository to check out, the checkout SHALL use the sandbox's configured git token when one is present. The token SHALL be resolved by the documented precedence (first non-empty wins): (1) `git_token_secret_ref` resolved from the server environment under the allowlisted name `SANDBOX_GIT_TOKEN`; (2) the server-managed legacy `git_token` field; (3) `GH_TOKEN` then `GITHUB_TOKEN` in the config's `env_vars` (pre-migration rows); (4) `GH_TOKEN` then `GITHUB_TOKEN` in the runtime env vars. For `https://github.com/` URLs the token SHALL be injected as `https://x-access-token:<token>@github.com/…`. The token SHALL NOT be logged and SHALL NOT appear in returned errors; stderr from the clone SHALL be sanitized. When no token is configured, checkout SHALL fall back to an unauthenticated clone for public repositories.

#### Scenario: A configured GH_TOKEN is used for a private clone

- **GIVEN** the agent definition's `sandbox_config.env_vars` contains `GH_TOKEN` (a pre-migration literal) or `git_token` (a migrated legacy literal)
- **AND** the repository URL is a `https://github.com/` URL
- **WHEN** the workspace is provisioned and the repository is cloned
- **THEN** the clone command uses `https://x-access-token:<token>@github.com/…`

#### Scenario: A git_token_secret_ref resolves from the server environment

- **GIVEN** the agent definition's `sandbox_config.git_token_secret_ref` names `SANDBOX_GIT_TOKEN`
- **AND** the server process has `SANDBOX_GIT_TOKEN` set
- **WHEN** the workspace is provisioned and the repository is cloned
- **THEN** the resolved server token is used for the clone and wins over any legacy literal or runtime env var

#### Scenario: A non-allowlisted secret reference is not honored

- **GIVEN** the agent definition's `sandbox_config.git_token_secret_ref` names an arbitrary environment variable (e.g. `DATABASE_PASSWORD`)
- **WHEN** the workspace is provisioned
- **THEN** the reference resolves to nothing and no arbitrary server environment value is read or injected

#### Scenario: The token is never logged or returned in errors

- **GIVEN** a git token is configured and the clone is attempted (success or failure)
- **WHEN** the checkout service logs progress or returns an error
- **THEN** the token value SHALL NOT appear in any log message or error string

#### Scenario: Public repositories clone unauthenticated when no token is configured

- **GIVEN** no `git_token_secret_ref`, no legacy `git_token`, no `GH_TOKEN`/`GITHUB_TOKEN`, and no GitHub App credential provider is wired
- **WHEN** a public repository is cloned
- **THEN** the clone proceeds with the unauthenticated URL

## ADDED Requirements

### Requirement: The sandbox config SHALL reference, not store, the git token

The stored sandbox config SHALL carry a git token as a reference (`git_token_secret_ref`), never as a newly-written plaintext literal. The only legacy plaintext representation is the server-managed `git_token` field backfilled by migration 00244, which read endpoints SHALL redact and which SHALL NOT be injected into the sandbox container environment. A write that supplies a fresh plaintext git token (via `env_vars.GH_TOKEN`/`GITHUB_TOKEN` or by setting `git_token` directly) SHALL be rejected. A write that echoes the redaction placeholder for a secret SHALL restore the previously stored value rather than overwriting it.

#### Scenario: A newly-supplied plaintext git token is rejected

- **GIVEN** a caller PUTs a sandbox config whose `env_vars` contains a fresh `GH_TOKEN`/`GITHUB_TOKEN` literal
- **WHEN** the config is validated for persistence
- **THEN** the write is rejected with an error directing the caller to `git_token_secret_ref`

#### Scenario: A placeholder echo preserves the stored secret

- **GIVEN** a stored config has a secret env var value
- **AND** a caller reads the (redacted) config and PUTs it back unchanged with the redaction placeholder
- **WHEN** the config is persisted
- **THEN** the stored secret value is preserved, not overwritten with the placeholder

#### Scenario: The legacy literal survives reads and writes

- **GIVEN** a pre-existing config has a legacy `git_token` value (migration 00244)
- **WHEN** the config is read (redacted) or written (placeholder echoed or field omitted)
- **THEN** the legacy value is carried forward, never erased by a normal read→write round-trip, and never returned in plaintext

#### Scenario: The legacy literal is not injected into the container

- **GIVEN** a sandbox config whose git token is stored in the server-managed `git_token` field
- **WHEN** the workspace container is created
- **THEN** the token is not present in the container's environment; it is resolved only at clone time

#### Scenario: The reference name is not treated as a secret

- **GIVEN** a sandbox config with `git_token_secret_ref` set to `SANDBOX_GIT_TOKEN`
- **WHEN** the config is read
- **THEN** the reference name is returned as-is (it is not a secret value and needs no redaction)

#### Scenario: Migration relocates, not destroys, the literal

- **GIVEN** an existing `sandbox_config` whose `env_vars` contains a literal `GH_TOKEN`/`GITHUB_TOKEN`
- **WHEN** migration 00244 runs
- **THEN** the literal is moved to the top-level `git_token` field and removed from `env_vars`; rolling the migration back restores it to `env_vars.GH_TOKEN`
