## Context

`ProviderDefinition.ExtraFields` drives dynamic provider inputs, but the only consumer was the read-only providers form. `ResolvedCredential.Extra` already existed and was copied into `pkg/adk.ResolvedCredential.Extra`, but nothing populated or used it. There was no storage column, no DTO field, and no request-time application.

## Decisions

### Storage: jsonb for non-secret, encrypted blob for secret
Non-secret extra values go in a `jsonb NOT NULL DEFAULT '{}'` column (`extra_config`) so they are queryable and forward-compatible. Secret values (`ExtraField.Secret`) go in `extra_secret_encrypted bytea` + `extra_secret_nonce bytea`, encrypted with the same `crypto.Encryptor` used for the primary credential. The secret map is serialized to JSON before encryption. Secrets never touch the plaintext column, so a DB reader cannot recover them.

Alternatives considered: fold secrets into the existing credential payload. Rejected because the credential payload's format is per-vendor (API key vs service-account JSON) and folding would require format detection and migration of existing rows.

### Omit-on-update semantics
`extraConfig` follows the existing credential/model omit-on-update convention:
- `nil` (key omitted) → preserve every stored extra.
- non-nil → per-key merge; a blank value for a declared secret field preserves the stored secret (the UI never echoes secrets), while a blank non-secret value clears that key.

The gateway enforces the blank-secret rule before the request; the server enforces it again defensively using the registry's `Secret` flags.

### Application points
`applyDefinition` (used by both stored-config decryption and the temp credential built during save) materialises defaults and derives `ExtraHeaders` from fields that declare a `Header`. It also rewrites the resolved base URL for the Volcengine region. Request-time application happens in:
- `pkg/adk/openai_model.go` (chat completions URL + headers),
- `domain/provider/catalog.go` (catalog `/models` fetch, generate test) and the OpenAI-compatible embeddings client,
- `pkg/embeddings/openai` (embedding URL + headers).

### LKEAP secret_key
Tencent's OpenAI-compatible endpoint officially authenticates with `Authorization: Bearer <key>` only. To satisfy the registry's `secret_key` extra field without fabricating signing, the field declares a configurable `Header` (`X-Secret-Key`) applied when set. The mechanism is generic: any `ExtraField` may declare a `Header`.

## Risks / Trade-offs

- Re-encrypting the secret map on every upsert produces fresh ciphertext even when unchanged — acceptable and simpler than change detection.
- Azure's `/openai/v1` path does not require `api-version`; it is only appended when the operator sets the field, matching the legacy-endpoint use case.

## Migration Plan

`00218_provider_extra_config.sql` adds the three columns to both provider config tables (Up) and drops them (Down). Existing rows default to `{}` with no secret blob. Rollback drops the columns; no data migration is required.

## Open Questions

- None blocking.
