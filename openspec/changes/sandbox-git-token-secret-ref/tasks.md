## 1. Config shape + resolution

- [x] 1.1 Add `GitTokenSecretRef` and `GitToken` fields to `AgentSandboxConfig` (`sandbox_config.go`).
- [x] 1.2 Add `GitTokenSecretRefEnv`, `IsValidGitTokenSecretRef`, `ResolveGitTokenSecretRef`, and `IsGitTokenEnvKey`; tighten `IsSecretEnvKey` to reuse the git-token helper.
- [x] 1.3 Validate `git_token_secret_ref` against the allowlist in `Validate()`.
- [x] 1.4 Add `ResolveGitTokenForConfig` (ref → legacy literal → env_vars → runtime) and wire it into `auto_provisioner.go` and `workspace_tools.go`.
- [x] 1.5 Unit tests for the new helpers, precedence, and validation.

## 2. Read-side redaction (extend #1883)

- [x] 2.1 Redact the legacy `git_token` field in `RedactSecrets()` and `RedactSandboxConfigMap()`.
- [x] 2.2 Keep `git_token_secret_ref` (a non-secret name) unredacted.
- [x] 2.3 Unit tests for both redaction helpers.

## 3. Write path

- [x] 3.1 Add `NormalizeSecretsForWrite` (placeholder round-trip, carry-forward legacy `git_token`, reject new git-token literals).
- [x] 3.2 Replace the inline placeholder round-trip in `UpdateSandboxConfig` with `NormalizeSecretsForWrite`.
- [x] 3.3 Unit tests covering placeholder round-trip, rejection of new literals, and the carry-forward of the legacy field.
- [x] 3.4 Add `NormalizeSandboxConfigForWrite` (raw-map entry point) as the single shared implementation; `NormalizeSecretsForWrite` delegates to it so typed and map write paths share one code path.
- [x] 3.5 Apply normalization to every write path that accepts a raw sandbox-config map: `CreateDefinition`, `UpdateDefinition`, `SetAgentOverride`, and blueprint apply (`buildAgentDefinition` / `applyAgentManifestToExisting`).
- [x] 3.6 Tests for each of the four paths: a client-supplied `git_token` / `env_vars.GH_TOKEN` literal is rejected and not persisted; the redaction-placeholder round-trip preserves a previously stored secret.
- [x] 3.7 Close the generic settings endpoint bypass: `SetProjectSetting` (PUT `/settings/:category/:key`) rejects the `agent_override` category outright, so agent overrides have exactly one authoritative write path (`SetAgentOverride`). No other settings category (`remember_config`, `feature_flags`) is consumed as a sandbox config.
- [x] 3.8 Test `TestSetProjectSetting_RejectsPlaintextGitToken`: a caller-supplied `sandboxConfig` git token via the generic endpoint is rejected and not persisted (mutation-verified: fails when the guard is removed).

## 4. Migration

- [x] 4.1 Add `00244_sandbox_git_token_secret_ref.sql`: relocate `env_vars.GH_TOKEN`/`GITHUB_TOKEN` into top-level `git_token` (data-preserving, reversible).
- [x] 4.2 Verify migration numbering via `scripts/preflight/migration-number.sh`.
- [x] 4.3 Verification status: the SQL transformation was validated data-preserving against a scratch table and independently confirmed correct on Postgres 16 by review. A live goose `up`/`down` against a real `kb` schema was **not** run (the dev test DB has no `kb` schema — goose version 0), so apply/rollback is unverified in-place. The migration is correct and reversible by construction.

## 5. Web UI

- [x] 5.1 Add `GitTokenSecretRef` / `GitToken` to the gateway `AgentSandboxConfig` DTO.
- [x] 5.2 Add a "Git token (server secret)" reference field to the sandbox form; reject `GH_TOKEN`/`GITHUB_TOKEN` lines in the env-vars textarea.
- [x] 5.3 Regenerate templ + build + existing tests pass.

## 6. Verify

- [x] 6.1 `go build ./...` + `go test ./domain/sandbox/... ./domain/agents/... ./domain/blueprints/...`.
- [x] 6.2 Gateway `go build ./...` + relevant tests.
- [x] 6.3 `openspec validate --all`.

<!-- openspec:archive-ready -->
