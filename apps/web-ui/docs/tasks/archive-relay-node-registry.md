# Archive the relay-node-registry change

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
After manual verification of the MCP nodes page (offline row + last-seen +
Remove), sync the delta specs into main specs and archive
`openspec/changes/add-relay-node-registry`.

## Why
The gateway change is implemented and gate-green but still active; it should be
archived like the connector changes once the UI behaviour is confirmed.

## Depends on
- [verify-relay-node-badges-deploy](verify-relay-node-badges-deploy.md) or a local
  `task dev` browser check.
- Update `openspec/specs/external-mcp-nodes/spec.md` + create
  `openspec/specs/relay-node-registry/spec.md`, then move the change to
  `openspec/changes/archive/`.

## Notes
- `openspec validate --specs` must pass after the sync.
