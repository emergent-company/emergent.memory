## 1. Storage

- [x] 1.1 Add migration `00218_provider_extra_config.sql`: `extra_config jsonb NOT NULL DEFAULT '{}'` + `extra_secret_encrypted`/`extra_secret_nonce` on `kb.project_provider_configs` and `kb.org_provider_configs`, with a real Down.

## 2. Entity, repository, service

- [x] 2.1 Add the three columns to `ProjectProviderConfig`/`OrgProviderConfig` entities and `ExtraConfig` to the request/response DTOs.
- [x] 2.2 Include the new columns in the repository upsert `Set(...)` list (and normalise nil maps).
- [x] 2.3 Merge/encrypt extras on upsert; decrypt into `ResolvedCredential.Extra`; populate defaults, `ExtraHeaders`, and the Volcengine base-URL rewrite in `applyDefinition` and `buildTempResolvedCred`.

## 3. Vendor declarations + apply path

- [x] 3.1 Declare `secret_key` (secret, header) on LKEAP and `region` on Volcengine in `builtin.go`; add `ExtraField.Header`.
- [x] 3.2 Apply `api_version` query param and extra headers in `pkg/adk/openai_model.go`, the catalog HTTP paths, and `pkg/embeddings/openai`.

## 4. SDK, CLI, UI

- [x] 4.1 SDK: `extraConfig` on request + response structs.
- [x] 4.2 CLI: repeatable `--extra key=value` and generic vendor fallback.
- [x] 4.3 Gateway: `extraConfig` client types, editable form inputs, submit/prefill logic.

## 5. Tests

- [x] 5.1 DB round-trip including a secret field (ciphertext-at-rest, plaintext-on-read) and omit/merge semantics.
- [x] 5.2 `applyDefinition` populates `Extra`/`ExtraHeaders` and rewrites the Volcengine region host.
- [x] 5.3 Azure `api_version` yields `?api-version=` on the request URL.
- [x] 5.4 Migration up/down.
- [x] 5.5 Mutation-test the apply path (region substitution and api-version append each fail when neutered).

## 6. Verification

- [x] 6.1 Server: `go build ./...`, `go test ./domain/provider/... ./pkg/adk/... ./pkg/embeddings/...` with `REQUIRE_DB=1`.
- [x] 6.2 Gateway: `templ generate`, `go build ./...`, `go test ./...`.
- [x] 6.3 `lint-ratchet` and `migration-number` preflights.
