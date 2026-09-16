# Tasks: cli-agent-mcp-keys

## Task 1: SDK client for the agent MCP endpoint and its keys

- [x] Add DTOs to `apps/server/pkg/sdk/mcp/agent_endpoint.go`:
      `AgentMCPEndpoint`, `AgentMCPKey`, `AgentMCPKeyList`,
      `CreateAgentMCPKeyRequest`, `AgentMCPKeySecret` (embeds `AgentMCPKey` plus
      `token`/`mcpUrl`), `AgentMCPSession`, `AgentMCPSessionList`
- [x] Add a shared `doJSON` request helper on `mcp.Client` that authenticates,
      parses `sdkerrors.ParseErrorResponse` on ≥400, and decodes a JSON body
- [x] Add `CreateAgentEndpoint` / `GetAgentEndpoint` (project + agent path)
- [x] Add `RevokeAgentEndpoint` (project + endpoint id)
- [x] Add `CreateAgentKey` / `ListAgentKeys` (project + endpoint id)
- [x] Add `RevokeAgentKey` / `RotateAgentKey` (project + key id)
- [x] Add `ListAgentSessions` (project + endpoint id, optional status query)
- [x] Unit tests (`agent_endpoint_test.go`) for each method against
      `sdk/testutil.MockServer`: method + path, request body, decoded response,
      and that a 409/404 maps to `*sdkerrors.Error`

## Task 2: CLI command tree

- [x] Add `apps/cli/internal/cmd/agent_mcp_endpoint.go` with the group
      `mcp-endpoint` registered on `agentsCmd`; help text naming the distinction
      from `agents mcp-servers`
- [x] `runMCPEndpointShow`: resolve project + agent, GET the endpoint, print
      fields (or decode with 404 mapped to "no endpoint for this agent")
- [x] `runMCPEndpointCreate`: resolve project + agent, POST; on 409 print the
      "endpoint already exists" guidance; print id/status/URL
- [x] `runMCPEndpointRevoke`: resolve project + agent, GET endpoint id, then
      DELETE; confirm with `--yes` / prompt
- [x] `runMCPEndpointKeysCreate`: require `--label`, POST, render secret once
- [x] `runMCPEndpointKeysList`: GET and print label/status/created/last-used
      (never a secret)
- [x] `runMCPEndpointKeysRevoke`: DELETE by key id, confirm with `--yes`
- [x] `runMCPEndpointKeysRotate`: POST rotate by key id, render secret once
- [x] `runMCPEndpointSessions`: GET sessions, optional `--status`, print owning
      key label, status, turn/step counts, timestamps
- [x] `--project` from the `agents` group persistent flag; `--json` on `show`,
      `keys list`, and `sessions`
- [x] `resolveAgentsMCPReference` — id passthrough, runtime-agent name match,
      definition name/slug match, ambiguity/not-found errors; reuse the picker
      when the argument is omitted on a terminal
- [x] `agentSlug` — parity with `pkg/acpslug.FromName`
- [x] `renderAgentMCPKeySecret` — the single secret-printing path, with the
      "shown once" warning and MCP URL
- [x] `mapAgentMCPError` — 409 duplicate label, 409 endpoint exists, 404, 403
- [x] `confirmDestructive` — `--yes` in non-interactive contexts, `[y/N]`
      prompt on a terminal

## Task 3: CLI unit tests

- [x] Command/flag wiring tests: group placement, subcommand registration,
      required args, `--label` required, `--json` on read commands, `--yes` on
      destructive commands
- [x] `resolveAgentsMCPReference` tests: UUID passthrough, name match, slug
      match, ambiguous, not found (against a mock agents/definitions server)
- [x] `agentSlug` parity table (lowercase, punctuation collapse, trim, 63-cap)
- [x] `renderAgentMCPKeySecret` test: token appears exactly once, warning text
      present, MCP URL present, JSON-free
- [x] `mapAgentMCPError` tests: 409 duplicate vs endpoint-exists, 404, 403,
      unrecognized error passthrough
- [x] `confirmDestructive` test: non-interactive without `--yes` errors;
      with `--yes` proceeds
- [x] Run `go build ./...` and `go test ./... -count=1` in `apps/cli`

## Task 4: Verification

- [x] `openspec validate cli-agent-mcp-keys --strict`
- [x] `go build ./...` + `go test ./... -count=1` in `apps/cli`
- [x] SDK client methods (Task 1) ship via the released `apps/server/pkg/sdk`
      module; the CLI imports them through the plain module graph
- [x] `golangci-lint` on the changed packages; compare findings to `main`
- [x] Pin the released SDK `v0.82.0` in `apps/cli/go.mod`/`go.sum` so the plain
      module graph resolves under `GOWORK=off`; simulate the Docker
      `cli-builder` stage (`go mod download` from a bare dir) → prints
      `DOCKER-STAGE-SIM-OK`
- [x] CLI e2e: not run (runlog + Docker + a live backend are not available in
      this environment); state so in the PR
