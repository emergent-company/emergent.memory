# Tasks

## 1. Registry-driven env resolver

- [x] 1.1 Add `pkg/adk/env_vendors.go`: `EnvPrefix` (`UPPER_SNAKE` scheme), `EnvVendor`, `EnvVendorResolver`, `EnvVendorConfig`, `ResolveEnvVendor`, and the protocol-driven `createEnvModel` creator. Verify `go build ./...`.
- [x] 1.2 Expose the vendor registry to `pkg/adk` via `domain/provider/env_vendor_adapter.go` (`EnvVendorAdapter`) and wire it as an optional `adk.EnvVendorResolver` in `domain/provider/module.go`. Verify `go build ./...`.

## 2. Replace the hardcoded env path

- [x] 2.1 Replace the `switch providerHint` and `createDeepSeek/OpenAI/GoogleAI/VertexAIEnv` creators in `pkg/adk/model.go` with the single `createEnvModel` call; keep the four legacy providers' vars, defaults and protocol selection identical. Verify `go test ./pkg/adk/ -count=1`.
- [x] 2.2 Add generic vendor discovery to `ModelFactory.ModelName`/`IsEnabled` so a configured non-legacy vendor is usable on the no-DB-resolver path. Verify `go test ./pkg/adk/ -count=1`.
- [x] 2.3 Generalize the credential-resolver-error fallback so it consults env for any vendor, not only the four legacy ones. Verify `go test ./pkg/adk/ -count=1`.

## 3. Tests

- [x] 3.1 Legacy back-compat: each of the four providers still resolves a model with the same base URL/model defaults and protocol. Verify `go test ./pkg/adk/ -run 'Env|Legacy' -count=1`.
- [x] 3.2 Non-legacy vendor (`ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`) resolves via env, including the Anthropic native protocol. Verify `go test ./pkg/adk/ -count=1`.
- [x] 3.3 Missing key ⇒ vendor not enabled (no accidental enable); unknown vendor errors. Verify `go test ./pkg/adk/ -run Missing -count=1`.
- [x] 3.4 Precedence: DB config beats env; `_BASE_URL`/`_MODEL` overrides apply; foreign model namespaces are preserved. Verify `go test ./pkg/adk/ -count=1`.
- [x] 3.5 Registry adapter returns prefix/protocol/auth/default endpoint per vendor. Verify `go test ./domain/provider/ -run EnvVendor -count=1`.
- [x] 3.6 Mutation-test the generic resolver: flip each guarded branch (prefix derivation, missing-key check, base-URL override, prefix stripping, DB precedence) and confirm the corresponding test goes red, then revert.

## 4. Docs

- [x] 4.1 Document the generic env convention in `apps/server/README.md` and `.env.example`.

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./... && go test -count=1 ./pkg/adk/... ./internal/config/... ./domain/chat/...`.
- [x] 5.2 `bash apps/server/scripts/lint-ratchet.sh`.
- [x] 5.3 `openspec validate generalize-provider-env-config --strict`.
- [ ] 5.4 Archive after merge (`openspec archive generalize-provider-env-config --yes`).

<!-- openspec:archive-ready -->
<!-- 5.4 is the post-merge archive step; declare ready so the archive guard enforces it after this PR merges. -->
