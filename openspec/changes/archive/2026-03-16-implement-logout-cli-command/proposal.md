## Why

The existing `memory logout` command only deletes the local credentials file (`~/.memory/credentials.json`). It does not revoke tokens server-side, meaning a stolen token remains valid until it expires naturally. Additionally, there is no way to clear all auth state (API keys, project tokens in `config.yaml`) in a single operation — users switching environments or cleaning up a machine must manually edit config files.

## What Changes

- **OIDC token revocation**: Before deleting local credentials, attempt to revoke the access token and refresh token via the OIDC revocation endpoint discovered from the stored issuer URL. Revocation failures are logged as warnings but do not block local cleanup.
- **`--all` flag**: When passed, also clears `api_key` and `project_token` fields from `~/.memory/config.yaml` (preserving all other config like `server_url`, `project_id`, UI preferences).
- **Improved output**: Show what was cleared (OAuth credentials, API key, project token) so the user knows the full effect.

## Capabilities

### New Capabilities

- `cli-logout-enhancement`: Enhanced logout with OIDC token revocation and `--all` flag for clearing all auth state.

### Modified Capabilities

_(none — no existing specs are affected)_

## Impact

- **Code**: `tools/cli/internal/cmd/auth.go` (logout command), `tools/cli/internal/auth/` (add revocation function), `tools/cli/internal/auth/discovery.go` (add `RevocationEndpoint` to OIDC config)
- **APIs**: Uses standard OIDC revocation endpoint (RFC 7009) on the Zitadel identity provider — no server-side changes needed
- **Dependencies**: No new dependencies; uses `net/http` and existing `auth` package
- **Backward compatibility**: Default behavior (no flags) adds revocation but remains non-breaking — if revocation fails, credentials are still deleted locally as before
