## 0. Setup — e2e repo infrastructure

- [x] 0.1 Create `tests/api/testmain_test.go` with `TestMain` → `framework.LoadDotEnv()` + `m.Run()`
- [x] 0.2 Create `tests/api/helpers_test.go` with shared helpers: `newRunLog`, `skipIfServerDown`, `serverURL`, `e2eTestToken`, `createOrg`, `createProject`, `deleteOrg`, `doAPI`, `mustStatus`, `parseID`
- [x] 0.3 Add `tests/api` CI job to `emergent.memory.e2e/.github/workflows/` (runs `go test ./tests/api/... -v -timeout 15m`)

## 1. Phase 1 — Core (health, authinfo, users, orgs, projects)

- [x] 1.1 Write `tests/api/health_test.go` (5 tests: `/health`, `/healthz`, `/ready`, `/debug`, auth info endpoints)
- [x] 1.2 Write `tests/api/authinfo_test.go` (3 tests)
- [x] 1.3 Write `tests/api/orgs_test.go` (18 tests: CRUD, membership, listing)
- [x] 1.4 Write `tests/api/projects_test.go` (35 tests: CRUD, membership, tokens, listing)
- [x] 1.5 Write `tests/api/users_test.go` (8 tests: profile, listing)
- [x] 1.6 Run Phase 1 tests against dev server — all green
- [x] 1.7 Open PR to `emergent.memory.e2e` — CI green — merge
- [x] 1.8 Delete `tests/e2e/health_test.go`, `authinfo_test.go`, `orgs_test.go`, `projects_test.go`, `users_test.go` from main repo — open PR — merge

## 2. Phase 2 — Auth & Access (apitoken, auth, useraccess, useractivity, invites, notifications)

- [x] 2.1 Write `tests/api/apitoken_test.go` (24 tests: create, list, revoke, scope enforcement)
- [x] 2.2 Write `tests/api/auth_test.go` (26 tests: bearer auth, scopes, middleware behavior)
- [x] 2.3 Write `tests/api/security_scopes_test.go` (25 tests: per-scope endpoint protection)
- [x] 2.4 Write `tests/api/useraccess_test.go` (7 tests)
- [x] 2.5 Write `tests/api/useractivity_test.go` (23 tests)
- [x] 2.6 Write `tests/api/invites_test.go` (9 tests)
- [x] 2.7 Write `tests/api/notifications_test.go` (23 tests)
- [x] 2.8 Run Phase 2 tests — all green
- [x] 2.9 Open PR to e2e repo — CI green — merge
- [x] 2.10 Delete corresponding files from main repo — PR — merge

## 3. Phase 3 — Documents & Chunks (documents, chunks, embedding_policies, extraction)

- [ ] 3.1 Write `tests/api/documents_test.go` (42 tests: upload, list, get, delete)
- [ ] 3.2 Write `tests/api/documents_upload_test.go` (8 tests: multipart upload mechanics)
- [ ] 3.3 Write `tests/api/chunks_test.go` (23 tests: list, filter, pagination)
- [ ] 3.4 Write `tests/api/embedding_policies_test.go` (23 tests: CRUD)
- [ ] 3.5 Write `tests/api/extraction_test.go` (8 tests: admin extraction job endpoints)
- [ ] 3.6 Run Phase 3 tests — all green
- [ ] 3.7 Open PR to e2e repo — CI green — merge
- [ ] 3.8 Delete corresponding files from main repo — PR — merge

## 4. Phase 4 — Graph & Search (graph, branches, search)

- [ ] 4.1 Write `tests/api/graph_test.go` (51 tests: object CRUD, relationships, traversal)
- [ ] 4.2 Write `tests/api/graph_search_test.go` (17 tests: semantic + metadata search)
- [ ] 4.3 Write `tests/api/graph_analytics_test.go` (9 tests)
- [ ] 4.4 Write `tests/api/graph_subgraph_test.go` (11 tests)
- [ ] 4.5 Write `tests/api/graph_similar_test.go` (6 tests)
- [ ] 4.6 Write `tests/api/graph_property_validation_test.go` (9 tests)
- [ ] 4.7 Write `tests/api/graph_query_endpoint_test.go` (4 tests)
- [ ] 4.8 Write `tests/api/graph_field_projection_test.go` (2 tests)
- [ ] 4.9 Write `tests/api/relationship_search_test.go` (17 tests)
- [ ] 4.10 Write `tests/api/branches_test.go` (37 tests: branch CRUD, merge, diff)
- [ ] 4.11 Write `tests/api/search_test.go` (25 tests: unified hybrid search)
- [ ] 4.12 Run Phase 4 tests — all green
- [ ] 4.13 Open PR to e2e repo — CI green — merge
- [ ] 4.14 Delete corresponding files from main repo — PR — merge

## 5. Phase 5 — AI Capabilities (chat, mcp, agents, skills, schemas)

- [ ] 5.1 Write `tests/api/chat_test.go` (35 tests: conversation CRUD, messages)
- [ ] 5.2 Write `tests/api/chat_conversation_history_test.go` (5 tests)
- [ ] 5.3 Write `tests/api/mcp_test.go` (24 tests: JSON-RPC protocol, initialization)
- [ ] 5.4 Write `tests/api/mcp_sse_tools_test.go` (29 tests: SSE streaming tools)
- [ ] 5.5 Write `tests/api/mcp_new_tools_test.go` (8 tests)
- [ ] 5.6 Write `tests/api/mcp_schema_lifecycle_test.go` (6 tests)
- [ ] 5.7 Write `tests/api/mcpregistry_test.go` (33 tests: registry CRUD, discovery)
- [ ] 5.8 Write `tests/api/agents_test.go` (39 tests: agent CRUD, trigger, runs)
- [ ] 5.9 Write `tests/api/agents_questions_test.go` (18 tests)
- [ ] 5.10 Write `tests/api/agents_webhooks_test.go` (18 tests)
- [ ] 5.11 Write `tests/api/agents_visibility_test.go` (6 tests)
- [ ] 5.12 Write `tests/api/agent_graph_query_live_test.go` (2 tests)
- [ ] 5.13 Write `tests/api/agent_chat_test.go` (2 tests)
- [ ] 5.14 Write `tests/api/adk_sessions_test.go` (1 test)
- [ ] 5.15 Write `tests/api/skills_test.go` (12 tests)
- [ ] 5.16 Write `tests/api/schemas_test.go` (merged templatepacks + schemaregistry: ~46 tests)
- [ ] 5.17 Run Phase 5 tests — all green
- [ ] 5.18 Open PR to e2e repo — CI green — merge
- [ ] 5.19 Delete corresponding files from main repo — PR — merge

## 6. Phase 6 — Admin & Isolation (superadmin, tenant_isolation, provider)

- [ ] 6.1 Write `tests/api/superadmin_test.go` (18 tests)
- [ ] 6.2 Write `tests/api/tenant_isolation_test.go` (13 tests: cross-tenant access prevention)
- [ ] 6.3 Write `tests/api/provider_test.go` (14 tests: LLM credential management)
- [ ] 6.4 Write `tests/api/tool_settings_test.go` (5 tests)
- [ ] 6.5 Write `tests/api/events_test.go` (6 tests)
- [ ] 6.6 Write `tests/api/userprofile_test.go` (17 tests)
- [ ] 6.7 Run Phase 6 tests — all green
- [ ] 6.8 Open PR to e2e repo — CI green — merge
- [ ] 6.9 Delete corresponding files from main repo — PR — merge

## 7. Cleanup — remove testutil from main repo

- [ ] 7.1 Delete `apps/server/internal/testutil/` from `emergent.memory`
- [ ] 7.2 Remove `test-e2e` job from `emergent.memory/.github/workflows/server.yml`
- [ ] 7.3 Remove `testutil` references from any remaining files in main repo (check with `grep -r testutil apps/server`)
- [ ] 7.4 Verify main repo CI passes without e2e job
- [ ] 7.5 Open cleanup PR to main repo — CI green — merge
