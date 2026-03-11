## 1. Database Migration

- [x] 1.1 Create `apps/server/migrations/00055_add_project_info.sql` — ADD COLUMN `project_info text`, copy `kb_purpose` values, DROP COLUMN `kb_purpose`
- [x] 1.2 Update `apps/server/internal/testutil/schema.sql` — replace `kb_purpose text` with `project_info text` in the `kb.projects` block

## 2. Projects Domain — Backend

- [x] 2.1 `projects/entity.go` — add `ProjectInfo *string` (bun tag `project_info`) to `Project`, `ProjectDTO`, `UpdateProjectRequest`; update `ToDTO()` to map `ProjectInfo`
- [x] 2.2 `projects/entity.go` — remove `KBPurpose` field from `Project`, `ProjectDTO`, `UpdateProjectRequest`, `ToDTO()`
- [x] 2.3 `projects/service.go` — replace `KBPurpose` handling with `ProjectInfo` in `Update()`
- [x] 2.4 `projects/repository.go` — replace `kb_purpose` with `project_info` in the `RETURNING` clause of `Create()` and `Update()`
- [x] 2.5 `projects/repository_test.go` — update `TestToDTO_WithNilKBPurpose` and `TestToDTO_WithKBPurpose` to use `ProjectInfo`

## 3. Discovery Jobs — Migration from kb_purpose

- [x] 3.1 `discoveryjobs/repository.go` — rename `GetProjectKBPurpose()` to `GetProjectInfo()`, update SQL to SELECT `project_info` instead of `kb_purpose`
- [x] 3.2 `discoveryjobs/service.go` — update all call sites of `GetProjectKBPurpose()` to `GetProjectInfo()`

## 4. User Access — Access Tree

- [x] 4.1 `useraccess/service.go` — replace `KBPurpose` with `ProjectInfo` in `ProjectWithRole` struct, `projectRow` scan struct, raw SQL SELECT, and the nil-safe copy block

## 5. MCP — get_project_info Tool

- [x] 5.1 `mcp/service.go` — add `get_project_info` `ToolDefinition` to `GetToolDefinitions()` (name, description, empty inputSchema)
- [x] 5.2 `mcp/service.go` — add `case "get_project_info"` in `ExecuteTool()` switch calling a new `executeGetProjectInfo(ctx, projectID)` method
- [x] 5.3 `mcp/service.go` — implement `executeGetProjectInfo()`: raw `s.db.NewSelect()` query against `kb.projects` for `project_info` by project ID; return markdown text or "No project info has been configured for this project." if null/empty

## 6. SDK

- [x] 6.1 `pkg/sdk/projects/client.go` — replace `KBPurpose *string` with `ProjectInfo *string` in `Project` and `UpdateProjectRequest`
- [x] 6.2 `pkg/sdk/testutil/fixtures.go` — update test fixture to use `ProjectInfo` instead of `KBPurpose`

## 7. API Tests

- [x] 7.1 `tests/api/suites/projects_test.go` — update `TestUpdateProject_PartialUpdate` to use `project_info` instead of `kb_purpose`
- [x] 7.2 `apps/server/tests/e2e/projects_test.go` — same update for the e2e variant

## 8. Frontend — ProjectInfoEditor Component

- [x] 8.1 Create `src/components/organisms/ProjectInfoEditor/ProjectInfoEditor.tsx` — markdown textarea + preview toggle + save via `PATCH /api/projects/:id`; pre-populate with default template when `project_info` is null/empty
- [x] 8.2 Create `src/components/organisms/ProjectInfoEditor/index.ts` — re-export `ProjectInfoEditor`
- [x] 8.3 `src/components/organisms/index.ts` — add export for `ProjectInfoEditor`, remove export for `KBPurposeEditor`
- [x] 8.4 `src/contexts/access-tree.tsx` — replace `kb_purpose?: string` with `project_info?: string` in `ProjectWithRole` interface

## 9. Frontend — Settings Page

- [x] 9.1 `src/pages/admin/pages/settings/project/auto-extraction.tsx` — replace `<KBPurposeEditor>` with `<ProjectInfoEditor>`, update imports
- [x] 9.2 Delete `src/components/organisms/KBPurposeEditor/KBPurposeEditor.tsx` and `src/components/organisms/KBPurposeEditor/index.ts`

## 10. Build & Verify

- [x] 10.1 Run `task build` — confirm Go server compiles with no errors
- [x] 10.2 Run `task test` — confirm unit tests pass
- [x] 10.3 Run `pnpm run lint` in `/root/emergent.memory.ui` — confirm no TypeScript errors
