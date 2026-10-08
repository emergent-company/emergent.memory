## 1. Sweep the raw-error `pageError` surfaces (`web-ui-degraded-backend-state`)

- [x] 1.1 Inventory every `pageError(...)` call site in `apps/web-ui/gateway` and classify each raw backend error.
- [x] 1.2 Redact the agent surfaces (`agent.go`, `agent_mcp_endpoint_handlers.go`) with `userFacingError`.
- [x] 1.3 Redact the API-token and inbox surfaces (`api_tokens_handlers.go`, `inbox.go`), including the reveal-branch `data.LoadErr.Error()` leak.
- [x] 1.4 Redact the MCP surfaces (`mcp_nodes.go`, `mcp_registry.go`, `mcp_servers_handlers.go`, `mcp_shares_handlers.go`), sanitizing the MCP relay `errors.Join` result once and preserving nil semantics.
- [x] 1.5 Redact the objects/runs/schedules/share-owner surfaces (`objects.go`, `runs.go`, `schedules.go`, `share_owner.go`); keep the local "not found" literals.
- [x] 1.6 Redact the org-members/backups/schema surfaces (`org_members_ui.go`, `backups.go`, `schema.go`); preserve the backups no-org sentinel and the "backup not found" copy.
- [x] 1.7 Redact the remaining UI surfaces (`ui.go`, `documents_ui.go`, `datasources_ui.go`, `sessions.go`, `usage.go`, `board.go`, `approvals_handlers.go`, `blueprints_handlers.go`, `schema_packs.go`, `embeddings.go`, `share_session_transcript.go`), guarding the success-path pass-throughs so nil/empty-state gating is unchanged.

## 2. Tests

- [x] 2.1 Add `TestPageErrorSweepRedactsRawBackendErrors` (table-driven): representative swept pages render `memoryUnavailableMessage` and none of `dial tcp`, the IP:port, `memory 5`, `service_unavailable`, `Token validation`.
- [x] 2.2 Update the pre-existing assertions that encoded the raw text (`sessions`, `usage`, `backups` unreachable) to the sanitized message.
- [x] 2.3 Mutation check: reverting one swept site to the raw error makes the new test fail.

## 3. Verification

- [x] 3.1 `go build ./...`, `go vet ./...`, `go test ./...` from `apps/web-ui/gateway`.
- [x] 3.2 `golangci-lint run ./...` (0 issues).
- [x] 3.3 `openspec validate web-ui-pageerror-sweep --strict`.
