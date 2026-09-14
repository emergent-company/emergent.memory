# Surface embedding-provider guidance for generative-only providers

**Status:** done
**Created:** 2026-09-09
**Source:** [2026-09-09-provider-setup-ux](sessions/2026-09-09-provider-setup-ux.md)

## What

When a project has only generative-only providers configured (openai, deepseek —
neither exposes an embedding API), tell the user in the gateway that document
indexing needs a separate embedding-capable provider, and link them to adding
one. Currently memory logs this as an internal warning
(`"<provider> provider configured without embeddings — configure a separate embedding provider for document indexing"` in `emergent.memory/.../domain/provider/service.go`) but the gateway UI is silent.

Candidate surfaces: the Providers page (near Default models / Rates), or the
project-settings area, shown only when ≥1 provider exists but none is
embedding-capable.

## Why

A user who adds an openai provider and expects embeddings to "just work" gets
silent failures or missing indexes; the diagnostics live only in server logs.

## Depends on

none

## Notes

- "Embedding-capable" = any configured provider whose stored embedding model is
  non-empty, or a google/google-vertex provider (embedding test path). DeepSeek
  has no embedding API at all (memory `noEmbedProvider2` covers openai + deepseek).
- Gateway-side only unless a memory API for "does this provider support
  embeddings" is preferred.

## Done (2026-09-10)

- `gateway/settings_providers.go`: added `providerSupportsEmbedding`,
  `providerTypeSupportsEmbedding`, and `providersNeedEmbeddingProvider`. Capability
  is derived strongest-signal-first: a non-empty configured `EmbeddingModel`, a
  cached catalog entry with `ModelType == "embedding"`, then the provider-type
  map (google/google-vertex capable; openai/deepseek not, mirroring memory's
  `noEmbedProvider2`). The type map is centralized in one helper.
- `gateway/project_settings.templ`: `providersPanel` renders
  `embeddingProviderWarning` when ≥1 provider is configured and none is
  embedding-capable. The callout reuses the outline-alert + link-button pattern
  from the agent model warning and links to `/settings/providers/new`. Hidden
  when an embedding-capable provider exists, when no providers are configured,
  or when the provider load errored.
- `gateway/settings_providers_test.go`: unit tests for the capability helper plus
  render tests for the present (openai/deepseek), absent (embedding-capable
  provider present), absent (no providers), and absent (load error) states.
