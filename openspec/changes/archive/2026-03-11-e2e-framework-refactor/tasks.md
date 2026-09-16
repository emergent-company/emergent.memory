## 1. Create framework/ package scaffold

- [x] 1.1 Create `framework/` directory and `framework/doc.go` with `package e2eframework` package declaration
- [x] 1.2 Verify `go build ./framework/...` compiles (empty package)

## 2. Implement framework/client.go

- [x] 2.1 Copy `doJSON` from `install_test.go` → `framework/client.go` as exported `DoJSON`
- [x] 2.2 Copy `readBody` from `install_test.go` → `framework/client.go` as exported `ReadBody`
- [x] 2.3 Add `SetAuthHeader` helper (extracts the auth header setting logic)
- [x] 2.4 Copy `doMCPJSON` (if present) → `framework/client.go` as exported `DoMCPJSON`
- [x] 2.5 Verify `go build ./framework/...` still compiles

## 3. Implement framework/server.go

- [x] 3.1 Move `serverURL()` from `install_test.go` → `framework/server.go` as exported `ServerURL()`
- [x] 3.2 Move `skipIfServerDown(t)` → `framework/server.go` as exported `SkipIfServerDown(t)`
- [x] 3.3 Move `e2eTestToken()` → `framework/server.go` as exported `E2ETestToken()`
- [x] 3.4 Move `filteredEnv()` → `framework/server.go` as exported `FilteredEnv()`

## 4. Implement framework/env.go

- [x] 4.1 Move `loadDotEnv` from `testmain_test.go` → `framework/env.go` as exported `LoadDotEnv`
- [x] 4.2 Extract blueprint env helpers from orchestrator test → `framework/env.go` as `BlueprintEnvVar` and `ParseBlueprintEnvFiles`

## 5. Implement framework/parse.go

- [x] 5.1 Move `parseProjectID` → `framework/parse.go` as exported `ParseProjectID`
- [x] 5.2 Move `parseAgentID` → `framework/parse.go` as exported `ParseAgentID`
- [x] 5.3 Add `ParseJSONField` generic helper for extracting a named field from JSON bytes
- [x] 5.4 Move `compactRunsOutput` from `helpers_test.go` → `framework/parse.go` as exported `CompactRunsOutput`
- [x] 5.5 Move `allRunsTerminal` from `helpers_test.go` → `framework/parse.go` as exported `AllRunsTerminal`

## 6. Implement framework/project.go

- [x] 6.1 Extract project-create boilerplate from `orchestrator_test.go` → `framework/project.go` as `CreateProject(t, token, serverURL, name) string`
- [x] 6.2 Implement `DeleteProjectOnCleanup(t, token, serverURL, projectID)` using `t.Cleanup`
- [x] 6.3 Extract `configureGoogleProvider` boilerplate → `framework/project.go` as exported `ConfigureGoogleProvider`
- [x] 6.4 Extract `installBlueprint` boilerplate → `framework/project.go` as exported `InstallBlueprint`

## 7. Implement framework/agents.go

- [x] 7.1 Move `pollAgentUntilSuccess` from `ai_news_blueprint_test.go` → `framework/agents.go` as exported `PollUntilSuccess`
- [x] 7.2 Move `dumpAgentRunDetails` from `orchestrator_test.go` → `framework/agents.go` as exported `DumpAgentRunDetails`
- [x] 7.3 Extract `createAgent` helper → `framework/agents.go` as exported `CreateAgent`
- [x] 7.4 Extract `triggerAgent` helper → `framework/agents.go` as exported `TriggerAgent`
- [x] 7.5 Move `parseAgentID` usage → confirm it calls `framework/parse.go`

## 8. Implement framework/graph.go

- [x] 8.1 Move `listGraphObjectsByType` from `ai_news_blueprint_test.go` → `framework/graph.go` as exported `ListByType`
- [x] 8.2 Move `listGraphObjectsByLabel` → `framework/graph.go` as exported `ListByLabel`
- [x] 8.3 Move `listRelationships` (if present) → `framework/graph.go` as exported `ListRelationships`

## 9. Implement framework/cli.go

- [x] 9.1 Move `mustRunCLI` from `install_test.go` → `framework/cli.go` as exported `MustRunCLI`
- [x] 9.2 Move `mustRunCLIInDir` → `framework/cli.go` as exported `MustRunCLIInDir`
- [x] 9.3 Move `mustRunCLIInDirWithHome` → `framework/cli.go` as exported `MustRunCLIInDirWithHome`
- [x] 9.4 Move `logStatusPreamble` → `framework/cli.go` as exported `LogStatusPreamble`

## 10. Implement framework/runlog.go

- [x] 10.1 Move `runLog` struct from `helpers_test.go` → `framework/runlog.go` as exported `RunLog`
- [x] 10.2 Move Gantt timeline renderer → `framework/runlog.go` as exported `PrintGantt`
- [x] 10.3 Move token-usage summary → `framework/runlog.go` as exported `PrintTokenSummary`

## 11. Create fixtures/ package

- [x] 11.1 Create `fixtures/` directory
- [x] 11.2 Copy `bookstore_fixture.go` → `fixtures/bookstore.go`, rename package to `e2efixtures`, export type as `BookstoreWorkspace`
- [x] 11.3 Verify `go build ./fixtures/...` compiles

## 12. Update test files to use framework and fixtures

- [x] 12.1 Update `testmain_test.go` — call `framework.LoadDotEnv` instead of local `loadDotEnv`
- [x] 12.2 Update `install_test.go` — import `framework/`, remove duplicate helpers, use `framework.*`
- [x] 12.3 Update `brave_search_test.go` — import `framework/`, remove duplicate `doJSON`/`readBody`
- [x] 12.4 Update `orchestrator_test.go` — replace 6× project-setup boilerplate with `framework.CreateProject` + `framework.DeleteProjectOnCleanup`; remove extracted helpers
- [x] 12.5 Update `ai_news_blueprint_test.go` — import `framework/`, remove `pollAgentUntilSuccess`, `listGraphObjectsByType/Label`
- [x] 12.6 Update `v2_orchestrator_test.go` — import `framework/`, replace inline helpers
- [x] 12.7 Update `v3_orchestrator_test.go` — import `framework/`, replace inline helpers
- [x] 12.8 Update `blueprint_test.go` — import `framework/` for any shared helpers used
- [x] 12.9 Update `blueprint_v3_skills_test.go` — import `framework/` for any shared helpers used
- [x] 12.10 Update `tokens_test.go` — import `framework/` for any shared helpers used
- [x] 12.11 Update `production_test.go` — import `framework/` for any shared helpers used
- [x] 12.12 Update `task_cli_test.go` — import `framework/` for any shared helpers used
- [x] 12.13 Delete `bookstore_fixture.go` from root package after `fixtures/bookstore.go` is in place and all imports updated

## 13. Verify clean build and vet

- [x] 13.1 Run `go build ./...` — confirm zero errors
- [x] 13.2 Run `go vet ./...` — confirm zero vet errors
- [x] 13.3 Confirm `grep -r "func doJSON" *_test.go` returns no results
- [x] 13.4 Confirm `grep -r "func readBody" *_test.go` returns no results

## 14. Create .opencode/skills/create-e2e-test/ skill

- [x] 14.1 Create `.opencode/skills/create-e2e-test/` directory
- [x] 14.2 Write `.opencode/skills/create-e2e-test/SKILL.md` with description, trigger, workflow, and example skeleton
- [x] 14.3 Write `.opencode/skills/create-e2e-test/reference/framework-api.md` with all exported function signatures and descriptions
- [x] 14.4 Write `.opencode/skills/create-e2e-test/reference/patterns.md` with project isolation, agent trigger/poll, graph assertion, and CLI invocation patterns (with code examples)

## 15. Update AGENTS.md

- [x] 15.1 Update `AGENTS.md` to document the `framework/` package layout and `fixtures/` package
- [x] 15.2 Add a "Before Writing Tests" table entry pointing to the `create-e2e-test` skill
