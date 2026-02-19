## Why

The graph API's dual ID model (`ID` = version-specific, `CanonicalID` = stable) causes an entire class of subtle bugs and forces every consumer to build ~200 lines of workaround infrastructure. The field name `ID` universally implies "the identifier" but is actually a version handle that becomes stale after any `UpdateObject` call, while the stable identifier is hidden behind the less prominent name `CanonicalID`. This naming mismatch, combined with no server-side normalization, led to 28+ bug sites across 11 files in SpecMCP alone. Every new Emergent consumer will independently rediscover these pitfalls.

GitHub issues: #43, #44, #45, #46, #47.

## What Changes

- **BREAKING**: Rename `GraphObject.ID` to `VersionID` and `GraphObject.CanonicalID` to `EntityID` to make the stable identifier prominent and the version-specific one clearly scoped (#43)
- **Server-side canonical normalization on write**: `CreateRelationship` resolves endpoint IDs (SrcID, DstID) to their canonical form before storage, so `ListRelationships` works regardless of which ID variant the caller passes (#44)
- **Default deduplication on read**: `ExpandGraph`, `ListObjects`, and `FTSSearch` return only the latest version per entity by default, with an opt-in `WithAllVersions` flag for consumers that need version history (#45)
- **SDK helpers for canonical ID handling**: First-class Go SDK utilities (`IDSet`, `ObjectIndex`, `UniqueObjects`, `GetByAnyID`, `HasRelationship`) so consumers don't need to build their own canonical-aware infrastructure (#46)
- **Dual ID model documentation**: Prominent SDK docs covering the mental model, `UpdateObject` footgun, query dedup patterns, and relationship lookup gotchas (#47)

## Capabilities

### New Capabilities

- `graph-sdk-helpers`: Go SDK utilities for canonical ID handling -- IDSet for canonical-aware comparison, ObjectIndex for O(1) lookup by either ID variant, UniqueObjects for dedup, GetByAnyID for ID-agnostic fetch, HasRelationship for canonical-aware existence checks

### Modified Capabilities

- `graph-api`: **BREAKING** -- GraphObject fields renamed (ID to VersionID, CanonicalID to EntityID) with deprecation aliases; CreateRelationship normalizes SrcID/DstID to canonical form on write; ExpandGraph and ListObjects dedup results by EntityID by default with opt-in WithAllVersions flag
- `graph-search`: FTSSearch deduplicates results by EntityID by default with opt-in WithAllVersions flag; search response objects use new field names

## Impact

- **Go SDK (graph client)**: Breaking field rename on `GraphObject` struct. All consumers must update field references. Deprecation aliases provide migration window.
- **Graph store (server-side)**: `CreateRelationship` adds a canonical ID resolution step before persisting. Existing relationships with version-specific endpoints need a backfill migration.
- **Query endpoints**: `ExpandGraph`, `ListObjects`, `FTSSearch` add server-side dedup logic. New `WithAllVersions` query parameter on all three.
- **All downstream consumers** (SpecMCP, admin UI, any SDK user): Must update to new field names during deprecation period. Can remove ~150-200 lines of workaround code (IDSet, CanonicalizeEdgeIDs, manual dedup loops) once server-side changes land.
- **Database**: Existing relationship rows may need backfill to normalize SrcID/DstID to canonical IDs. One-time migration.
