## 1. Database Migration

- [x] 1.1 Create `apps/server/migrations/00052_create_skills.sql` with `kb.skills` table (`id`, `name`, `description`, `content`, `metadata jsonb`, `description_embedding vector(768)`, `project_id uuid NULL FK`, `created_at`, `updated_at`)
- [x] 1.2 Add unique index on `(name) WHERE project_id IS NULL` (global uniqueness)
- [x] 1.3 Add unique constraint on `(name, project_id)` (per-project uniqueness)
- [x] 1.4 Add IVFFlat index on `description_embedding` using `vector_cosine_ops` with `lists=100`
- [x] 1.5 Add standard index on `project_id`
- [x] 1.6 Write Down migration that drops the table and all indexes

## 2. Domain — Entity and Store

- [x] 2.1 Create `apps/server/domain/skills/entity.go`: `Skill` Bun model, `SkillDTO`, `CreateSkillDTO`, `UpdateSkillDTO` structs; name validation regex constant
- [x] 2.2 Create `apps/server/domain/skills/store.go`: `Repository` struct with `bun.IDB` + `*slog.Logger`; implement `FindAll(ctx, projectID *string)` for global/project-scoped listing
- [x] 2.3 Implement `Repository.FindForAgent(ctx, projectID string)` — returns merged global + project-scoped skills, project wins on name collision, using a single SQL query with `WHERE project_id = ? OR project_id IS NULL ORDER BY project_id NULLS LAST`
- [x] 2.4 Implement `Repository.FindRelevant(ctx, projectID string, vec []float32, topK int)` — cosine similarity query with `SET LOCAL ivfflat.probes = 10`, filters `description_embedding IS NOT NULL`, ordered by `description_embedding <=> ?::vector ASC`, limited to `topK`
- [x] 2.5 Implement `Repository.FindByID`, `Create`, `Update`, `Delete` methods following existing store patterns (apperror wrapping, schema-qualified table names)

## 3. Domain — Handler

- [x] 3.1 Create `apps/server/domain/skills/handler.go`: `Handler` struct with `*Repository`, `*embeddings.Service`, `*slog.Logger`
- [x] 3.2 Implement `ListGlobalSkills` (`GET /api/skills`) and `CreateGlobalSkill` (`POST /api/skills`) with auth check + embedding generation on create
- [x] 3.3 Implement `GetSkill` (`GET /api/skills/:id`), `UpdateSkill` (`PATCH /api/skills/:id`), `DeleteSkill` (`DELETE /api/skills/:id`) with embedding regeneration on description update
- [x] 3.4 Implement `ListProjectSkills` (`GET /api/projects/:projectId/skills`) returning merged global + project-scoped set via `FindForAgent`
- [x] 3.5 Implement `CreateProjectSkill` (`POST /api/projects/:projectId/skills`), `UpdateProjectSkill`, `DeleteProjectSkill`
- [x] 3.6 Add name slug validation in create handlers (regex `^[a-z0-9]+(-[a-z0-9]+)*$`, 1–64 chars)
- [x] 3.7 Add Swag annotations to all handler methods

## 4. Domain — Skill Tool

- [x] 4.1 Create `apps/server/domain/skills/skill_tool.go`: define `SkillToolDeps` struct (`Repo *Repository`, `EmbeddingsSvc *embeddings.Service`, `Logger *slog.Logger`, `ProjectID string`, `TriggerMessage string`, `AgentName string`, `AgentDescription string`)
- [x] 4.2 Implement `BuildSkillTool(deps SkillToolDeps) (tool.Tool, error)`: call `FindForAgent`, apply threshold logic (≤50 → all, >50 → semantic retrieval), build `<available_skills>` XML for tool description
- [x] 4.3 Implement semantic retrieval path: embed trigger message (fallback to agent name+desc), call `FindRelevant`, handle embedding error by falling back to full list with warning log
- [x] 4.4 Implement `skill({name})` tool handler: exact name lookup from in-memory map built at `BuildSkillTool` time, return `<skill_content name="...">` block or not-found error with available names list
- [x] 4.5 Define `SkillListThreshold = 50` and `SkillTopK = 10` as package-level constants

## 5. Domain — Module and fx Wiring

- [x] 5.1 Create `apps/server/domain/skills/module.go`: `fx.Module("skills", fx.Provide(NewRepository, NewHandler), fx.Invoke(RegisterRoutes))`
- [x] 5.2 Create route registration function that mounts all skills routes on the Echo instance
- [x] 5.3 Add `skills.Module` to the fx app in `apps/server/cmd/server/main.go`

## 6. Executor Integration

- [x] 6.1 Add `skillRepo *skills.Repository` and `embeddingsSvc *embeddings.Service` fields to `AgentExecutor` struct in `executor.go`
- [x] 6.2 Update `provideAgentExecutor` in `agents/module.go` to inject `*skills.Repository` and `*embeddings.Service`
- [x] 6.3 In `runPipeline()`, after workspace tools step: check if `"skill"` is in `AgentDefinition.Tools`; if so, call `skills.BuildSkillTool(deps)` with run context (trigger message, agent name/desc, projectID) and append to tool list; log warn + continue on error

## 7. SDK

- [x] 7.1 Create `apps/server/pkg/sdk/skills/client.go`: define `Skill`, `CreateSkillRequest`, `UpdateSkillRequest`, `ListSkillsResponse` types
- [x] 7.2 Implement `Client` methods: `List(ctx, projectID)`, `Get(ctx, id)`, `Create(ctx, req)`, `Update(ctx, id, req)`, `Delete(ctx, id)` using standard SDK HTTP patterns
- [x] 7.3 Add `Skills *skills.Client` field to `sdk.Client` struct in `apps/server/pkg/sdk/sdk.go`
- [x] 7.4 Initialize `Skills` client in `sdk.Client.initClients()`

## 8. CLI

- [x] 8.1 Create `tools/cli/internal/cmd/skills.go`: declare `skillsCmd` cobra group (GroupID: `ai`) and all subcommand vars (`listSkillsCmd`, `getSkillCmd`, `createSkillCmd`, `updateSkillCmd`, `deleteSkillCmd`, `importSkillCmd`)
- [x] 8.2 Implement `runListSkills`: call `c.SDK.Skills.List(ctx, projectID)`, print table with columns `NAME`, `DESCRIPTION`, `SCOPE`, `ID`; support `--project`, `--global`, `--json` flags
- [x] 8.3 Implement `runGetSkill`: call `c.SDK.Skills.Get(ctx, id)`, print full details including content; support `--json`
- [x] 8.4 Implement `runCreateSkill`: validate `--name` against slug regex client-side; read content from `--content` or `--content-file`; call `c.SDK.Skills.Create`; support `--project`, `--json`
- [x] 8.5 Implement `runUpdateSkill`: call `c.SDK.Skills.Update` with only provided fields; support `--description`, `--content`, `--content-file`, `--json`
- [x] 8.6 Implement `runDeleteSkill`: show confirmation prompt unless `--confirm`; call `c.SDK.Skills.Delete`
- [x] 8.7 Implement `runImportSkill`: read file, split on `---` frontmatter delimiter, parse YAML for `name`/`description`/`metadata`, use remaining text as `content`; call `c.SDK.Skills.Create`; support `--project`, `--json`; return descriptive errors for missing frontmatter fields
- [x] 8.8 Wire all subcommands in `init()` and register `skillsCmd` with `rootCmd`
- [x] 8.9 Import the skills command package in `tools/cli/cmd/main.go` (blank import if needed)
