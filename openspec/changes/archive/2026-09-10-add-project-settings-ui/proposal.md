## Why

Alfred's gateway UI lets users edit per-agent settings, but there is no surface for project-level memory configuration. The Memory service stores the project record itself (name, project info, chat prompt template, extraction toggles, budget) and general per-project settings (agent definition overrides, the `remember` pipeline config, the entity-dedup threshold), yet these are reachable only through raw REST calls. Exposing them in the UI closes the gap so users can inspect and adjust project-level behavior without leaving Alfred.

## What Changes

- Add a new **Project Settings** page to the gateway web UI, linked from the existing Settings sidebar group.
- Surface the project record itself — name, project info (free text), chat prompt template, auto-extract toggles, and budget — with read and edit.
- Surface agent definition overrides (per-agent system prompt, model, tools, max steps, sandbox config) with read and edit.
- Surface the `remember` pipeline config (which agent definition powers `POST /remember`) and the entity-create dedup threshold.
- Add `MemoryClient` methods to read and write these settings via the Memory REST API.
- Add a sidebar navigation entry point for the new page.

## Capabilities

### New Capabilities

- `project-settings-ui`: a gateway UI page for viewing and editing project-level Memory settings (the project record, agent definition overrides, remember config, and entity-create dedup threshold).

### Modified Capabilities

<!-- No existing capability requirements change. -->

## Impact

- **gateway/** (Go + templ): new page handler, new `.templ` component, new `MemoryClient` methods, sidebar navigation entry.
- **Memory service REST API** (read/write dependency, no service change required): `/api/projects/current` and `/api/projects/:id` (project record), `/api/projects/:projectId/settings/:category/:key`, and `/api/projects/:projectId/agent-definitions/overrides`.
- **Tests**: gateway unit tests for the new handler and `MemoryClient` methods (TDD), following the existing `handlers_test.go` / `memory_test.go` patterns.
