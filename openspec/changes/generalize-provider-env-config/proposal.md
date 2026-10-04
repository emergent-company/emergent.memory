## Why

The environment-variable-only LLM configuration path hardcodes four providers
(`DEEPSEEK_*`, `OPENAI_*`, `VERTEX_AI_*`/`GOOGLE_API_KEY`) in `pkg/adk/model.go`
and `internal/config`. The vendor registry merged in #1455/#1458 now describes
~27 vendors, but a self-host operator who has no database provider config cannot
use any of them: `CreateModelWithName` falls through a four-case `switch` and
rejects every other vendor.

This is the deferred follow-up (task 4.5) of the `multivendor-provider-registry`
change: generalize the env-var path onto the registry while keeping the existing
four providers working exactly as before.

## What Changes

- Add a registry-driven env resolver. For any registered vendor, resolve
  `<PREFIX>_API_KEY`, `<PREFIX>_BASE_URL`, and `<PREFIX>_MODEL`, where `PREFIX`
  is the vendor dialect upper-cased with `-` replaced by `_` (deterministic,
  `UPPER_SNAKE`).
- Keep the four legacy providers' env vars, defaults, and protocol selection
  unchanged (`DEEPSEEK_API_KEY`/`DEEPSEEK_MODEL`, `OPENAI_API_KEY`/
  `OPENAI_BASE_URL`/`OPENAI_MODEL`, `GOOGLE_API_KEY`, `GCP_PROJECT_ID` +
  `VERTEX_AI_LOCATION`).
- Replace the hardcoded per-provider env creators with one protocol-driven
  creator, so a registry vendor needs no code change to be env-configurable.
- A vendor with no API key is not enabled — a base URL or model alone never
  enables it.
- Preserve precedence: database provider configuration (from
  `project_provider_configs`) is used first; env is a fallback only when no
  stored credential exists.
- `ModelFactory.IsEnabled`/`ModelName` recognize a configured non-legacy vendor
  (env key + model) so the no-DB-resolver path can select it.
- Document the generic convention in the server env docs and `.env.example`.

## Impact

**Server (`apps/server/`)**
- `pkg/adk/env_vendors.go` (new): `EnvPrefix`, `EnvVendor`, `EnvVendorResolver`,
  `EnvVendorConfig`, the resolver, and the protocol-driven env model creator.
- `pkg/adk/model.go`: `ModelFactory` gains an optional registry resolver and an
  env getter; the `providerHint` switch and per-provider creators are replaced;
  `ModelName`/`IsEnabled` gain generic vendor discovery.
- `domain/provider/env_vendor_adapter.go` (new) + `module.go`: expose the vendor
  `Registry` to `pkg/adk` as an `adk.EnvVendorResolver` (avoids the import
  cycle).
- No database migration.

**Docs**
- `apps/server/README.md` and `.env.example` document the generic convention.

## Non-goals

- No new provider config storage, no schema/API change; the database path is
  untouched apart from remaining first in precedence.
- Embedding env-variable generalization is out of scope (this change covers the
  chat/generative `ModelFactory` path).
