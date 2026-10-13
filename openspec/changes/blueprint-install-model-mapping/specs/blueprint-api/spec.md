## MODIFIED Requirements

### Requirement: GitHub URL blueprint import

The API SHALL import a blueprint from a GitHub repository URL: fetch the
archive, extract and validate its manifest, and create and publish a blueprint
through the existing create/publish service path, scoped to the caller's
project (global only for an authorized superadmin), matching the existing
blueprint-create authorization guards.

The endpoint is `POST /api/blueprints/import` with body
`{url, ref?, token?, modelOverrides?, apply?}`. `modelOverrides` maps an agent
`name` to a `provider/model` reference (or an empty string to select the project
default). The stored manifest SHALL NOT be rewritten by `modelOverrides`: with
`apply: true` the overrides SHALL be applied at apply time (and persisted
per-project after a successful apply); with `apply: false` they SHALL be
persisted per-project for a later apply, skipped for a global import (which has
no project override scope). When `apply` is true, the import SHALL require a
project context, publish the blueprint, and apply it atomically — on an apply
failure the just-created row SHALL be hard-deleted so a retry re-imports cleanly
(no 409), and the apply error is returned.

When a blueprint with the same `(name, version)` already exists in the caller's
scope, the import SHALL adopt the existing row — refreshing it when it is a
draft, reusing it when published — and then publish and apply as requested,
instead of returning a conflict.

Only `https://github.com/<org>/<repo>` URLs are accepted, with an optional
`#<ref>` fragment and/or `/tree/<ref>` suffix; every other host and all
non-https schemes are rejected. The archive is fetched only from
`https://codeload.github.com/...` (a fixed origin built from the validated
org/repo/ref segments); the final URL and every redirect hop must be https and
on the exact allowlisted host (a redirect to a non-allowlisted host or to a
non-https scheme is refused). The caller-supplied URL must match the exact host
`github.com` (no port is accepted), so no arbitrary host is ever resolved;
additionally, as defence in depth, IP-literal hosts, localhost, and internal
host suffixes are rejected. Private/link-local/loopback/CGNAT/metadata address
ranges are therefore never reachable — not via an address-range check, but
because only the two exact public GitHub hosts (`github.com` for parsing and
`codeload.github.com` for the fetch) are ever dialed. The archive download is
capped at 50 MiB and the request is bounded by a timeout. Extraction rejects
path traversal (`../`), absolute paths, and symlinks/hardlinks escaping the
archive root, and caps the file count at 20000 and total extracted bytes at
200 MiB. The optional `token` is used only as an outbound Authorization header;
it is never persisted, never logged, and never echoed in responses or errors.
Errors map to 400 for an invalid URL/manifest, 413 for an oversize archive, 502
for a fetch failure, and 401/403 for authorization failures.

#### Scenario: Allowlisted URL accepted

- **WHEN** a client sends `POST /api/blueprints/import` with a body whose `url`
  is `https://github.com/<org>/<repo>` (with an optional `#<ref>` and/or
  `/tree/<ref>`)
- **THEN** the request proceeds to fetch from the `codeload.github.com`
  allowlist and does not return 400 for the URL

#### Scenario: Non-github host rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` whose host
  is not `github.com` (for example `https://example.com/org/repo`)
- **THEN** the API returns HTTP 400 and performs no fetch

#### Scenario: Non-https scheme rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` using a
  non-https scheme (for example `http://github.com/org/repo` or
  `git+ssh://github.com/org/repo`)
- **THEN** the API returns HTTP 400 and performs no fetch

#### Scenario: Redirect to non-allowlisted host or non-https refused

- **WHEN** the allowlisted fetch endpoint responds with a redirect whose target
  host is not allowlisted (not `codeload.github.com`), or whose scheme is not
  `https`
- **THEN** the API refuses the redirect, performs no further fetch, and maps the
  failure to HTTP 502

#### Scenario: Oversize archive rejected

- **WHEN** the fetched archive exceeds 50 MiB
- **THEN** the API aborts the download and returns HTTP 413 with no blueprint
  created

#### Scenario: Traversal or symlink archive rejected

- **WHEN** the fetched archive contains a path-traversal entry (`../`), an
  absolute path, or a symlink/hardlink escaping the archive root
- **THEN** extraction is rejected and the API returns HTTP 400 with no
  blueprint created

#### Scenario: Valid manifest creates a published blueprint in the caller's project

- **WHEN** a valid archive with a valid manifest is imported by an authorized
  caller
- **THEN** the API creates and publishes a blueprint scoped to the caller's
  project (global only for an authorized superadmin) and returns success

#### Scenario: Existing name and version is adopted

- **WHEN** a client imports a blueprint whose `(name, version)` already exists in
  its scope
- **THEN** the API adopts the existing blueprint, publishes and applies it as
  requested, and does not return 409

#### Scenario: Model override applied at apply time and persisted per-project

- **WHEN** a client sends `POST /api/blueprints/import` with a `modelOverrides`
  mapping an agent name to a `provider/model` reference
- **THEN** the stored blueprint manifest is unchanged (the pristine upstream
  content), and the override is applied at apply time and persisted per-project;
  an empty-string override clears the model name (project default)

#### Scenario: Model override rewrites the stored manifest

- **WHEN** a client sends `POST /api/blueprints/import` with a `modelOverrides`
  mapping an agent name to a `provider/model` reference
- **THEN** the stored manifest is **no longer** rewritten by the override; the
  override is applied at apply time and persisted per-project (supersedes the
  previous rewrite-at-import behavior)

#### Scenario: Atomic apply rolls back on failure

- **WHEN** a client sends `POST /api/blueprints/import` with `apply: true` and
  the apply fails
- **THEN** the API returns the apply error, leaves no published blueprint row,
  and a retry with the manifest fixed re-imports without a 409

#### Scenario: Atomic apply requires a project context

- **WHEN** a client sends `POST /api/blueprints/import` with `apply: true` but no
  project context
- **THEN** the API returns HTTP 400 and creates nothing

#### Scenario: Private token not leaked

- **WHEN** a client supplies a `token` and the import fails for any reason
- **THEN** the token is used only as an outbound Authorization header and
  appears in neither the response body, any error message, nor the logs

#### Scenario: Unauthenticated rejected

- **WHEN** an unauthenticated client sends `POST /api/blueprints/import`
- **THEN** the API returns HTTP 401 and performs no fetch and creates no
  blueprint

#### Scenario: Insufficient role rejected

- **WHEN** an authenticated caller lacking the required capability sends
  `POST /api/blueprints/import`
- **THEN** the API returns HTTP 403 and performs no fetch and creates no
  blueprint

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

- **WHEN** an applied GitHub blueprint has project model overrides (applied at
  apply time, never baked into the stored manifest) and the upstream version is
  unchanged
- **THEN** the check compares against the raw upstream manifest content recorded
  at import and records `updateAvailable: false`

#### Scenario: A failed check preserves the last known result

- **WHEN** a check fails (network error, deleted source) after a prior successful
  check recorded an update
- **THEN** the previous `updateAvailable` and `latestVersion` are retained and
  only the error and last-checked timestamp are updated

## ADDED Requirements

### Requirement: Project-scoped model overrides

The API SHALL expose a project-scoped store for a blueprint's agent model
overrides, keyed by blueprint name (not blueprint id) so a user's mapping
survives a version upgrade. An override maps an agent `name` to a
`provider/model` reference, where an empty string selects the project default.

#### Scenario: Read stored model overrides

- **WHEN** a client sends `GET /api/blueprints/model-overrides?name=<name>` with
  a project context
- **THEN** the API returns HTTP 200 with `{modelOverrides: {agentName:
  "provider/model"}}`, and an empty object when no overrides are stored

#### Scenario: Write model overrides

- **WHEN** a client sends `PUT /api/blueprints/model-overrides` with
  `{name, modelOverrides}` and a project context
- **THEN** the API upserts the overrides and returns HTTP 200 with the resulting
  stored map

#### Scenario: Model overrides require a project context

- **WHEN** a client sends GET or PUT to `/api/blueprints/model-overrides`
  without a project context
- **THEN** the API returns HTTP 400 and stores nothing

### Requirement: Apply-time model override resolution

Applying a blueprint SHALL resolve the effective model for each manifest agent
with precedence **explicit request override > stored per-project override >
manifest pin**, apply the result to the in-memory manifest before validation and
materialization, and persist the explicit request overrides per-project after a
successful apply. An override value of empty SHALL clear the model name (project
default); an agent with no override SHALL keep its manifest pin.

#### Scenario: Explicit override wins over stored and manifest

- **WHEN** an apply carries an explicit `modelOverrides` entry that differs from
  both a stored per-project override and the manifest's pinned model
- **THEN** the explicit value is applied, and after a successful apply it is
  persisted per-project

#### Scenario: Stored override wins over the manifest pin

- **WHEN** an apply carries no explicit override but a stored per-project
  override exists for an agent
- **THEN** the stored override is applied in place of the manifest's pinned model

#### Scenario: Empty override selects the project default

- **WHEN** an override maps an agent to an empty string
- **THEN** the applied agent's model name is empty (project default) rather than
  the manifest's pinned reference
