## Why

`entity-query` selected the full `properties` JSONB for every row. In projects whose objects carry large text blobs (statute text, ~14 KB/row), a `limit=200` query returned multiple MB of payload and took ~218s (issue #1069), contributing to agent-run timeouts. The other read/search tools already project properties via `field_strategy`; `entity-query` did not.

## What Changes

- `entity-query` gains a `field_strategy` input (`compact` | `minimal` | `full`), matching `search-hybrid`.
- The type/pagination path defaults to `compact` (name only, no `properties`), so the wide JSONB column is no longer selected by default. Callers opt into properties with `field_strategy="full"`.
- The `ids[]` fast-path keeps its documented "fetch full properties of a specific version" behaviour: it defaults to `full` but still honours an explicit override.
- The existing `fields` projection applies only under `full`.

## Capabilities

### Modified Capabilities
- `mcp-tool-results`: entity-query's properties projection is now explicit (`field_strategy`), consistent with `search-hybrid`.
