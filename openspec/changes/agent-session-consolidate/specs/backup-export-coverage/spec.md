## MODIFIED Requirements

### Requirement: Backup export covers the full project surface

The backup exporter SHALL export a curated set of stateful project-scoped tables, not just the original eight. The set SHALL include documents, chunks, graph objects and relationships, chat, extraction jobs, object type schemas and schema registries, branches, project settings and provider configs, embedding policies, agents and agent definitions, project-owned skills, MCP servers, tags, tasks, external sources, product versions, sandbox images, and project memberships.

The exporter SHALL exclude ephemeral, derived, and security-sensitive tables: document parsing jobs, discovery jobs, agent sessions, run events, LLM usage events, user recent items, invites, notifications, and API tokens.

#### Scenario: Full backup includes schemas and branches

- **WHEN** a user creates a backup of a project that has object type schemas, branches, and agent definitions
- **THEN** the backup archive SHALL contain NDJSON for `object_type_schemas`, `branches`, and `agent_definitions` in addition to the original eight tables
- **THEN** a restore from that archive SHALL recreate the schemas and branches

#### Scenario: Security-sensitive tables are never exported

- **WHEN** a user creates a backup of any project
- **THEN** the archive SHALL NOT contain `core.api_tokens` or any API token material
- **THEN** the archive SHALL NOT contain `llm_usage_events`, `sessions`, `run_events`, `user_recent_items`, `invites`, `notifications`, or `document_parsing_jobs`

#### Scenario: Journal is opt-in

- **WHEN** a user creates a backup without `includeJournal`
- **THEN** the archive SHALL NOT contain `project_journal` or `project_journal_notes`
- **WHEN** a user creates a backup with `includeJournal: true`
- **THEN** the archive SHALL contain `project_journal` and `project_journal_notes`

#### Scenario: Project-owned skills only

- **WHEN** an org has org-level skills (`org_id` set, `project_id` NULL) and a project has project-owned skills
- **WHEN** a backup of the project is created
- **THEN** the archive SHALL contain only the project-owned skills
- **THEN** org-level skills SHALL NOT be exported
