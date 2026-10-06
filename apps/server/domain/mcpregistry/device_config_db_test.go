package mcpregistry_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// fakePusher records device-config pushes and resolves device ownership for
// authorization tests, without touching the real relay.
type fakePusher struct {
	mu             sync.Mutex
	instancePushes map[string][]mcpregistry.DeviceServerConfig // key: proj+"/"+inst
	pushCounts     map[string]int                              // key: proj+"/"+inst
	owners         map[string]string                           // key: proj+"/"+inst
}

func newFakePusher() *fakePusher {
	return &fakePusher{
		instancePushes: map[string][]mcpregistry.DeviceServerConfig{},
		pushCounts:     map[string]int{},
		owners:         map[string]string{},
	}
}

func (f *fakePusher) setOwner(projectID, instanceID, owner string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[projectID+"/"+instanceID] = owner
}

func (f *fakePusher) PushConfigToInstance(_ context.Context, projectID, instanceID string, servers []mcpregistry.DeviceServerConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := projectID + "/" + instanceID
	f.instancePushes[key] = append([]mcpregistry.DeviceServerConfig(nil), servers...)
	f.pushCounts[key]++
}

func (f *fakePusher) PushConfigToProject(_ context.Context, projectID, ownerSubject string, admin bool, resolve func(ctx context.Context, instanceID string) ([]mcpregistry.DeviceServerConfig, error)) {
	// Not exercised by these tests; the relay-side fan-out is covered in mcprelay.
}

func (f *fakePusher) OwnerSubject(projectID, instanceID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.owners[projectID+"/"+instanceID]
	return o, ok
}

func (f *fakePusher) instancePushed(projectID, instanceID string) []mcpregistry.DeviceServerConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instancePushes[projectID+"/"+instanceID]
}

// instancePushCount reports how many times PushConfigToInstance was called for
// an instance (each call replaces instancePushes, so a len check alone cannot
// distinguish a second hydration push from the first create-time push).
func (f *fakePusher) instancePushCount(projectID, instanceID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pushCounts[projectID+"/"+instanceID]
}

type DeviceConfigSuite struct {
	testutil.BaseSuite
	enc    *crypto.Encryptor
	pusher *fakePusher
	svc    *mcpregistry.Service
}

func TestDeviceConfigSuite(t *testing.T) { suite.Run(t, new(DeviceConfigSuite)) }

func (s *DeviceConfigSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_device_config")
	s.BaseSuite.SetupSuite()
}

func (s *DeviceConfigSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	key, err := crypto.GenerateKey()
	s.Require().NoError(err)
	s.enc, err = crypto.NewEncryptor(key)
	s.Require().NoError(err)
	s.pusher = newFakePusher()
	s.svc, err = mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), nil, s.enc, slog.Default())
	s.Require().NoError(err)
	s.svc.RegisterDeviceConfigPusher(s.pusher)
}

// seedServer inserts an MCP server row with an explicit target.
func (s *DeviceConfigSuite) seedServer(projectID, name, target string, instID *string) string {
	serverID := uuid.New().String()
	var inst any
	if instID != nil {
		inst = *instID
	}
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_servers (id, project_id, name, enabled, type, target, target_instance_id, created_at, updated_at)
		VALUES (?, ?, ?, true, 'stdio', ?, ?, NOW(), NOW())
	`, serverID, projectID, name, target, inst).Exec(s.Ctx)
	s.Require().NoError(err)
	return serverID
}

func (s *DeviceConfigSuite) seedTool(serverID, toolName string) {
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_server_tools (id, server_id, tool_name, enabled, created_at)
		VALUES (?, ?, ?, true, NOW())
	`, uuid.New().String(), serverID, toolName).Exec(s.Ctx)
	s.Require().NoError(err)
}

func strPtr(s string) *string { return &s }

// --- device server list (task 3.5) ---

func (s *DeviceConfigSuite) TestListDeviceServersFiltered() {
	s.seedServer(s.ProjectID, "broadcast", "device", nil)
	s.seedServer(s.ProjectID, "dev1", "device", strPtr("dev-1"))
	s.seedServer(s.ProjectID, "dev2", "device", strPtr("dev-2"))
	s.seedServer(s.ProjectID, "server-row", "server", nil)

	all, err := s.svc.ListDeviceServers(s.Ctx, s.ProjectID, nil)
	s.Require().NoError(err)
	s.Require().Len(all, 3, "unfiltered list must return every device row and no server row")

	filtered, err := s.svc.ListDeviceServers(s.Ctx, s.ProjectID, strPtr("dev-1"))
	s.Require().NoError(err)
	var names []string
	for _, srv := range filtered {
		names = append(names, srv.Name)
	}
	s.Require().ElementsMatch([]string{"broadcast", "dev1"}, names,
		"filtered list must return broadcast + the matching instance, excluding other instances and server rows")
}

// --- task 1.3: device rows excluded from every outbound path ---

func (s *DeviceConfigSuite) TestFindAllEnabledToolsExcludesDevice() {
	serverSrv := s.seedServer(s.ProjectID, "srv", "server", nil)
	s.seedTool(serverSrv, "server-tool")
	deviceSrv := s.seedServer(s.ProjectID, "dev", "device", nil)
	s.seedTool(deviceSrv, "device-tool")

	tools, err := s.svc.GetEnabledToolsForProject(s.Ctx, s.ProjectID)
	s.Require().NoError(err)

	var names []string
	for _, t := range tools {
		names = append(names, t.ToolName)
	}
	s.Require().ElementsMatch([]string{"server-tool"}, names,
		"device-targeted server tools must be excluded from the tool pool")
}

func (s *DeviceConfigSuite) TestOutboundPathsRejectDevice() {
	deviceSrv := s.seedServer(s.ProjectID, "dev", "device", nil)

	_, err := s.svc.InspectServer(s.Ctx, deviceSrv, s.ProjectID)
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceServerNotOutbound, "InspectServer must refuse device rows")

	_, err = s.svc.DiscoverAndSyncTools(s.Ctx, deviceSrv, s.ProjectID)
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceServerNotOutbound, "DiscoverAndSyncTools must refuse device rows")

	_, err = s.svc.CallToolOnServer(s.Ctx, s.ProjectID, deviceSrv, "tool", nil, nil, "")
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceServerNotOutbound, "CallToolOnServer must refuse device rows")

	err = s.svc.SyncServerTools(s.Ctx, s.ProjectID, deviceSrv, []mcpregistry.DiscoveredTool{{Name: "x"}})
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceServerNotOutbound, "SyncServerTools must refuse device rows")
}

// --- task 3.4: authorization ---

func (s *DeviceConfigSuite) TestDeviceWriteOwnerAllowed() {
	s.pusher.setOwner(s.ProjectID, "dev-1", testutil.RegularUser.ID)

	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.RegularUser.ID})
	_, err := s.svc.CreateServer(ctx, s.ProjectID, s.deviceCreateDTO("dev-1"))
	s.Require().NoError(err, "the device owner may create a device-targeted row")
}

func (s *DeviceConfigSuite) TestDeviceWriteNonOwnerDenied() {
	s.pusher.setOwner(s.ProjectID, "dev-1", testutil.RegularUser.ID)

	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.NoScopeUser.ID})
	_, err := s.svc.CreateServer(ctx, s.ProjectID, s.deviceCreateDTO("dev-1"))
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceTargetForbidden,
		"a non-owner, non-admin member must be denied")
}

func (s *DeviceConfigSuite) TestDeviceWriteAdminAllowed() {
	s.pusher.setOwner(s.ProjectID, "dev-1", testutil.RegularUser.ID)

	// AdminUser is a project_admin of s.ProjectID (BaseSuite) — may target any device.
	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	_, err := s.svc.CreateServer(ctx, s.ProjectID, s.deviceCreateDTO("dev-1"))
	s.Require().NoError(err, "a project admin may target any device")
}

func (s *DeviceConfigSuite) TestDeviceWriteBroadcastNonAdminDenied() {
	// A broadcast row has no owner identity and is hydrated to every opted-in
	// device of the project, so a non-admin must not be able to create one
	// (cross-member RCE). Only a project admin may broadcast.
	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.NoScopeUser.ID})
	_, err := s.svc.CreateServer(ctx, s.ProjectID, s.deviceCreateDTO(""))
	s.Require().ErrorIs(err, mcpregistry.ErrDeviceTargetForbidden,
		"a non-admin member must not create a broadcast device row")
}

func (s *DeviceConfigSuite) TestDeviceWriteBroadcastAdminAllowed() {
	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	_, err := s.svc.CreateServer(ctx, s.ProjectID, s.deviceCreateDTO(""))
	s.Require().NoError(err, "a project admin may broadcast to every eligible device")
}

func (s *DeviceConfigSuite) deviceCreateDTO(instanceID string) *mcpregistry.CreateMCPServerDTO {
	cmd := "echo"
	dto := &mcpregistry.CreateMCPServerDTO{
		Name:    "dev-server-" + uuid.New().String()[:8],
		Type:    mcpregistry.ServerTypeStdio,
		Command: &cmd,
		Target:  mcpregistry.TargetDevice,
	}
	if instanceID != "" {
		dto.TargetInstanceID = &instanceID
	}
	return dto
}

// --- task 3.2/3.3: resolve + decrypt + push ---

func (s *DeviceConfigSuite) TestCreateDeviceServerPushesDecryptedConfig() {
	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	inst := "dev-1"
	cmd := "echo"
	_, err := s.svc.CreateServer(ctx, s.ProjectID, &mcpregistry.CreateMCPServerDTO{
		Name:             "dev-server",
		Type:             mcpregistry.ServerTypeStdio,
		Command:          &cmd,
		Target:           mcpregistry.TargetDevice,
		TargetInstanceID: &inst,
		Env:              map[string]any{"TOKEN": "supersecret"},
		SecretEnvKeys:    []string{"TOKEN"},
	})
	s.Require().NoError(err)

	s.Require().Eventually(func() bool {
		servers := s.pusher.instancePushed(s.ProjectID, inst)
		if len(servers) == 0 {
			return false
		}
		// The decrypted secret must be delivered in the resolved config.
		return servers[0].Env["TOKEN"] == "supersecret"
	}, 3*time.Second, 20*time.Millisecond, "device config must be pushed with the decrypted secret")
}

func (s *DeviceConfigSuite) TestHydrateDevicePushesResolvedConfig() {
	ctx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})
	inst := "dev-1"
	cmd := "echo"
	_, err := s.svc.CreateServer(ctx, s.ProjectID, &mcpregistry.CreateMCPServerDTO{
		Name:             "dev-server",
		Type:             mcpregistry.ServerTypeStdio,
		Command:          &cmd,
		Target:           mcpregistry.TargetDevice,
		TargetInstanceID: &inst,
	})
	s.Require().NoError(err)

	// Drain the create-time push.
	s.Require().Eventually(func() bool {
		return s.pusher.instancePushCount(s.ProjectID, inst) >= 1
	}, 3*time.Second, 20*time.Millisecond)

	// Hydration triggers a fresh resolve + push (used on device reconnect).
	s.svc.HydrateDevice(s.ProjectID, inst)
	s.Require().Eventually(func() bool {
		return s.pusher.instancePushCount(s.ProjectID, inst) >= 2
	}, 3*time.Second, 20*time.Millisecond, "hydration must push the resolved config")
	s.Require().NotEmpty(s.pusher.instancePushed(s.ProjectID, inst),
		"hydration must deliver the resolved config")
}
