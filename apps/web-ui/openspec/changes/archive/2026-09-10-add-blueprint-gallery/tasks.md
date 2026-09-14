## 1. Memory client methods + structs

- [x] 1.1 Add blueprint structs in `gateway/memory.go` (`InstalledSchemaItem`, `AvailableSchemaItem`, `CompiledSchemaTypes`, `AssignResult`, `RegisterSchemaRequest`) mirroring memory's `/api/schemas` JSON shapes
- [x] 1.2 Add `MemoryClient.ListInstalledSchemas(ctx)` (GET `/api/schemas/projects/:pid/installed`)
- [x] 1.3 Add `MemoryClient.ListAvailableSchemas(ctx)` (GET `/api/schemas/projects/:pid/available`)
- [x] 1.4 Add `MemoryClient.GetCompiledTypes(ctx)` (GET `/api/schemas/projects/:pid/compiled-types`)
- [x] 1.5 Add `MemoryClient.AssignSchema(ctx, schemaID)` (POST `/api/schemas/projects/:pid/assign`)
- [x] 1.6 Add `MemoryClient.RegisterSchema(ctx, req)` (POST `/api/schemas`)
- [x] 1.7 Add `MemoryClient.ToggleSchemaAssignment(ctx, assignmentID, active)` (PATCH `/api/schemas/projects/:pid/assignments/:id`)
- [x] 1.8 Add `MemoryClient.RemoveSchemaAssignment(ctx, assignmentID)` (DELETE `/api/schemas/projects/:pid/assignments/:id`)
- [x] 1.9 Extend `MemoryBackend` interface in `gateway/backend.go` with the seven new methods
- [x] 1.10 Add unit tests in `gateway/memory_test.go` for each new client method (fake HTTP server asserting method/path/body and decoding responses)

## 2. Bundled blueprint discovery

- [x] 2.1 Add `ListBundledBlueprints(dir)` that scans the `blueprints/` dir, parses `project.yaml` + pack/schema YAML into `AvailableSchemaItem{source:"bundled"}`
- [x] 2.2 Add `BLUEPRINTS_DIR` config field in `gateway/config.go` (default `blueprints/`), wired into the handler for testability
- [x] 2.3 Add unit tests for `ListBundledBlueprints` against a temp dir fixture (valid pack, missing project.yaml, malformed YAML)

## 3. API handlers

- [x] 3.1 Add `blueprints_handlers.go` with `listInstalledBlueprints`, `listAvailableBlueprints` (merge registry + bundled), `getCompiledTypes`
- [x] 3.2 Add `installBlueprint` handler: registry path (`schemaId` → assign) and bundled path (read local → register → assign), returning normalized `AssignResult`
- [x] 3.3 Add `toggleBlueprintAssignment` and `removeBlueprintAssignment` handlers
- [x] 3.4 Add error mapping helper (404 not_found → 404, else 502) mirroring `agentMemoryError`
- [x] 3.5 Register routes in `gateway/main.go`: `GET /api/blueprints/installed`, `GET /api/blueprints/available`, `GET /api/blueprints/compiled-types`, `POST /api/blueprints/install`, `PATCH /api/blueprints/assignments/:id`, `DELETE /api/blueprints/assignments/:id`
- [x] 3.6 Add unit tests in `blueprints_handlers_test.go` covering each route against a fake `MemoryBackend` (success, empty list, conflict result, 404, memory error 502)

## 4. UI page

- [x] 4.1 Add "Blueprints" item to `sidebarGroups()` in `gateway/ui.go` (`Href: /blueprints`, `lucide--library`)
- [x] 4.2 Add `blueprints.templ` with `BlueprintsPage` — Installed view (list, active toggle, remove, inspect types) and Available view (list + install action)
- [x] 4.3 Add `uiBlueprints` handler in `gateway/ui.go` calling `s.page(...)`; register `GET /blueprints` in `main.go`
- [x] 4.4 Run `templ generate` to produce `blueprints_templ.go`
- [x] 4.5 Add UI handler tests (rendered HTML contains installed/available rows, empty-state, load-error path) mirroring `agent_ui_test.go`

## 5. Verification

- [x] 5.1 `go build ./...` passes
- [x] 5.2 `go test ./...` passes (existing + new tests)
- [x] 5.3 `task lint` (gofmt + go vet + golangci-lint) passes
- [x] 5.4 Expand memory `emt_*` token to include `schema:write` (ops step); confirm list/install round-trip against memory
- [x] 5.5 Server restart + browser test: `/blueprints` renders, install/toggle/remove flows work
