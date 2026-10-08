## Why

#1671/#1679 redacted every raw backend error rendered through `pageError` on the gateway's degraded pages.
But a second family of surfaces renders its error **directly from a template field** — not through
`pageError` — and those fields are still populated with the raw backend error. On a memory outage they leak
the same internals into the HTML: transport strings such as `dial tcp 172.17.0.1:8080: connection refused`,
the `memory 503 service_unavailable:` prefix, and `Token validation temporarily unavailable`.

The affected templated, non-`pageError` fields (from the #1679 review) are: `.PendingErr`, `.ToolsErr`,
`.ChatsErr`, `.StatusErr` / `.CoverageErr`, `.GeneralErr`, `.SaveErr`, `.MCPActionErr`, and the providers
`d.Err`. Each is set in a handler and rendered raw in a `.templ` file, bypassing the shared sanitizer.

The `web-ui-degraded-backend-state` capability already requires that unavailable surfaces never expose
internal error detail, but its "templated field" coverage stops at `pageError` surfaces and the two directly
rendered `data.LoadErr.Error()` surfaces. This change extends that guarantee to the remaining templated
error fields (#1689).

## What Changes

- Sanitize each listed field **at the point it is assigned** on the page/view model, using the existing
  `userFacingError` / `userFacingMemoryError` helpers (`apps/web-ui/gateway/memory.go`), so the template
  renders an already-safe value. No `.templ` change, and no change to control flow, branching, HTTP status
  codes, or which variant/empty-state renders.
- Add a small `redactBackendError(err) error` helper for surfaces that mix locally-authored copy with
  backend failures (the object-type editor's `SaveErr`, the MCP action banner's `MCPActionErr`): a memory
  HTTP error, a transport failure, or a timeout is sanitized; a local validation/sentinel error passes
  through unchanged.
- Preserve specific, safe, locally-authored copy: the object-type editor's validation messages ("property
  \"x\" needs a type", duplicate/wildcard properties) and the MCP action banner's duplicate-label /
  already-exists / "this agent has no active MCP endpoint" copy stay as authored; only genuine backend
  failures are redacted.
- Guard against double-wrapping: already-sanitized loader values passed through unchanged (for example
  `connectedDevicesPageData.ToolsErr = mcp.ToolsErr`) are not re-sanitized.
- Add a table-driven regression test (`TestTemplatedErrorFieldsRedactRawBackendErrors`) that drives
  representative affected pages through their real handlers/loaders with a raw transport failure and a raw
  503 `service_unavailable` error, asserting each renders `memoryUnavailableMessage` and none of the leak
  tokens (`dial tcp`, the IP:port, `memory 5`, `service_unavailable`, `Token validation`). Mutation-checked.

## Capabilities

### Modified Capabilities

- `web-ui-degraded-backend-state` — extend "Unavailable surfaces never expose internal error detail" from
  the `pageError` and directly-rendered `data.LoadErr` surfaces to every templated, non-`pageError` error
  field (`.PendingErr`, `.ToolsErr`, `.ChatsErr`, `.StatusErr`/`.CoverageErr`, `.GeneralErr`, `.SaveErr`,
  `.MCPActionErr`, providers `d.Err`).

## Impact

- `apps/web-ui/gateway/memory.go` — new `redactBackendError` helper alongside the existing
  `userFacingError` / `userFacingMemoryError`.
- `apps/web-ui/gateway/{org_members_ui,mcp_nodes,embeddings,agent,mcp_shares_handlers,mcp_servers_handlers,settings_providers,schema,agent_mcp_endpoint_handlers}.go`
  — sanitize the field assignments.
- `apps/web-ui/gateway/templated_error_fields_test.go` — new regression test (issue #1689).
- No `.templ` changes; no control-flow, branch, or status-code changes.
