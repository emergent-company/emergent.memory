## Why

Providers in the vendor registry declare vendor-specific configuration inputs (`ExtraField` — Azure OpenAI `api_version`, Volcengine Ark `region`, LKEAP `secret_key`). They are surfaced read-only in the providers settings UI but are never persisted or applied, so Azure's legacy dated endpoint and Volcengine's rerank region cannot be configured. Today only the Azure undated `/openai/v1` shape works.

## What Changes

- Persist vendor extra config on `kb.project_provider_configs` (and `kb.org_provider_configs`): non-secret fields in a `jsonb` map, secret fields (`ExtraField.Secret`) encrypted with the existing credential envelope in a second ciphertext+nonce pair. Secrets are never stored in plaintext.
- Round-trip extra config through the upsert API (`extraConfig` on the request and public-safe responses), the SDK, the CLI (`--extra key=value`), and the providers settings UI (real inputs replacing the read-only list).
- Populate `ResolvedCredential.Extra` / `Extra` in the credential resolver and apply it at request time:
  - Azure `api_version` → `?api-version=<v>` on chat-completions and embeddings request URLs;
  - Volcengine Ark `region` → base-URL host substitution (`ark.<region>.volces.com`);
  - extra fields that declare an HTTP header (LKEAP `secret_key`) → attached as that header.
- Declare the missing `ExtraField`s for LKEAP (`secret_key`, secret) and Volcengine (`region`).

## Capabilities

### New Capabilities
- `provider-extra-config`: persistence, API round-trip, request-time application, and UI editing of vendor-specific provider extra config fields.

### Modified Capabilities
<!-- none -->

## Impact

- **Memory server** (`apps/server/domain/provider`): migration `00218`, entity/DTO columns, repository upsert, credential service merge + resolution, `builtin.go` field declarations, catalog HTTP paths.
- **ADK runtime** (`apps/server/pkg/adk`, `apps/server/pkg/embeddings/openai`): extra config applied to outbound requests.
- **SDK/CLI** (`apps/server/pkg/sdk/provider`, `apps/cli/internal/cmd/provider.go`): `extraConfig` request/response field and `--extra` flag.
- **Gateway** (`apps/web-ui/gateway`): `extraConfig` on the provider config client types; editable extra-field inputs in the provider form.

## Non-Goals

- No change to the primary credential shape (API key / service-account JSON); extra config is additive.
- No vendor-specific TC3 signing implementation; secret extras are applied as declared headers.
