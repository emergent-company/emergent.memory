## ADDED Requirements

### Requirement: Skill entity data model
A skill SHALL be stored as a row in `kb.skills` with the following fields: `id` (UUID, generated), `name` (slug), `description` (plain text), `content` (Markdown body), `metadata` (JSON), `description_embedding` (`vector(768)`), `project_id` (nullable), `org_id` (nullable), `created_at`, `updated_at`. A skill's scope SHALL be project > org > global depending on which ID field is set.

#### Scenario: Project-scoped skill
- **WHEN** a skill is created with `project_id` set
- **THEN** the skill's scope is `project`

#### Scenario: Global skill
- **WHEN** a skill is created with neither `project_id` nor `org_id` set
- **THEN** the skill's scope is `global`

### Requirement: Skill name validation
A skill name SHALL match `^[a-z0-9]+(-[a-z0-9]+)*$` (lowercase slug, hyphen-separated) and SHALL be 1–64 characters.

#### Scenario: Valid name accepted
- **WHEN** a skill is created with name `code-review`
- **THEN** the create succeeds

#### Scenario: Invalid name rejected
- **WHEN** a skill is created with name `Code Review!`
- **THEN** the create is rejected with a validation error

#### Scenario: Empty name rejected
- **WHEN** a skill is created with name `""`
- **THEN** the create is rejected with a validation error

### Requirement: Skill description and content are required
A skill SHALL have a non-empty `description` and non-empty `content`.

#### Scenario: Missing description rejected
- **WHEN** a skill is created without a description
- **THEN** the create is rejected with a validation error

#### Scenario: Missing content rejected
- **WHEN** a skill is created without content
- **THEN** the create is rejected with a validation error

### Requirement: Skill content size cap
The server SHALL reject skill content larger than a configured maximum (default 1 MiB).

#### Scenario: Oversized content rejected
- **WHEN** a skill is created with content larger than the configured maximum
- **THEN** the create is rejected with a validation error

### Requirement: Skill name uniqueness within scope
A skill name SHALL be unique globally, and unique within a project. When a project creates a skill with a name that also exists globally or at the org level, the project-scoped skill SHALL shadow the broader-scoped skill for that project (no collision error).

#### Scenario: Duplicate name in same project rejected
- **WHEN** a project attempts to create a second skill with the same name it already has
- **THEN** the create is rejected with a conflict error

#### Scenario: Project skill shadows global skill
- **WHEN** a global skill `deploy` exists and a project creates its own skill named `deploy`
- **THEN** the create succeeds and the project-scoped `deploy` takes precedence for that project

### Requirement: REST skill management surface
The server SHALL expose REST endpoints for skill CRUD at global, org, and project scope. Creating, updating, or deleting a global skill SHALL require `superadmin_full`; a `superadmin_readonly` grant, a plain authenticated user, or an absent superadmin module SHALL all be denied (the gate SHALL fail closed). Creating project/org skills SHALL require the corresponding scope authorization.

- `GET|POST /api/skills` and `GET|PATCH|DELETE /api/skills/:id` (global)
- `GET|POST /api/orgs/:orgId/skills` and `PATCH|DELETE /api/orgs/:orgId/skills/:id` (org)
- `GET|POST /api/projects/:projectId/skills` and `PATCH|DELETE /api/projects/:projectId/skills/:id` (project)

#### Scenario: Project list returns merged visible skills
- **WHEN** `GET /api/projects/:projectId/skills` is called
- **THEN** the response contains the union of project-scoped, org-scoped, and global skills, with project-shadowing applied

#### Scenario: Global create rejected for non-superadmin
- **WHEN** a non-superadmin calls `POST /api/skills`
- **THEN** the create is rejected with an authorization error

#### Scenario: Global update and delete rejected for non-superadmin
- **WHEN** a non-superadmin calls `PATCH /api/skills/:id` or `DELETE /api/skills/:id`
- **THEN** the mutation is rejected with an authorization error

#### Scenario: Global mutation rejected for superadmin_readonly
- **WHEN** a `superadmin_readonly` principal calls `POST /api/skills`, `PATCH /api/skills/:id`, or `DELETE /api/skills/:id`
- **THEN** the mutation is rejected with an authorization error

#### Scenario: Global mutation fails closed when superadmin module is absent
- **WHEN** the superadmin module is not loaded and any authenticated principal calls `POST /api/skills`, `PATCH /api/skills/:id`, or `DELETE /api/skills/:id`
- **THEN** the mutation is denied rather than permitted

#### Scenario: Update is partial
- **WHEN** `PATCH /api/skills/:id` is called with only `content`
- **THEN** only `content` changes; `name` and `description` are preserved

### Requirement: MCP skill management tools
The server SHALL expose MCP tools for skill management scoped by the calling agent's permissions:

- `skill-list` (read) — returns `{id, name, description, scope}` summaries for in-scope skills.
- `skill-get` (read) — returns full content for a skill by `skill_id` (UUID or name).
- `skill-create` (write) — creates a **project-scoped** skill with `name`, `description`, `content`.
- `skill-update` (write) — updates `description`/`content` of an existing skill by `skill_id`.
- `skill-delete` (write) — deletes a skill by `skill_id`.

`skills:write` SHALL imply `skills:read`. Write tools SHALL be project-scoped (an agent cannot create global/org skills via MCP).

#### Scenario: Agent creates a project skill
- **WHEN** an agent calls `skill-create` with `name`, `description`, `content`
- **THEN** a project-scoped skill is created in the agent's project

#### Scenario: skill-get resolves by name
- **WHEN** `skill-get` is called with a skill name that exists project-scoped, else global
- **THEN** the matching skill's full content is returned

#### Scenario: write requires skills:write scope
- **WHEN** an agent lacks `skills:write` calls `skill-create`
- **THEN** the call is rejected

### Requirement: Description embedding on every write path
When a skill is created or its description updated through **any** path (REST, MCP, CLI, blueprint), the server SHALL generate and store a description embedding. If embedding generation fails, the write SHALL still succeed but SHALL record that no embedding is present.

#### Scenario: MCP create populates embedding
- **WHEN** `skill-create` succeeds with a non-empty description
- **THEN** the stored skill has a non-null `description_embedding`

#### Scenario: Embedding failure is non-fatal
- **WHEN** the embeddings service errors during a create
- **THEN** the skill is still created, with `description_embedding` null

### Requirement: Skill provenance metadata
A skill's `metadata` SHALL support optional provenance fields: `source` (one of `manual`, `cli`, `blueprint`, `agent`, `marketplace`), `license` (SPDX identifier or free text), `version` (string), `source_url`, `origin_id`, and `content_hash` (SHA-256 of `content`, computed server-side at write). These fields SHALL be preserved verbatim and never inferred or overwritten by the server except for `content_hash`.

#### Scenario: Import records provenance
- **WHEN** a skill is imported from a SKILL.md file with a `license` and `version` in frontmatter
- **THEN** the stored skill's metadata carries those values plus a server-computed `content_hash`

#### Scenario: content_hash recomputed on content change
- **WHEN** a skill's content is updated
- **THEN** `content_hash` is recomputed to match the new content

#### Scenario: provenance preserved on update
- **WHEN** a skill is updated without changing metadata
- **THEN** existing `source`, `license`, `version`, `source_url`, `origin_id` values are unchanged

### Requirement: Local skill import
The CLI SHALL support importing skills from SKILL.md files: `memory skills import --builtin` (embedded catalog) and `memory skills import --discover` (scan known agent skill directories). Imported skills SHALL carry provenance metadata (`source`, `license`, `version` from frontmatter where present).

#### Scenario: builtin import idempotent
- **WHEN** `memory skills import --builtin` runs twice
- **THEN** the second run does not duplicate existing skills (matched by name)

#### Scenario: discover scans known directories
- **WHEN** `memory skills import --discover` runs
- **THEN** skills are imported from the configured known skill directories, each with `source = cli`

### Requirement: AutoLoadSkills prefix convention
When an agent definition sets `auto_load_skills = true`, any skill whose name matches `"{agentName}.{suffix}"` SHALL be automatically included in that agent's available skills, merged with the agent's explicit `skills` list (explicit names first, deduplicated).

#### Scenario: Auto-load matches agent-prefixed skills
- **WHEN** an agent named `diane` has `auto_load_skills = true` and skills `diane.meetings` and `diane.review` exist
- **THEN** both skills are available to the agent without being listed in its `skills` field

#### Scenario: Explicit skills take precedence and dedup
- **WHEN** the agent also lists `skills: ["diane.review", "research"]`
- **THEN** `diane.review` appears once, and `research` is included

#### Scenario: AutoLoadSkills off — no prefix matching
- **WHEN** `auto_load_skills = false`
- **THEN** only explicitly listed skills are available, regardless of name prefix
