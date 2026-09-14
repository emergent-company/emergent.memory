## Why

Diane (the memory assistant) records preferences, facts, and notes into the Emergent Memory service, but users have no way to review what she has remembered from the iOS app. This adds a read-only browsing surface so users can see their agents' memories. Because memory is opt-in per agent (only Diane has it today; Alfred does not), the backend must expose a dedicated, memory-aware API rather than assuming every agent has memories.

## What Changes

- New backend "agent info" endpoints that proxy memory search/recall to the Emergent Memory service and return structured results to clients.
- The memory proxy is agent-aware: it exposes memories only for agents configured with a memory MCP server; agents without memory yield an empty result rather than an error.
- New native SwiftUI "Memories" browser in the iOS app (NavigationStack + List) that lists and searches memories for the selected agent.
- The iOS app queries the agent-info API to discover whether the selected agent supports memory and shows or hides the memories UI accordingly.

## Capabilities

### New Capabilities

- `agent-memory-api`: HTTP API exposing an agent's memories (search/recall) by proxying the Emergent Memory service, scoped so only memory-enabled agents surface results.
- `ios-memory-browser`: native SwiftUI browsing UI (navigation stack + list) for viewing memories created by the selected agent.

### Modified Capabilities

None.

## Impact

- Backend: `agent/` — new agent-info / memory-proxy endpoints (admin server and/or control-plane API), read-only proxy to the memory service.
- iOS: `client/ios/` — new memory browser views and navigation, an agent-info API client, and agent memory-capability discovery.
- Emergent Memory service: consumed read-only (existing search/recall), no changes.
- No breaking changes.
