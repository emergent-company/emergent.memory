## Why

The agent-scoped MCP endpoint (the "MCP endpoint" section on an agent's own Settings page) shipped without Playwright coverage. It replaces the superseded project-share agent picker: an agent now owns one endpoint and many labeled keys, each key's secret is shown exactly once, and keys can be rotated or revoked independently. Nothing currently exercises that surface end-to-end, so the two invariants that matter most — the one-time secret guarantee and the independence of keys on a shared endpoint — can regress silently, along with the "no agent picker on the project shares page" contract that the migration depends on.

## What Changes

Add one Playwright spec, `apps/web-ui/tests/e2e/specs/agents/agent-mcp-keys-ui.spec.ts`, in the serial `mutations` project, covering the agent Settings MCP section:

- the section renders on the agent Settings page, with the create-the-endpoint first step before one exists;
- creating the endpoint shows a readable endpoint URL that the copy affordance actually copies;
- the sessions panel renders its empty state for an endpoint with no external client sessions (no live agent run required);
- a labeled key create shows the one-time secret and lists an active row;
- the raw secret never reappears after navigating away and reloading;
- a duplicate label surfaces a readable inline error (backend 409) and creates no second key;
- rotation shows a new secret exactly once and keeps the key's identity;
- revoking one key marks it revoked and leaves the other key on the endpoint usable;
- revoking the endpoint returns the section to its not-enabled state;
- a regression guard that the project shares page (`/settings/mcp-servers/shares`) offers no agent picker.

The spec creates one dedicated `E2E MCP Agent` in `beforeAll` and, in `afterAll`, revokes every key it created, revokes the endpoint, and deletes the agent, so repeated runs are idempotent and it never races the parallel read surface.

No product behavior change and no new Playwright project or config change — test spec only. All locators use the section's existing roles/labels and `agent-mcp-*` test ids.

## Capabilities

### New Capabilities

- `e2e-agent-mcp-keys`: Playwright coverage of the agent-owned MCP endpoint UI on the agent Settings page — endpoint create/revoke, labeled key create/rotate/revoke, the one-time secret guarantee, the sessions panel, and the superseded project-share agent-picker guard.

### Modified Capabilities

None.

## Impact

- `apps/web-ui/tests/e2e/specs/agents/agent-mcp-keys-ui.spec.ts` — new spec (mutations project, `-ui.spec.ts` testMatch).
- No changes to gateway templ/handlers, server code, SDK, CLI, or Playwright config.
- No secret or token is committed; the spec only reads secrets from the live one-time reveal at runtime and asserts their absence afterwards.
