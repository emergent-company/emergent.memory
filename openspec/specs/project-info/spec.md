# project-info Specification

## Purpose
Defines how project-level context information is stored, exposed, and edited. Replaces the `kb_purpose` field with a richer `project_info` markdown document per project.

## Requirements

### Requirement: Project info document stored per project
Each project SHALL have a `project_info` text column on `kb.projects` that stores a freeform markdown document describing the knowledge base's purpose, goals, audience, and scope. The field SHALL be nullable and have no maximum length enforced at the database level.

#### Scenario: Project created without project info
- **WHEN** a new project is created via `POST /api/projects`
- **THEN** `project_info` is `null` in the response

#### Scenario: Project info saved via PATCH
- **WHEN** an admin sends `PATCH /api/projects/:id` with `{ "project_info": "# About\nThis KB is about..." }`
- **THEN** the response includes the updated `project_info` value
- **AND** subsequent `GET /api/projects/:id` returns the same value

#### Scenario: Project info cleared via PATCH
- **WHEN** an admin sends `PATCH /api/projects/:id` with `{ "project_info": null }`
- **THEN** `project_info` is set to `null` and returned as omitted/null in the response

### Requirement: Project info included in access-tree response
The `project_info` field SHALL be included in each project entry returned by `GET /api/user/orgs-and-projects` so that clients have project context without an additional API call.

#### Scenario: Access tree includes project info
- **WHEN** a user requests `GET /api/user/orgs-and-projects`
- **THEN** each project in the response includes `project_info` if set
- **AND** the field is omitted (or null) if not set

### Requirement: kb_purpose removed from projects API
The `kb_purpose` field SHALL be removed from `kb.projects`, from the project REST API response, and from the SDK. Its value SHALL be migrated to `project_info` during the database migration. The `kb_purpose` snapshot column on `kb.discovery_jobs` SHALL remain unchanged.

#### Scenario: Migration preserves existing kb_purpose data
- **WHEN** the migration `00055_add_project_info.sql` runs on a database with existing `kb_purpose` values
- **THEN** all non-null, non-empty `kb_purpose` values are copied into `project_info`
- **AND** the `kb_purpose` column is dropped from `kb.projects`

#### Scenario: Discovery jobs continue using snapshotted value
- **WHEN** a discovery job was created before the migration
- **THEN** its `kb.discovery_jobs.kb_purpose` snapshot column retains the original value
- **AND** new discovery jobs read `project_info` instead of `kb_purpose` from `kb.projects`

### Requirement: get_project_info MCP tool
The system SHALL expose a built-in MCP tool named `get_project_info` that agents can call to retrieve the `project_info` document for the current project. The tool SHALL require no input parameters. The project ID SHALL be resolved from the execution context.

#### Scenario: Agent calls get_project_info with content set
- **WHEN** an agent calls `get_project_info` with no arguments
- **AND** the project's `project_info` is set
- **THEN** the tool returns the markdown document as a text content block

#### Scenario: Agent calls get_project_info with no content set
- **WHEN** an agent calls `get_project_info` with no arguments
- **AND** the project's `project_info` is null or empty
- **THEN** the tool returns a message indicating no project info has been configured

#### Scenario: get_project_info available in tool pool
- **WHEN** an agent's tool list includes `get_project_info` or `*`
- **THEN** the tool is resolved by the ToolPool and callable during the agent run

### Requirement: ProjectInfoEditor UI component
The admin UI SHALL provide a `ProjectInfoEditor` component that allows admins to view and edit the `project_info` field. The editor SHALL support markdown with a preview toggle. When `project_info` is null or empty, the editor SHALL pre-populate with a memory-oriented default template.

#### Scenario: Editor shows default template for new project
- **WHEN** an admin opens the ProjectInfoEditor for a project with no `project_info`
- **THEN** the textarea is pre-filled with the default markdown template
- **AND** saving the pre-filled template writes it to the database

#### Scenario: Editor saves updated content
- **WHEN** an admin edits the content and clicks save
- **THEN** a `PATCH /api/projects/:id` request is sent with the new `project_info` value
- **AND** a success toast is shown on save

#### Scenario: Editor preview toggle renders markdown
- **WHEN** an admin clicks the preview toggle
- **THEN** the markdown content is rendered as HTML in the preview pane
