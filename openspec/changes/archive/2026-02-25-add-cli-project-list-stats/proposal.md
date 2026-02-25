## Why

The CLI `emergent projects list` and `emergent projects get` commands currently show only basic project information with inconsistent formatting. Users need visibility into project activity, content volume (documents, objects, extraction jobs), and installed template packs to quickly assess project health and configuration without opening the admin interface or running manual database queries. The current `projects get` output has misaligned labels and content, making it hard to read.

## What Changes

- Add `--stats` flag to `emergent projects list` command
- Add `--stats` flag to `emergent projects get` command (single project details)
- When enabled, display additional statistics for each project:
  - Number of documents
  - Number of graph objects
  - Number of graph relationships
  - Number of extraction jobs (total, running, queued/pending)
  - Installed template packs with:
    - Pack name and version
    - Object type names defined in the pack
    - Relationship type names defined in the pack
- Enhance backend API to provide project statistics efficiently
- Fix formatting alignment issues in `projects get` command output
- Update CLI output format to display stats in a readable, well-aligned format

## Capabilities

### New Capabilities

- `cli-project-stats`: CLI flag to display project statistics including document counts, object counts, relationship counts, extraction job statuses, and installed template packs. Also includes formatting improvements for better readability.

### Modified Capabilities

<!-- No existing spec requirements are changing - this is all new functionality -->

## Impact

**Code Changes:**

- CLI: `tools/emergent-cli/internal/cmd/projects.go` - Add `--stats` flag and display logic
- Backend API: `apps/server-go/domain/projects/` - Add stats endpoint or enhance list endpoint
- Backend Repository: Add database queries to aggregate counts per project
- SDK Client: `apps/server-go/pkg/sdk/projects/` - Add stats types and methods

**Database Queries:**

- Aggregate counts from `kb.documents`, `kb.graph_objects`, `kb.graph_relationships`, `kb.object_extraction_jobs`
- Join `kb.project_template_packs` with `kb.graph_template_packs` to get installed template pack info including object_type_schemas and relationship_type_schemas JSON
- Extract object type names and relationship type names from JSON schema keys
- All queries filtered by `project_id`
- Job queries filtered by status (`running`, `pending`)
- Template pack queries filter by `active = true`

**Performance Considerations:**

- Stats queries should be optional to avoid slowing down simple list operations
- Consider caching strategy if stats become expensive for projects with large datasets
