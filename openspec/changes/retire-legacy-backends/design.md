## Context

Target architecture (already decided, spec 00): one Go binary = gateway API + supervisor + web UI; Memory owns all durable state; clients hold no Memory/LiveKit secrets.

## Decisions

- **Single origin.** The gateway (`:8080`) becomes the only host iOS and the web UI talk to. No more `:8081` FastAPI or `:8080` stdlib admin server.
- **Session-log sourced from Memory.** The legacy `SESSION_LOG` jsonl was written only by the retired Gemini-realtime bridge. The new `alfred_bridge` records turns/tool-calls in Memory (conversations + ACP history). The gateway session-log API maps Memory conversations/history onto the existing iOS wire contract, reusing `session_dump.go` timeline parsing.
- **Memory capability from Memory, not SQLite.** The memory-proxy `capability` check derives from the agent-definition's MCP references (does it reference the `memory` MCP server?), not the FastAPI `Store`.
- **Retirement is last.** Gateway endpoints land and iOS retargets and is verified against the gateway before any legacy file is deleted.

## Risks / mitigations

- **Contract drift breaks iOS.** Mitigation: reuse admin.py's exact JSON field names and the iOS `Codable` models as the contract; smoke-test the endpoints before cutting iOS over.
- **Memory search endpoint shape.** The proxy's search/list must match admin.py's `/api/search/unified` + entity-query behavior; verify against a live Memory instance.
- **Deploy flag-day.** `deploy.sh`/systemd must flip to gateway-only in the same change that deletes `alfred-admin`/`alfred-api`.
