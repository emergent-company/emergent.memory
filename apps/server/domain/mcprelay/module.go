package mcprelay

import (
	"context"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

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
	fx.Invoke(registerRelayProvider),
)

// relayAdapter adapts mcprelay.Service to mcp.RelayToolProvider.
type relayAdapter struct {
	svc *Service
}

func (a *relayAdapter) ListByProject(projectID string) []*mcp.RelaySession {
	sessions := a.svc.ListByProject(projectID)
	out := make([]*mcp.RelaySession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, &mcp.RelaySession{
			ProjectID:  s.ProjectID,
			InstanceID: s.InstanceID,
			Tools:      s.Tools,
		})
	}
	return out
}

func (a *relayAdapter) CallTool(ctx context.Context, projectID, instanceID, toolName string, args map[string]any) (map[string]any, error) {
	return a.svc.CallTool(ctx, projectID, instanceID, toolName, args)
}

// registerRelayProvider wires the relay service into the MCP service so relay
// tools appear in tools/list and relay tool calls are forwarded correctly.
func registerRelayProvider(relaySvc *Service, mcpSvc *mcp.Service) {
	mcpSvc.SetRelayProvider(&relayAdapter{svc: relaySvc})
}
