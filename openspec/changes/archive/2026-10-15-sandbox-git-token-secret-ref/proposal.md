## Why

`kb.agent_definitions.sandbox_config` stores a literal `GH_TOKEN` (or `GITHUB_TOKEN`) inside its `env_vars` map. That puts a plaintext git credential both in the database and — because `env_vars` is injected verbatim — in every sandbox container's environment, where an agent can read it via `env` or `workspace_bash`. Issue #1844 splits the fix: part (a) (#1883, already shipped) redacts the token on read; part (b) — this change — stops *storing* it as a plaintext literal on the write path and introduces a secret reference resolved from server environment at provision time.

## What Changes

- The sandbox config gains a `git_token_secret_ref` field: a reference to a server-managed credential (the allowlisted env var name `SANDBOX_GIT_TOKEN`), resolved at provision time — never a plaintext token.
- A reversible migration (00244) relocates any pre-existing literal `GH_TOKEN`/`GITHUB_TOKEN` out of `env_vars` into a server-managed top-level `git_token` field, so the token stops landing in the container environment. The literal value is preserved, not destroyed, and stays resolvable until the operator rotates it to the server-side secret (an external action that stays with #1844).
- The write path (every endpoint that accepts a sandbox config — `CreateDefinition`, `UpdateDefinition`, `SetAgentOverride`, `UpdateSandboxConfig`, and blueprint apply) accepts/stores the reference, keeps the #1883 placeholder round-trip, and rejects a newly-supplied plaintext git token. Normalization lives in one shared entry point (`NormalizeSandboxConfigForWrite`). The generic project-settings endpoint (`SetProjectSetting`) rejects the `agent_override` category outright, so agent overrides have exactly one authoritative write path (`SetAgentOverride`) and no caller can bypass normalization via the key/value settings API.
- Read endpoints keep redacting both the legacy `git_token` field and any secret `env_vars` key; the `git_token_secret_ref` name is not secret and passes through.
- The web UI sandbox form asks for a reference, not a raw token.

## Capabilities

### Modified Capabilities

- `agent-sandbox-workspace`: the checkout token resolution precedence gains the secret-reference tier and the storage/redaction model moves from a literal `env_vars` entry to a reference + server-managed legacy literal.

## Impact

- Server (`apps/server/domain/sandbox/`): config shape, resolution, write-path normalization; `domain/agents/handler.go` write path; migration 00244.
- Web UI (`apps/web-ui/gateway/`): form field + mapping.
