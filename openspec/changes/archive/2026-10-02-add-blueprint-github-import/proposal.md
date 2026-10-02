## Why

Blueprints can only be installed from the local registry or the bundled
`blueprints/` directory. The CLI can install a blueprint from a GitHub repo URL,
but the server API and the web UI cannot — a user with only browser access has
no way to install a community blueprint that lives in a GitHub repository.
Refs #1324.

## What Changes

- Add a server API endpoint `POST /api/blueprints/import` that fetches a
  blueprint archive from a GitHub repository URL, extracts and validates its
  manifest, and creates and publishes a blueprint through the existing
  create/publish service path.
- Enforce strict URL acceptance and fetch safety: only `https://github.com/…`
  repository URLs are accepted (exact host, no port); the archive is fetched only
  from the fixed `codeload.github.com` allowlist; every redirect hop must be
  https and stay on that exact allowlist. No address-range check is used: only
  the two exact public GitHub hosts are ever dialed, so private/link-local/
  loopback/CGNAT/metadata ranges are unreachable by construction, and
  IP-literal/localhost/internal hosts are additionally rejected as defence in
  depth. Archive download is capped at 50 MiB with
  a bounded timeout; extraction rejects path traversal, absolute paths, and
  symlinks/hardlinks escaping the archive root, with file-count and total-byte
  caps.
- Add an optional `ref` and an optional `token` to the import body. The token is
  used only as an outbound Authorization header and is never persisted, logged,
  or echoed.
- Add a web UI gallery install surface with a URL field, an optional ref field,
  and an optional token field that posts to the import flow and installs the
  blueprint on success.
- Map errors explicitly: 400 invalid URL/manifest, 413 archive too large, 502
  fetch failure, 401/403 authorization.
- No breaking changes: the surface is additive; existing registry-pack and
  bundled-pack install paths are preserved.

## Capabilities

### New Capabilities

None. The work extends two existing capabilities.

### Modified Capabilities

- `blueprint-api`: gains `POST /api/blueprints/import` with URL acceptance,
  SSRF/allowlist guards, size/timeout limits, safe extraction, token handling,
  and error mapping.
- `blueprint-gallery`: the gallery install surface gains a GitHub URL import
  form (URL, optional ref, optional token) alongside the existing registry and
  bundled install paths.

## Impact

- **Memory API**: new `POST /api/blueprints/import` handler plus a fetch +
  extract + manifest loader service path; reuses the existing
  create/publish service path. URL parsing, allowlist validation, redirect-hop
  enforcement, IP/private-range rejection, size/timeout limits, and safe
  archive extraction are all new server-side code.
- **Gateway** (`gateway/`): new UI form and handler wiring for the gallery
  import surface; changes to `blueprints.go`, `blueprints.templ`,
  `blueprints_handlers.go`, `main.go`.
- **Tests**: TDD — unit tests for every guard case (non-github host, non-https
  scheme, redirect to non-allowlisted host, oversize archive, traversal/symlink
  archive, token non-leak, authz) are the minimum bar; e2e UI coverage is
  optional.

## Out of Scope

- Installing a blueprint from an MCP URL is explicitly out of scope and is a
  follow-up. This change covers GitHub repository URLs only.
