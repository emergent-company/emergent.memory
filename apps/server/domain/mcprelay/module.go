package mcprelay

import "go.uber.org/fx"

// Module provides the MCP Relay domain.
//
// Exposes:
//   - WebSocket endpoint: GET /api/mcp-relay/connect
//     Remote MCP providers open outbound connections here and register their tools.
//   - REST endpoints:
//     GET  /api/mcp-relay/sessions                      – list connected instances
//     GET  /api/mcp-relay/sessions/:id/tools            – get tool list for an instance
//     POST /api/mcp-relay/sessions/:id/call             – call a tool on an instance
var Module = fx.Module("mcprelay",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
