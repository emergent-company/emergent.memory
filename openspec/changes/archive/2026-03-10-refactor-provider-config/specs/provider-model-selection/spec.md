## MODIFIED Requirements

### Requirement: Default Model Selection per Provider
The system SHALL auto-select default generative and embedding models from the synced catalog when a provider config is saved without explicit model names. Model selections are stored in the same row as credentials — no separate table or endpoint.

#### Scenario: Auto-select models on credential save
- **WHEN** `PUT /api/v1/organizations/:orgId/providers/:provider` is called without `generativeModel` or `embeddingModel`
- **THEN** the system SHALL sync the model catalog from the provider API
- **AND** SHALL select the top-ranked generative model and top-ranked embedding model from that catalog
- **AND** SHALL store both selections in the `generative_model` and `embedding_model` columns of the config row

#### Scenario: Explicit model names honored
- **WHEN** `PUT .../providers/:provider` is called with explicit `generativeModel` and/or `embeddingModel`
- **THEN** the system SHALL store the provided names without overriding them with catalog defaults

#### Scenario: Catalog fetch fails, static fallback used
- **WHEN** the provider API times out or returns an error during catalog sync
- **THEN** the system SHALL fall back to a statically defined model list
- **AND** SHALL still complete the save successfully

## REMOVED Requirements

### Requirement: Fetching Available Models from Provider APIs (separate write)
**Reason**: Model selection is now part of the credential save — no separate `PUT .../models` endpoint or `organization_provider_model_selections` table. The catalog sync still happens during the credential save, but the storage destination is the unified config row.
**Migration**: Pass `generativeModel` and `embeddingModel` in the `PUT /api/v1/organizations/:orgId/providers/:provider` request body, or omit them to get auto-selected defaults.
