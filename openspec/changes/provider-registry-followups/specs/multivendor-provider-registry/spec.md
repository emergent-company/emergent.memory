## ADDED Requirements

### Requirement: Model limits sync applies the models.dev feed

The system SHALL refresh per-model token limits from the models.dev catalog feed. Because the feed no longer carries a model `type` field, the sync SHALL identify entries by their token-limit block (a positive `limit.output`) rather than by a `type` filter, and SHALL read the complete response body — the feed exceeds 4 MB — so the remote limits are applied instead of falling back to the static map on every run. The static map SHALL remain the fallback only when the fetch fails or yields no entries.

#### Scenario: Type-less generative entries are applied

- **WHEN** the models.dev feed returns a model entry with no `type` field and a positive `limit.output`
- **THEN** the sync SHALL include that model's context and output limits

#### Scenario: Entries without an output limit are skipped

- **WHEN** a model entry has no positive `limit.output`
- **THEN** the sync SHALL skip it rather than record a zero or bogus output limit

#### Scenario: A response body larger than 4 MB is parsed

- **WHEN** the models.dev response body exceeds 4 MB
- **THEN** the sync SHALL read and parse the full body rather than truncate it

#### Scenario: Static fallback only when the fetch yields nothing

- **WHEN** the models.dev fetch fails or returns no usable entries
- **THEN** the sync SHALL fall back to the static model-limits map
