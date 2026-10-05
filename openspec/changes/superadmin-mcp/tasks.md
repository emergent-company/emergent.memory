# Tasks

Implementation plan for `superadmin-mcp`. Per project convention, each behaviour is built test-first: unit tests are the minimum bar. All work is in `apps/server`.

## 1. Dedicated operator endpoint

- [ ] 1.1 Add an `admin` mode to the streamable MCP transport so project-claim, project catalog, instance-scope, and `requiresProject` are skipped, exposing the operator catalog and executing with an empty project
- [ ] 1.2 Provide a distinct admin transport instance and register `GET|POST|DELETE /api/mcp/admin` gated on `RequireAuth()` + `RequireSuperadminFull()`
- [ ] 1.3 Unit-test: unauthenticated `/api/mcp/admin` returns 401
- [ ] 1.4 Unit-test: authenticated non-`superadmin_full` caller (including `admin:all` token and `superadmin_readonly`) returns 403 at the route before any session/tool processing
- [ ] 1.5 Unit-test: `superadmin_full` principal with no project context initializes a session and lists the operator catalog
- [ ] 1.6 Unit-test: admin session carries no project id and does not call project claim

## 2. Operator tool catalog

- [ ] 2.1 Define `superadminToolDefinitions()` with the read and mutating tools named in the spec, each `SuperadminOnly`, kebab-case, with input schemas requiring their identifiers
- [ ] 2.2 Register the builder in `dynamicToolBuilders`
- [ ] 2.3 Unit-test: every catalog tool is `SuperadminOnly` and has an empty `RequiredScope`
- [ ] 2.4 Unit-test: every catalog tool name matches the kebab-case convention (lowercase/digits/hyphens, no underscores)
- [ ] 2.5 Unit-test: `tools/list` on `/api/mcp/admin` contains the operator tools and no project-scoped tool names
- [ ] 2.6 Adapt the re-homed `provider-test` and `provider-usage-get` to the projectless admin transport: resolve the target and credentials from explicit tool arguments rather than `auth.ProjectIDFromContext`, and have `provider-usage-get` return real usage data (call the provider usage service) instead of an endpoint-redirect placeholder; unit-test both with no project context

## 3. Read handlers

- [ ] 3.1 Implement handlers for `superadmin-get`, `user-list`, `user-get`, `org-list`, `org-get`, `project-list`, `project-get`, `project-member-list`, `email-job-list`, `email-job-get`, `embedding-job-list/-stats`, `extraction-job-list/-stats`, `document-parsing-job-list/-stats`
- [ ] 3.2 Require an explicit `org_id` on every org-targeted operator tool on the admin path — reads and mutations, including the re-homed `provider-configure-org` — and remove its ambient auth-context fallback
- [ ] 3.3 Unit-test (fakes): each read handler delegates to the corresponding repository/interface method with parsed arguments and returns the wrapped result
- [ ] 3.4 Unit-test: a read tool missing a required identifier returns an error naming the field and performs no repository call

## 4. Mutating handlers and confirmation

- [ ] 4.1 Implement handlers for `user-delete`, `org-delete`, `project-delete`, `superadmin-grant`, `project-member-create`, `project-member-delete`, `service-token-create`, `embedding-job-delete`, `embedding-job-cleanup`, `embedding-job-reset`, `extraction-job-delete`, `extraction-job-cancel`, `document-parsing-job-delete`, `document-parsing-job-retry`
- [ ] 4.2 Enforce the `confirm` guard before any repository mutation; return a structured dry-run naming the action and targets when unconfirmed
- [ ] 4.3 Unit-test: unconfirmed mutation returns a dry-run and makes zero repository mutation calls
- [ ] 4.4 Unit-test: confirmed mutation invokes the repository exactly once and returns its outcome
- [ ] 4.5 Unit-test: malformed/missing identifiers are rejected before the confirmation check and before any repository call
- [ ] 4.6 Bring the pre-existing mutating operator tools (`embedding-pause`, `embedding-resume`, `embedding-config-update`, `provider-configure-org`) under the same `confirm` guard, adding a `confirm` field to their input schemas
- [ ] 4.7 Unit-test: each pre-existing mutating operator tool returns a dry-run when unconfirmed and mutates only when `confirm=true`

## 5. Cross-domain wiring

- [ ] 5.1 Define the `SuperadminToolHandler` interface in `domain/mcp` with the repository/token operations the handlers need
- [ ] 5.2 Add it to `ServiceParams` as `optional:"true"` (nil-safe, fail-closed) and wire it in `NewService`
- [ ] 5.3 Provide the adapter in `cmd/server/main.go` under `MCP` + `SuperAdmin` (wrap `superadmin.Repository` + `apitoken.Service`)
- [ ] 5.4 Add dispatch cases in `dispatchTool`
- [ ] 5.5 Unit-test: with a nil handler (feature off), every operator tool fails closed with a clear error and performs no operation

## 6. Hard split of the project endpoint

- [ ] 6.1 Make the project MCP transports (`handler.go`, `sse_handler.go`, `streamable_http_handler.go`) always exclude `SuperadminOnly` tools from listing and refuse calling them, independent of caller superadmin status
- [ ] 6.2 Unit-test: a `superadmin_full` principal requesting `tools/list` on `/api/mcp` sees no `SuperadminOnly` tool
- [ ] 6.3 Unit-test: calling an operator tool name on `/api/mcp` returns method-not-found and performs no operation

## 7. Audit trail

- [ ] 7.1 Define a `SuperadminAuditWriter` and a DB implementation inserting into `kb.audit_log` (no migration; table exists in the baseline)
- [ ] 7.2 Emit an audit record for each operator tool call and each admin session initialize; redact credential-bearing arguments; emit a parallel structured log line
- [ ] 7.3 Make audit best-effort: a store failure logs a warning and never fails the tool call
- [ ] 7.4 Unit-test: a successful call writes one audit record with actor, tool, outcome, and duration
- [ ] 7.5 Unit-test: a failed call writes a record with the error outcome
- [ ] 7.6 Unit-test: credential arguments (API key, service-account JSON, token) never appear in the record or log
- [ ] 7.7 Unit-test: an audit writer error does not change the tool result
- [ ] 7.8 DB test: an audit row is inserted with the expected `event_type`, `action`, `endpoint`, and redacted `details`

## 8. In-process authority parity

- [ ] 8.1 Confirm every operator tool is covered by the derived `superadminOnlyToolNames` set (no manual map needed); extend tests to pin the full operator set
- [ ] 8.2 Unit-test: an in-process trusted dispatch of an operator tool without a resolved `superadmin_full` principal is denied
- [ ] 8.3 Unit-test: an unresolvable platform-grant lookup on the in-process path fails closed

## 9. Documentation

- [ ] 9.1 Add an operator runbook: connecting to `/api/mcp/admin`, the tool catalog, the `confirm` protocol, and audit retention/access
- [ ] 9.2 Document the endpoint move (operator tools no longer on `/api/mcp`) as a release note
- [ ] 9.3 Update the MCP tool catalog/README counts to include the operator tools

## 10. Verification

- [ ] 10.1 `cd apps/server && go build ./...`
- [ ] 10.2 `cd apps/server && go test ./domain/mcp/... ./domain/superadmin/... ./pkg/authz/...`
- [ ] 10.3 `task lint` (server)
- [ ] 10.4 `openspec validate superadmin-mcp --strict`
- [ ] 10.5 End-to-end against a dev server: a `superadmin_full` session lists and calls an operator read tool and an unconfirmed mutation (dry-run), then a confirmed mutation; verify audit rows
- [ ] 10.6 Confirm a non-superadmin session is rejected on `/api/mcp/admin` and sees no operator tools on `/api/mcp`
