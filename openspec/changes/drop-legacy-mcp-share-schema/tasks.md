## 1. OpenSpec change

- [x] 1.1 Add `openspec/changes/drop-legacy-mcp-share-schema/` with `.openspec.yaml` (`schema: spec-driven`, `created: 2026-09-16`), `proposal.md`, `design.md`, `tasks.md`, and the `agent-mcp-shares` `## MODIFIED Requirements` delta; verify `openspec validate drop-legacy-mcp-share-schema --strict` is valid.

## 2. Migration

- [x] 2.1 Add `apps/server/migrations/00153_drop_legacy_mcp_share_schema.sql`: Up drops `core.agent_mcp_shares` and `core.mcp_share_instances.allowed_agents`; Down recreates the table exactly as `00148` (columns, both partial unique indexes, the project/agent index, the `core.api_tokens` FK) and re-adds `allowed_agents UUID[]` with the `00150`/`00144` comment text.
- [x] 2.2 Verify from scratch against a throwaway Postgres: `go run ./cmd/migrate -c up` applies the full chain (proving the `00152` backfill still runs before the drop), `core.agent_mcp_shares` and `core.mcp_share_instances.allowed_agents` are gone, and `core.agent_mcp_endpoints` / `core.agent_mcp_keys` / `core.agent_mcp_sessions` / `core.mcp_share_instances` survive.
- [x] 2.3 Verify the Down restores both objects, then re-apply Up.

## 3. Dead Go code removal

- [x] 3.1 Delete the deprecated `AgentMCPShare` struct, `agentMCPShareStore`, and `bunAgentMCPShareStore` from `apps/server/domain/mcp/agent_mcp_share.go`, keeping `CallAgentOnce`, `agentRunErrorResult`, `resolveProjectAgent`, and `resolveAgentShareTarget`; remove the now-unused `database/sql` and `bun` imports.
- [x] 3.2 Delete `apps/server/domain/mcp/agent_mcp_share_store_test.go`.
- [x] 3.3 Remove the unused `Service.agentShares` field and `agentMCPShareOrNil(p.DB)` wiring from `apps/server/domain/mcp/service.go`; verify `go build ./...` passes.
- [x] 3.4 Remove the deprecated `allowed_agents` note from `apps/server/domain/mcp/share_instance.go`.
- [x] 3.5 Remove the `core.agent_mcp_shares` table block and the `allowed_agents` column line from `apps/server/internal/testutil/schema.sql`.
- [x] 3.6 Grep the whole repo for `agent_mcp_shares`, `AgentMCPShare`, and `allowed_agents`; confirm the only remaining hits are historical migrations (`00144`, `00148`, `00150`, the `00152` comments), the new `00153`, and archive docs — and report every hit with a justification.

## 4. Verification

- [x] 4.1 `openspec validate drop-legacy-mcp-share-schema --strict` is valid.
- [x] 4.2 From `apps/server`: `go build ./...`, `go vet ./domain/mcp/...`, `go test ./domain/mcp/... -short -count=1`, and `golangci-lint run ./domain/mcp/...` add no new findings over the ~31 pre-existing `main` findings.
- [x] 4.3 Run the non-short `go test ./domain/mcp/... -count=1` with `TEST_DATABASE_URL` set; report PASS/SKIP with 0 skips in `domain/mcp`.
- [x] 4.4 Confirm the legacy read-only share flow still works: `is_legacy`, `HandleShareMCPAccess`, and `recordLegacyShareInstance` are unchanged and their tests stay green.
