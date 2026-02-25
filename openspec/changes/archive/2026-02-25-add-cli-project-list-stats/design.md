## Context

The CLI currently provides a basic `emergent projects list` command that queries `GET /api/projects` and displays project names, IDs, and KB purpose. The `emergent projects get` command shows similar information for a single project, but with formatting issues where labels and content are not properly aligned, making it harder to read.

Users often need to check project activity levels, content volume, and installed template packs, currently requiring database queries or navigating to the admin UI.

The backend `/api/projects` endpoint returns `ProjectDTO` objects without statistics or template pack information. The repository layer already filters projects by user membership and organization, making it straightforward to add aggregate queries for statistics and template pack joins.

## Goals / Non-Goals

**Goals:**

- Add `--stats` flag to both `projects list` and `projects get` commands without breaking existing behavior
- Display project statistics inline with each project in the list
- Show installed template packs with names and versions
- Fix alignment issues in `projects get` output (labels and values properly aligned)
- Keep stats queries performant (< 500ms for typical projects)
- Make backend stats capability reusable for future features (admin UI, API clients)

**Non-Goals:**

- Paginating or filtering by stats (e.g., "show projects with > 100 docs")
- Historical stats or trends over time
- Detailed job status breakdown (e.g., failed job reasons) - only counts
- Stats for individual documents or objects
- Detailed template pack configuration or customizations - only names/versions
- Template pack management (install/uninstall) - only display what's installed

## Decisions

### Decision 1: Extend existing `/api/projects` endpoint vs. new `/api/projects/stats` endpoint

**Chosen approach:** Add optional `include_stats=true` query parameter to existing endpoint

**Rationale:**

- Reduces API surface area (one endpoint instead of two)
- Naturally groups project data with stats
- Easier to maintain consistency (same auth, filtering logic)
- Frontend/CLI can fetch everything in one call when needed

**Alternative considered:** New `/api/projects/{id}/stats` endpoint

- Would require N+1 requests for listing multiple projects
- More complex for CLI to coordinate multiple requests

### Decision 2: Stats aggregation strategy

**Chosen approach:** Query stats on-demand with subqueries in single SQL statement

**Rationale:**

- No additional infrastructure needed (no cache layer)
- Always returns accurate real-time counts
- Acceptable performance for typical projects (< 10k objects)
- Subqueries can leverage existing indexes on `project_id`

**SQL pattern:**

```sql
SELECT
  p.*,
  (SELECT COUNT(*) FROM kb.documents WHERE project_id = p.id) AS document_count,
  (SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = p.id AND deleted_at IS NULL) AS object_count,
  (SELECT COUNT(*) FROM kb.graph_relationships WHERE project_id = p.id) AS relationship_count,
  (SELECT COUNT(*) FROM kb.object_extraction_jobs WHERE project_id = p.id) AS total_jobs,
  (SELECT COUNT(*) FROM kb.object_extraction_jobs WHERE project_id = p.id AND status = 'running') AS running_jobs,
  (SELECT COUNT(*) FROM kb.object_extraction_jobs WHERE project_id = p.id AND status = 'pending') AS queued_jobs,
  (SELECT json_agg(json_build_object(
     'name', tp.name,
     'version', tp.version,
     'objectTypes', (SELECT json_object_keys(tp.object_type_schemas)),
     'relationshipTypes', (SELECT json_object_keys(tp.relationship_type_schemas))
   ))
   FROM kb.project_template_packs ptp
   JOIN kb.graph_template_packs tp ON tp.id = ptp.template_pack_id
   WHERE ptp.project_id = p.id AND ptp.active = true) AS template_packs
FROM kb.projects p
WHERE ...existing filters...
```

Note: The actual implementation will need to properly aggregate the keys from the JSONB schemas. Alternative approach using array aggregation:

```sql
(SELECT json_agg(json_build_object(
   'name', tp.name,
   'version', tp.version,
   'objectTypes', ARRAY(SELECT jsonb_object_keys(tp.object_type_schemas)),
   'relationshipTypes', ARRAY(SELECT jsonb_object_keys(tp.relationship_type_schemas))
 ))
 FROM kb.project_template_packs ptp
 JOIN kb.graph_template_packs tp ON tp.id = ptp.template_pack_id
 WHERE ptp.project_id = p.id AND ptp.active = true) AS template_packs
```

**Alternative considered:** Materialized view or cached stats table

- Would require background job to update counts
- Adds complexity and potential staleness
- Overkill for current scale

### Decision 3: CLI output format improvements

**Chosen approach:**

1. For `projects list --stats`: Indent stats under each project
2. For `projects get`: Fix label alignment using consistent width, optionally show stats with `--stats`
3. Template packs shown with nested structure: name@version followed by indented object/relationship types

**Format for `projects list --stats`:**

```
Found 2 project(s):

1. My Project (abc-123-uuid)
   KB Purpose: Track product requirements
   Stats:
     • Documents: 42
     • Objects: 156
     • Relationships: 89
     • Extraction jobs: 5 total, 1 running, 2 queued
     • Template packs:
       - product-requirements@1.0.0
         Objects: Requirement, Feature, Epic
         Relationships: DEPENDS_ON, IMPLEMENTS
       - decisions@1.2.0
         Objects: Decision, Option
         Relationships: HAS_OPTION, SUPERCEDES

2. Another Project (def-456-uuid)
   Stats:
     • Documents: 8
     • Objects: 23
     • Relationships: 12
     • Extraction jobs: 0 total
     • Template packs: none
```

**Format for `projects get` (without --stats):**

```
Project:    My Project (abc-123-uuid)
Org ID:     org-uuid-here
KB Purpose: Track product requirements and decisions
```

**Format for `projects get --stats`:**

```
Project:    My Project (abc-123-uuid)
Org ID:     org-uuid-here
KB Purpose: Track product requirements and decisions

Stats:
  • Documents: 42
  • Objects: 156
  • Relationships: 89
  • Extraction jobs: 5 total, 1 running, 2 queued
  • Template packs:
    - product-requirements@1.0.0
      Objects: Requirement, Feature, Epic
      Relationships: DEPENDS_ON, IMPLEMENTS
    - decisions@1.2.0
      Objects: Decision, Option
      Relationships: HAS_OPTION, SUPERCEDES
```

**Rationale:**

- Clear visual hierarchy with nested indentation
- Fixed-width labels in `get` command improve readability
- Compact but informative
- Easy to scan multiple projects and understand what types are available
- Stats only shown when requested
- Template packs show configuration at a glance (what object/relationship types are available)

**Alternative considered:** Table format (columns for each stat)

- Would be harder to read with varying project name lengths
- Doesn't scale well if we add more stats later

### Decision 4: Backend DTO structure

**Chosen approach:** New `ProjectStatsDTO` embedded in `ProjectDTO`, plus `TemplatePack` slice

```go
type TemplatePack struct {
    Name              string   `json:"name"`
    Version           string   `json:"version"`
    ObjectTypes       []string `json:"objectTypes"`
    RelationshipTypes []string `json:"relationshipTypes"`
}

type ProjectStats struct {
    DocumentCount      int             `json:"documentCount"`
    ObjectCount        int             `json:"objectCount"`
    RelationshipCount  int             `json:"relationshipCount"`
    TotalJobs          int             `json:"totalJobs"`
    RunningJobs        int             `json:"runningJobs"`
    QueuedJobs         int             `json:"queuedJobs"`
    TemplatePacks      []TemplatePack  `json:"templatePacks"`
}

type ProjectDTO struct {
    ID                 string         `json:"id"`
    Name               string         `json:"name"`
    OrgID              string         `json:"orgId"`
    KBPurpose          *string        `json:"kbPurpose"`
    // ... existing fields ...
    Stats              *ProjectStats  `json:"stats,omitempty"` // nil when not requested
}
```

**Rationale:**

- `Stats` is optional pointer - omitted from JSON when nil
- Backward compatible with existing API consumers
- Single DTO handles both modes (with/without stats)
- Template packs include object/relationship type arrays for complete configuration view
- Type name lists extracted from JSONB schema keys in database query

## Risks / Trade-offs

**[Risk]** Stats queries slow down for projects with 100k+ objects
→ **Mitigation:** Monitor query performance; add indexes if needed (`project_id` already indexed on all tables); consider caching strategy in future if this becomes an issue

**[Risk]** Breaking changes for API consumers expecting consistent response shape
→ **Mitigation:** `stats` field is optional (omitted by default); existing clients unaffected

**[Trade-off]** Stats are computed on every request (not cached)
→ **Benefit:** Always accurate real-time counts; acceptable performance for current scale

**[Trade-off]** CLI output becomes longer with stats
→ **Benefit:** Opt-in flag keeps default behavior clean; users who need stats explicitly request them

## Migration Plan

1. Backend changes (no migration needed):

   - Add `ProjectStats` type and embed in `ProjectDTO`
   - Update repository to accept `includeStats bool` parameter
   - Modify SQL query to include subqueries when `includeStats=true`
   - Update handler to parse `include_stats` query param

2. SDK changes:

   - Add `IncludeStats bool` option to `List()` method
   - Update `ProjectDTO` type to include optional `Stats` field

3. CLI changes:

   - Add `--stats` flag to `projects list` command
   - Update output formatting to display stats when present
   - Update help text and examples

4. Testing:

   - Unit tests for stats query correctness
   - E2E tests for CLI flag behavior
   - Performance test with large project (10k+ objects)

5. Rollback strategy:
   - Changes are additive and backward compatible
   - If stats query performance is problematic, disable via feature flag or revert to always returning `stats: nil`

## Open Questions

None - design is straightforward and additive.
