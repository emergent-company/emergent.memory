## ADDED Requirements

### Requirement: Provenance-based update detection

A blueprint imported from a GitHub repository SHALL be checked against its
upstream source: the server fetches the upstream manifest at the recorded source
URL/ref, and compares the upstream version and manifest content against the
applied blueprint. An update is reported when the upstream version is strictly
newer, or when the version is unchanged but the manifest content differs.

#### Scenario: Newer upstream version reported

- **WHEN** the upstream repository serves a version newer than the applied
  blueprint (semver-compared where both parse)
- **THEN** the check records `updateAvailable: true` and the newer `latestVersion`

#### Scenario: Same version and content reports no update

- **WHEN** the upstream version and manifest content match the applied blueprint
- **THEN** the check records `updateAvailable: false`

#### Scenario: Same version but changed content reports an update

- **WHEN** the upstream version is unchanged but the manifest content differs
- **THEN** the check records `updateAvailable: true`

#### Scenario: Locally-overridden import is not a false positive

- **WHEN** an applied GitHub blueprint's stored manifest was rewritten by
  project model overrides and the upstream version is unchanged
- **THEN** the check compares against the raw upstream manifest hash recorded at
  import and records `updateAvailable: false`

#### Scenario: A failed check preserves the last known result

- **WHEN** a check fails (network error, deleted source) after a prior successful
  check recorded an update
- **THEN** the previous `updateAvailable` and `latestVersion` are retained and
  only the error and last-checked timestamp are updated

### Requirement: Periodic always-on update check

The server SHALL periodically enqueue one `check` job per applied,
GitHub-imported blueprint, so update status stays current without manual
intervention.

#### Scenario: Sweep enqueues a job per applied GitHub blueprint

- **WHEN** the periodic sweep runs and a project has an applied blueprint whose
  `sourceKind` is `github`
- **THEN** one pending `check` job is enqueued for that (project, blueprint)

#### Scenario: Sweep skips non-GitHub blueprints

- **WHEN** the periodic sweep runs and a project has only non-GitHub applied
  blueprints
- **THEN** no `check` job is enqueued

### Requirement: Durable job queue with retry and backoff

Update-check jobs SHALL be durably stored and claimed with `FOR UPDATE SKIP
LOCKED` so multiple instances can run concurrently without double-claiming.
Failed checks SHALL be retried with exponential backoff (capped), and terminal
failures recorded with an error.

#### Scenario: Job claim is multi-instance safe

- **WHEN** several instances claim pending jobs concurrently
- **THEN** each pending job is claimed by at most one instance

#### Scenario: Failed check retries with backoff

- **WHEN** a check fails before exhausting its attempt budget
- **THEN** the job is returned to pending and is not immediately re-claimable
  (backoff scales with the attempt count)

#### Scenario: Exhausted job fails terminally

- **WHEN** a check keeps failing past the attempt cap
- **THEN** the job is marked failed with a `lastError`

#### Scenario: Orphaned running job is reclaimed

- **WHEN** a job is left `running` past its lease because the process crashed
  or restarted mid-check
- **THEN** it is claimed again by the runner rather than permanently blocking
  that blueprint's future checks

### Requirement: Encrypted project GitHub credential

A project's GitHub personal access token SHALL be stored encrypted at rest and
decrypted only for the outbound fetch. The token SHALL never be serialized in
API responses or written to logs. When no encryption key is configured, PAT
storage SHALL be refused rather than stored unencrypted.

#### Scenario: Token encrypted at rest

- **WHEN** a project stores its GitHub token
- **THEN** the stored row holds only ciphertext and nonce, never the plaintext

#### Scenario: Token never echoed

- **WHEN** a project sets its GitHub token
- **THEN** the API response does not contain the token

#### Scenario: No encryption key refuses storage

- **WHEN** the server has no encryption key configured and a project attempts to
  store a token
- **THEN** the request is refused with a conflict error

### Requirement: Update-status API

The blueprints API SHALL expose the latest update status for the caller's
project: the applied-blueprint list carries update provenance fields, a POST
triggers an immediate check, a GET returns the current status, and a PUT sets the
project GitHub credential.

#### Scenario: Applied list carries update status

- **WHEN** a client requests `GET /api/blueprints/applied`
- **THEN** each entry carries `sourceUrl`, `sourceRef`, `updateAvailable`,
  `latestVersion`, and `lastCheckedAt`

#### Scenario: Trigger a check

- **WHEN** a client sends `POST /api/blueprints/:name/updates/check`
- **THEN** the server enqueues a check job and returns it

#### Scenario: Read the current status

- **WHEN** a client sends `GET /api/blueprints/:name/updates`
- **THEN** the server returns the latest check result for that blueprint name

#### Scenario: Set the project credential

- **WHEN** a client sends `PUT /api/blueprints/github-credentials` with `{token}`
- **THEN** the server stores the encrypted token and returns no content
