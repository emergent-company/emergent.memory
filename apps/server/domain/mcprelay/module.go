package mcprelay

import (
	"context"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
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
	fx.Provide(provideRelayProvider),
	fx.Provide(provideDeviceConfigPusher),
	fx.Invoke(registerDeviceConfigHydration),
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

// provideRelayProvider exposes the relay service as mcp.RelayToolProvider so
// relay tools appear in tools/list and relay tool calls are forwarded correctly.
func provideRelayProvider(relaySvc *Service) mcp.RelayToolProvider {
	return &relayAdapter{svc: relaySvc}
}

// deviceConfigAdapter adapts mcprelay.Service to mcpregistry.DeviceConfigPusher,
// so mcpregistry can push resolved device configuration without importing
// mcprelay (one-way dependency).
type deviceConfigAdapter struct {
	svc *Service
}

func (a *deviceConfigAdapter) PushConfigToInstance(ctx context.Context, projectID, instanceID string, servers []mcpregistry.DeviceServerConfig) {
	a.svc.PushConfigToInstance(ctx, projectID, instanceID, servers)
}

func (a *deviceConfigAdapter) PushConfigToProject(ctx context.Context, projectID, ownerSubject string, admin bool, resolve func(ctx context.Context, instanceID string) ([]mcpregistry.DeviceServerConfig, error)) {
	a.svc.PushConfigToProject(ctx, projectID, ownerSubject, admin, resolve)
}

func (a *deviceConfigAdapter) OwnerSubject(projectID, instanceID string) (string, bool) {
	return a.svc.OwnerSubject(projectID, instanceID)
}

func provideDeviceConfigPusher(relaySvc *Service) mcpregistry.DeviceConfigPusher {
	return &deviceConfigAdapter{svc: relaySvc}
}

// registerDeviceConfigHydration wires the relay's post-ack registration hook to
// mcpregistry.HydrateDevice, so an opted-in device receives its resolved
// configuration after its register ack (never before — the connector's
// waitForAck drops pre-ack frames).
func registerDeviceConfigHydration(relaySvc *Service, registry *mcpregistry.Service) {
	relaySvc.OnRegistered(func(projectID, instanceID string) {
		registry.HydrateDevice(projectID, instanceID)
	})
}
