# Re-verify MCP tool call after memory release

**Status:** done
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md)

## What

After Emergent Memory's next release (containing PR #410 — slugified tool keys + slug-aware
call routing) deploys to api.dev, re-run the tool-call scenario and confirm it passes:

```
cd /root/alfred/tests/e2e && npx playwright test scenarios/mcp-servers-tool-call.spec.ts --project=scenarios
```

Cross-check `kb.agent_runs.tools` for the resolved `<slugged-server>_<tool>` key.

## Why

The scenario currently skips: api.dev runs v0.75.0 (has #406 only). #410 is merged but
undeployed; until it ships, external MCP tools either reach the model under an invalid
function name (spaces) or fail to route on call.

## Depends on

- Emergent Memory release ≥ version containing `687c86da` deployed to api.dev.
- None in alfred code — the spec is complete.

## Notes

- Re-run is the only step; expect the scenario's env-gated skip to disappear.
- If it still skips, capture the annotation + check readme fixture (provider key validity
  also gates the scenario).

## Resolution (2026-09-10)

Done. api.dev runs **v0.76.0** (contains #406/#410; `bareNameToKeys` resolves bare
whitelists and the pooled `<slugged-server>_<tool>` key routes correctly). The scenario
**passes live** (`npx playwright test scenarios/mcp-servers-tool-call.spec.ts --project=scenarios`
→ 2 passed; memory SSE emits `mcp_tool started/completed` for `e2e_mcp_tool_<ts>_web_fetch_exa`).

The remaining blockers were not the memory release but the test itself: it matched the
**bare** tool name while memory emits the resolved namespaced id, and it double-prefixed the
agent model. Both fixed in PRs #55 and #47. See
[2026-09-10-mcp-tool-call-e2e](../sessions/2026-09-10-mcp-tool-call-e2e.md).
