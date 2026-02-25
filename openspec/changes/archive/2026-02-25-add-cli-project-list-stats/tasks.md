## 1. Backend: Add ProjectStats type

- [x] 1.1 Add `TemplatePack` struct to `apps/server-go/domain/projects/entity.go` with fields: Name, Version, ObjectTypes ([]string), RelationshipTypes ([]string)
- [x] 1.2 Add `ProjectStats` struct with fields: DocumentCount, ObjectCount, RelationshipCount, TotalJobs, RunningJobs, QueuedJobs, TemplatePacks ([]TemplatePack)
- [x] 1.3 Add optional `Stats *ProjectStats` field to `ProjectDTO` struct with `json:"stats,omitempty"` tag

## 2. Backend: Update repository layer for List endpoint

- [x] 2.1 Add `includeStats bool` parameter to `List()` method in `apps/server-go/domain/projects/repository.go`
- [x] 2.2 Modify SQL query to include subqueries for document_count, object_count, relationship_count when `includeStats=true`
- [x] 2.3 Add subqueries for total_jobs, running_jobs (status='running'), queued_jobs (status='pending')
- [x] 2.4 Add subquery for template_packs using json_agg with json_build_object including name, version, and arrays from jsonb_object_keys()
- [x] 2.5 Extract object type names from object_type_schemas JSONB using ARRAY(SELECT jsonb_object_keys(...))
- [x] 2.6 Extract relationship type names from relationship_type_schemas JSONB using ARRAY(SELECT jsonb_object_keys(...))
- [x] 2.7 Update result scanning to populate `ProjectStats` fields when includeStats is enabled
- [x] 2.8 Ensure deleted_at IS NULL filter is applied to object_count subquery
- [x] 2.9 Parse template_packs JSON result into []TemplatePack slice with all fields

## 3. Backend: Update service layer

- [x] 3.1 Add `includeStats bool` parameter to `List()` method in `apps/server-go/domain/projects/service.go`
- [x] 3.2 Pass `includeStats` parameter through to repository layer
- [x] 3.3 Ensure Stats field is populated in returned ProjectDTO when includeStats is true

## 4. Backend: Update handler for List endpoint

- [x] 4.1 Parse `include_stats` query parameter in `List()` handler in `apps/server-go/domain/projects/handler.go`
- [x] 4.2 Pass `includeStats` boolean to service layer
- [x] 4.3 Ensure backward compatibility (default includeStats=false when parameter not provided)

## 5. Backend: Update repository layer for Get endpoint

- [x] 5.1 Add `includeStats bool` parameter to `Get()` method in `apps/server-go/domain/projects/repository.go`
- [x] 5.2 Modify SQL query similar to List() to include stats subqueries when `includeStats=true`
- [x] 5.3 Update result scanning to populate `ProjectStats` fields when includeStats is enabled

## 6. Backend: Update service layer for Get endpoint

- [x] 6.1 Add `includeStats bool` parameter to `Get()` method in `apps/server-go/domain/projects/service.go`
- [x] 6.2 Pass `includeStats` parameter through to repository layer

## 7. Backend: Update handler for Get endpoint

- [x] 7.1 Parse `include_stats` query parameter in `Get()` handler in `apps/server-go/domain/projects/handler.go`
- [x] 7.2 Pass `includeStats` boolean to service layer
- [x] 7.3 Ensure backward compatibility (default includeStats=false when parameter not provided)

## 8. SDK: Update client types

- [x] 8.1 Add `TemplatePack` struct to `apps/server-go/pkg/sdk/projects/types.go` with Name, Version, ObjectTypes ([]string), RelationshipTypes ([]string) fields
- [x] 8.2 Add `ProjectStats` struct matching backend structure (include TemplatePacks []TemplatePack with all fields)
- [x] 8.3 Add `Stats *ProjectStats` field to SDK `ProjectDTO` with `json:"stats,omitempty"` tag

## 9. SDK: Update client methods for List

- [x] 9.1 Add `IncludeStats bool` option to `ListOptions` struct in `apps/server-go/pkg/sdk/projects/client.go`
- [x] 9.2 Update `List()` method to append `?include_stats=true` query parameter when option is set
- [x] 9.3 Ensure SDK properly parses optional Stats field in response

## 10. SDK: Update client methods for Get

- [x] 10.1 Add `IncludeStats bool` option to `GetOptions` struct (or create if doesn't exist) in `apps/server-go/pkg/sdk/projects/client.go`
- [x] 10.2 Update `Get()` method to accept options parameter
- [x] 10.3 Update `Get()` method to append `?include_stats=true` query parameter when option is set
- [x] 10.4 Ensure SDK properly parses optional Stats field in Get response

## 11. CLI: Add --stats flag to list command

- [x] 11.1 Add `stats bool` flag to `projects list` command in `tools/emergent-cli/internal/cmd/projects.go`
- [x] 11.2 Update command usage/help text to document --stats flag for list
- [x] 11.3 Pass `IncludeStats: stats` value to SDK client List() call when flag is set

## 12. CLI: Update list command output formatting

- [x] 12.1 Add conditional logic in runListProjects() to display "Stats:" section when project.Stats is not nil
- [x] 12.2 Format document count as "• Documents: X"
- [x] 12.3 Format object count as "• Objects: X"
- [x] 12.4 Format relationship count as "• Relationships: X"
- [x] 12.5 Format extraction jobs with smart display: "• Extraction jobs: X total, Y running, Z queued" (omit running/queued if zero)
- [x] 12.6 Format template packs header as "• Template packs:" or "• Template packs: none" if empty
- [x] 12.7 For each template pack, display indented "- name@version" line
- [x] 12.8 Below each pack, display "Objects: Type1, Type2, Type3" with proper indentation
- [x] 12.9 Below each pack, display "Relationships: REL1, REL2" with proper indentation (or "none" if empty)
- [x] 12.10 Ensure proper indentation (3 spaces for "Stats:", 5 spaces for bullets, 7 spaces for pack details)

## 13. CLI: Add --stats flag to get command

- [x] 13.1 Add `stats bool` flag to `projects get` command in `tools/emergent-cli/internal/cmd/projects.go`
- [x] 13.2 Update command usage/help text to document --stats flag for get
- [x] 13.3 Pass `IncludeStats: stats` value to SDK client Get() call when flag is set

## 14. CLI: Fix get command formatting and add stats display

- [x] 14.1 Update runGetProject() to use fixed-width label formatting (e.g., "Project:", "Org ID:", "KB Purpose:")
- [x] 14.2 Ensure all labels align at same column position (12 chars width recommended)
- [x] 14.3 Add conditional logic to display "Stats:" section when project.Stats is not nil
- [x] 14.4 Display blank line before Stats section
- [x] 14.5 Format stats identically to list command (bullets with same indentation and nested template pack structure)
- [x] 14.6 Handle multiline KB Purpose text with proper continuation indentation

## 15. Testing: Backend unit tests

- [x] 15.1 Add unit test for repository List() with includeStats=false (verify stats not calculated)
- [x] 15.2 Add unit test for repository List() with includeStats=true (verify all stats populated correctly)
- [x] 15.3 Add unit test for repository Get() with includeStats=true
- [x] 15.4 Add unit test verifying object count excludes deleted objects (deleted_at IS NULL)
- [x] 15.5 Add unit test for correct job status filtering (running vs pending)
- [x] 15.6 Add unit test for template packs query (only active=true, with name, version, objectTypes, relationshipTypes)
- [x] 15.7 Add unit test verifying object/relationship type extraction from JSONB schema keys

## 16. Testing: API e2e tests

- [x] 16.1 Add e2e test: GET /api/projects without include_stats returns no stats field
- [x] 16.2 Add e2e test: GET /api/projects?include_stats=false returns no stats field
- [x] 16.3 Add e2e test: GET /api/projects?include_stats=true returns stats field with correct counts
- [x] 16.4 Add e2e test: GET /api/projects/:id?include_stats=true returns stats for single project
- [x] 16.5 Add e2e test verifying stats respect user authorization (only projects user has access to)
- [x] 16.6 Add e2e test verifying template packs include objectTypes and relationshipTypes arrays

## 17. Testing: CLI tests

- [x] 17.1 Add test: CLI list command without --stats flag displays no stats
- [x] 17.2 Add test: CLI list command with --stats flag displays all stats sections including template packs
- [x] 17.3 Add test: CLI get command without --stats flag displays no stats
- [x] 17.4 Add test: CLI get command with --stats flag displays stats
- [x] 17.5 Add test: Stats formatting matches expected output format (indentation, bullets)
- [x] 17.6 Add test: Extraction jobs display logic (omits zero running/queued counts)
- [x] 17.7 Add test: Template packs display shows nested structure with object/relationship types
- [x] 17.8 Add test: Template packs display shows "none" when empty
- [x] 17.9 Add test: Get command label alignment is consistent

## 18. Documentation and polish

- [x] 18.1 Update CLI help text and examples to show --stats usage for both list and get commands
- [x] 18.2 Run `nx run server-go:lint` and fix any issues
- [x] 18.3 Run `nx run emergent-cli:build` and verify successful build
- [x] 18.4 Manual smoke test: Run `emergent projects list --stats` against dev environment
- [x] 18.5 Manual smoke test: Run `emergent projects get <project> --stats` against dev environment
- [x] 18.6 Verify formatting looks good in terminal (alignment, readability)
