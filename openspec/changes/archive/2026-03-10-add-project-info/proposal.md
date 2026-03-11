## Why

Projects currently lack a structured place for admins to describe what a knowledge base is for — its purpose, audience, key topics, and scope. Agents have no reliable way to understand project context beyond what they can infer from the graph itself, which makes it hard for them to give well-targeted responses or decide what belongs in the knowledge base. A project info document fixes this by giving both humans and agents a single source of truth about the project.

## What Changes

- Add a `project_info` text column to `kb.projects` for storing a freeform markdown document
- Expose `project_info` via the existing project REST API (GET/PATCH `/api/projects/:id`)
- Include `project_info` in the access-tree response (`/api/user/orgs-and-projects`)
- Add a new built-in MCP tool `get_project_info` that agents can call to retrieve the document
- Add a `ProjectInfoEditor` React component with a markdown default template, rendered in project settings
- Migrate the discovery jobs system from `kb_purpose` to `project_info`; drop the `kb_purpose` column from `kb.projects` (the snapshotted `kb_purpose` column on `kb.discovery_jobs` stays as historical data)

## Capabilities

### New Capabilities

- `project-info`: Per-project markdown document (`project_info` DB column, REST API field, `get_project_info` MCP tool, and `ProjectInfoEditor` UI component with a memory-oriented default template)

### Modified Capabilities

- `mcp-integration`: New built-in tool `get_project_info` added to the tool definitions list and `ExecuteTool` dispatch

## Impact

- **Database**: Migration adds `project_info text` to `kb.projects` and drops `kb_purpose`; `kb.discovery_jobs.kb_purpose` snapshot column stays unchanged
- **Backend**: `projects/entity.go`, `projects/service.go`, `projects/repository.go`, `useraccess/service.go`, `mcp/service.go`, `discoveryjobs/repository.go`, `discoveryjobs/service.go`
- **SDK**: `pkg/sdk/projects/client.go`
- **Tests**: `projects/repository_test.go`, `tests/api/suites/projects_test.go`
- **Frontend**: New `ProjectInfoEditor` component; `auto-extraction.tsx` settings page rendered alongside existing `KBPurposeEditor` (which will be removed)
- **No breaking API changes on new fields** — `project_info` is additive; `kb_purpose` removal is handled within the same migration
