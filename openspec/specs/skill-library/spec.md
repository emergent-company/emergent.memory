## ADDED Requirements

### Requirement: Skill entity and storage
The system SHALL store skills in a `kb.skills` table with fields: `id` (uuid pk), `name` (text, slug), `description` (text), `content` (text, Markdown body), `metadata` (jsonb, optional), `description_embedding` (vector(768), nullable), `project_id` (uuid nullable FK to `kb.projects`, NULL = global), `created_at`, `updated_at`.

A skill name SHALL conform to the pattern `^[a-z0-9]+(-[a-z0-9]+)*$` (1–64 chars, lowercase alphanumeric with hyphens, no leading/trailing or consecutive hyphens).

A skill with `project_id IS NULL` SHALL be considered global and visible to agents in all projects. A skill with a non-null `project_id` SHALL be scoped to that project only.

Within a scope, skill names SHALL be unique: a unique index on `(name)` WHERE `project_id IS NULL` enforces global uniqueness, and a unique constraint on `(name, project_id)` enforces per-project uniqueness.

#### Scenario: Create global skill
- **WHEN** a skill is created with `project_id` omitted or null
- **THEN** the skill is stored with `project_id = NULL` and is visible to all projects

#### Scenario: Create project-scoped skill
- **WHEN** a skill is created with a valid `project_id`
- **THEN** the skill is visible only to agents in that project

#### Scenario: Duplicate name within scope is rejected
- **WHEN** a skill is created with a name that already exists in the same scope (global or same project)
- **THEN** the system returns HTTP 409 Conflict

#### Scenario: Same name allowed across scopes
- **WHEN** a global skill named `emergent-onboard` exists and a project creates a skill also named `emergent-onboard`
- **THEN** both are stored; the project-scoped skill overrides the global one when resolving skills for that project

### Requirement: Skill description embedding
The system SHALL generate a `description_embedding vector(768)` for each skill using `embeddingsSvc.EmbedQuery(ctx, description)` (gemini-embedding-001, 768 dimensions) at create and update time.

If the embedding service is unavailable or returns an error, the skill SHALL still be created/updated with `description_embedding = NULL`. The system SHALL log a warning but SHALL NOT fail the request.

Skills with `description_embedding = NULL` SHALL be excluded from semantic retrieval results but SHALL remain accessible by exact name lookup.

#### Scenario: Embedding generated on create
- **WHEN** a skill is successfully created with a non-empty description and the embedding service is available
- **THEN** `description_embedding` is populated with a 768-dimensional vector before the row is inserted

#### Scenario: Embedding regenerated on description update
- **WHEN** a skill's `description` is updated
- **THEN** `description_embedding` is regenerated from the new description

#### Scenario: Embedding service unavailable on create
- **WHEN** a skill is created and the embedding service returns an error
- **THEN** the skill is stored with `description_embedding = NULL` and a warning is logged; the HTTP response indicates success

### Requirement: Skill CRUD REST API
The system SHALL expose REST endpoints for creating, reading, updating, and deleting skills.

Global skill endpoints (no project context):
- `GET /api/skills` — list all global skills (paginated)
- `POST /api/skills` — create a global skill
- `GET /api/skills/:id` — get skill by ID
- `PATCH /api/skills/:id` — update skill (partial)
- `DELETE /api/skills/:id` — delete skill

Project-scoped skill endpoints:
- `GET /api/projects/:projectId/skills` — list skills visible to the project (global + project-scoped merged; project-scoped wins on name collision)
- `POST /api/projects/:projectId/skills` — create a project-scoped skill
- `PATCH /api/projects/:projectId/skills/:id` — update a project-scoped skill
- `DELETE /api/projects/:projectId/skills/:id` — delete a project-scoped skill

All endpoints SHALL require a valid JWT (standard `RequireAuth()` middleware). Project-scoped endpoints SHALL additionally verify the caller has access to the given project.

Responses SHALL use the standard `APIResponse[T]` envelope: `{"success": true, "data": ...}`.

#### Scenario: List project skills merges global and project-scoped
- **WHEN** `GET /api/projects/:projectId/skills` is called
- **THEN** the response includes all global skills plus all skills scoped to that project, with project-scoped skills replacing any global skill of the same name

#### Scenario: Create skill returns 201
- **WHEN** `POST /api/skills` is called with valid `name`, `description`, and `content`
- **THEN** the system returns HTTP 201 with the created skill DTO

#### Scenario: Partial update via PATCH
- **WHEN** `PATCH /api/skills/:id` is called with only `description` provided
- **THEN** only the description (and its embedding) is updated; `name`, `content`, and `metadata` remain unchanged

#### Scenario: Delete returns 200 with no body data
- **WHEN** `DELETE /api/skills/:id` is called for an existing skill
- **THEN** the system returns HTTP 200 `{"success": true}`

#### Scenario: Unauthorized access rejected
- **WHEN** any skills endpoint is called without a valid JWT
- **THEN** the system returns HTTP 401

### Requirement: Skill resolution with project override
The `Repository.FindForAgent(ctx, projectID)` method SHALL return the merged set of skills visible to a given project: all global skills plus all project-scoped skills for `projectID`, with project-scoped entries replacing global entries sharing the same `name`.

#### Scenario: Project skill overrides global
- **WHEN** `FindForAgent` is called for a project that has a skill named `emergent-onboard` AND a global skill named `emergent-onboard` also exists
- **THEN** only the project-scoped `emergent-onboard` is returned (the global one is omitted)

#### Scenario: Global skills included when no project override
- **WHEN** `FindForAgent` is called for a project with no project-scoped skills
- **THEN** all global skills are returned

### Requirement: IVFFlat index on description_embedding
The system SHALL create an IVFFlat index on `kb.skills(description_embedding)` using `vector_cosine_ops` with `lists = 100` for efficient approximate nearest-neighbor search.

#### Scenario: Index exists after migration
- **WHEN** migration `00052_create_skills.sql` is applied
- **THEN** the index `idx_skills_description_embedding_ivfflat` exists on `kb.skills`
