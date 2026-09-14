## Why

The web app can inspect memory objects only indirectly — search and per-agent detail views inside the agent dashboard. Emergent Memory already stores a rich, typed graph of objects and relationships per project, but the web UI has no dedicated way to browse that graph as a whole (only the iOS client ships a list+detail memory browser). This change adds a first-class Objects & Relationships browser to the web app's main menu, giving users a direct, searchable, drill-down view of their project's memory objects and how they connect.

## What Changes

- Add an "Objects & Relationships" entry to the web app main menu (sidebar) and to the ⌘K spotlight palette.
- Add a new browser page that lists/searchable a project's memory objects, shows object detail on selection, and surfaces the relationships (edges) between objects.
- Reuse the existing `MemoryBackend` client surface: `entity-query`, graph objects, `relationship-*`, and `graph-traverse` — no new backend contract.
- Add a graph-style relationship view (derived edges, search-first with a list/detail fallback) as the primary relationship navigation; exact render approach is a design decision.

## Capabilities

### New Capabilities

- `objects-relationships-browser`: web UI for browsing a project's memory objects and their relationships, including search, filtering, list and detail views, and relationship navigation.

### Modified Capabilities

<!-- none -->

## Impact

- **Navigation**: `gateway/ui.go` (`sidebarGroups`), `gateway/ui.templ` (`appShell`), `gateway/spotlight.templ` (`spotlightItems`).
- **New page**: new templ component + handler + route registered in `gateway/main.go`.
- **Data layer**: reuse `gateway/memory.go` client + `gateway/backend.go` `MemoryBackend` interface (entity query, graph objects, relationship CRUD, graph traversal). No schema/data changes to Emergent Memory.
- **UI deps**: go-daisy components for list/detail; likely a graph-render dependency (e.g., force-graph class) — to be resolved in design.
- **Testing**: TDD — unit tests for handler/query layer and any traversal logic; deterministic, no timing/environment-dependent tests.
