## Context

The Memory CLI (`tools/cli/`) authenticates via three mechanisms, in priority order:
1. **Project token** (`config.yaml` → `project_token` or `MEMORY_PROJECT_TOKEN` env var) — scoped to a single project
2. **API key** (`config.yaml` → `api_key` or `MEMORY_API_KEY` env var) — account-level
3. **OAuth credentials** (`~/.memory/credentials.json`) — acquired via `memory login` using OIDC Device Authorization Grant against Zitadel

The current `memory logout` command (in `tools/cli/internal/cmd/auth.go`) only deletes `~/.memory/credentials.json`. It does not revoke tokens server-side, and there is no mechanism to clear API keys or project tokens from `config.yaml`.

The OIDC discovery document (`auth.OIDCConfig`) already parses several endpoints but does not include the revocation endpoint. Zitadel exposes a standard RFC 7009 revocation endpoint at `{issuer}/oauth/v2/revoke`.

## Goals / Non-Goals

**Goals:**
- Revoke the OAuth access token and refresh token server-side before deleting local credentials, so stolen tokens cannot be reused
- Provide an `--all` flag that additionally clears `api_key` and `project_token` from `config.yaml`
- Keep the default (no flags) backward-compatible — revocation is best-effort, local deletion always proceeds
- Provide clear output showing what was cleaned up

**Non-Goals:**
- Revoking API keys server-side (they are long-lived account keys managed separately)
- Clearing environment variables (not possible from within the process)
- Implementing session management or multi-account support
- Adding an interactive confirmation prompt (logout is low-risk)

## Decisions

### 1. Revocation endpoint discovery via OIDC

**Decision**: Add `RevocationEndpoint` to the `OIDCConfig` struct and parse it from the OIDC discovery document.

**Rationale**: The revocation endpoint is part of the standard OIDC discovery metadata (`revocation_endpoint` field). Using discovery is consistent with how the login flow already discovers the device authorization and token endpoints. Hardcoding the Zitadel URL would be fragile.

**Alternative considered**: Hardcode `{issuer}/oauth/v2/revoke` — rejected because it couples us to Zitadel's URL structure and breaks if the IdP changes.

### 2. Best-effort revocation with warning

**Decision**: If revocation fails (network error, endpoint not available, non-2xx response), log a warning to stderr and proceed with local credential deletion.

**Rationale**: The primary purpose of logout is to clear local state. Server-side revocation is a security enhancement, not a gate. Blocking logout on network availability would degrade UX. The warning ensures users are informed.

### 3. Revoke both access and refresh tokens

**Decision**: Revoke the refresh token first (higher value target), then the access token. Per RFC 7009, each requires a separate POST.

**Rationale**: The refresh token is the more dangerous credential — it can mint new access tokens. Revoking it first ensures that even if the process is interrupted, the most critical token is invalidated.

### 4. Clear config fields in-place for --all

**Decision**: Load `config.yaml`, zero out only `api_key` and `project_token`, and save back. Preserve all other fields.

**Rationale**: Users customize many config fields (server_url, project_id, UI preferences). A destructive approach (deleting the file) would be hostile. Surgical clearing of auth-related fields is what users expect.

**Alternative considered**: Delete the entire config file — rejected because it destroys non-auth settings.

### 5. Add a Revoke function to the auth package

**Decision**: Create a new `Revoke(issuerURL, clientID, token, tokenTypeHint string) error` function in `tools/cli/internal/auth/` rather than inlining HTTP calls in the command handler.

**Rationale**: Keeps the `auth` package as the single home for all OIDC interactions. Follows the existing pattern where `DiscoverOIDC` and `DeviceFlow` live in the auth package. Makes the revocation logic independently testable.

## Risks / Trade-offs

- **[Risk] Revocation endpoint not in discovery document** → Mitigation: treat missing `revocation_endpoint` as a warning, skip revocation, proceed with local cleanup. This handles non-standard OIDC providers gracefully.
- **[Risk] Revocation takes too long (network timeout)** → Mitigation: use a 10-second HTTP timeout for revocation requests. Users should not wait indefinitely for a best-effort operation.
- **[Risk] Config file has unexpected format or extra fields** → Mitigation: load with `yaml.Unmarshal` into the known Config struct, modify, and save. Unknown fields in the YAML are lost. However, the current `config.Save()` already works this way — no regression.
- **[Trade-off]** Revocation adds network calls to logout. Acceptable because it is best-effort with a short timeout.
