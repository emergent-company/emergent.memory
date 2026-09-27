## Context

`GET /api/graph/objects/search` returns `GraphObjectResponse` items whose `properties` map is passed through verbatim. An include-only projection (`fields`) already exists and is applied in the list service after the response objects are built. Issue #1106 shows the cost is dominated by serializing/transferring large properties, not by the SQL.

## Goals / Non-Goals

- **Goals**: let callers drop named property keys from list responses; make the objects-browser list small.
- **Non-Goals**: changing default responses; SQL-level projection; a schema-aware notion of "large" fields.

## Decisions

- **Parameter name `exclude_fields`**, comma-separated, mirrors the existing `fields` include parameter and its parsing.
- **Applied at the same projection step as `fields`**, via the existing `projectProperties` helper, so include+exclude compose without new serialization logic.
- **Gateway passes a literal `content`.** The objects browser list renders no properties, so dropping the document body is safe; keeping the change to the caller (not the API default) avoids breaking agents/CLI that rely on full properties.

## Risks / Trade-offs

- Callers that need an excluded key get a silently smaller map. Mitigated by opt-in (no parameter ⇒ unchanged) and by documenting the parameter.
- In-memory projection still reads the full row from Postgres; the DB→app read is unavoidable without SQL projection (deferred).

## Migration Plan

None — additive query parameter and a client-side change.
