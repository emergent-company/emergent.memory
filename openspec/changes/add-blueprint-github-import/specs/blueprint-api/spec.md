## ADDED Requirements

### Requirement: GitHub URL blueprint import

The API SHALL import a blueprint from a GitHub repository URL: fetch the
archive, extract and validate its manifest, and create and publish a blueprint
through the existing create/publish service path, scoped to the caller's
project (global only for an authorized superadmin), matching the existing
blueprint-create authorization guards.

The endpoint is `POST /api/blueprints/import` with body `{url, ref?, token?}`.
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
capped at 50 MiB and the request
is bounded by a timeout. Extraction rejects path traversal (`../`), absolute
paths, and symlinks/hardlinks escaping the archive root, and caps the file
count at 20000 and total extracted bytes at 200 MiB. The optional `token` is
used only as an outbound Authorization header; it is never persisted, never
logged, and never echoed in responses or errors. Errors map to 400 for an
invalid URL/manifest, 413 for an oversize archive, 502 for a fetch failure, and
401/403 for authorization failures.

#### Scenario: Allowlisted URL accepted

- **WHEN** a client sends `POST /api/blueprints/import` with a body whose `url`
  is `https://github.com/<org>/<repo>` (with an optional `#<ref>` and/or
  `/tree/<ref>`)
- **THEN** the request proceeds to fetch from the `codeload.github.com`
  allowlist and does not return 400 for the URL

#### Scenario: Non-github host rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` whose host
  is not `github.com` (for example, `https://example.com/org/repo`)
- **THEN** the API returns HTTP 400 and performs no fetch

#### Scenario: Non-https scheme rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` using a
  non-https scheme (for example, `http://github.com/org/repo` or
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
