package mcpregistry

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidTarget(t *testing.T) {
	require.True(t, ValidTarget(TargetServer))
	require.True(t, ValidTarget(TargetDevice))
	require.False(t, ValidTarget("builtin"))
	require.False(t, ValidTarget(""))
}

func TestDeviceTransport(t *testing.T) {
	require.Equal(t, "http", deviceTransport(ServerTypeHTTP))
	require.Equal(t, "sse", deviceTransport(ServerTypeSSE))
	require.Equal(t, "stdio", deviceTransport(ServerTypeStdio))
	require.Equal(t, "stdio", deviceTransport(ServerTypeBuiltin))
}

func TestMergeDecrypted(t *testing.T) {
	enc := testEncryptor(t)
	secret := encryptForTest(t, enc, "s3cr3t")

	out, err := mergeDecrypted(enc, map[string]any{"A": "b"}, map[string]EncryptedSecret{"TOKEN": secret}, "env")
	require.NoError(t, err)
	require.Equal(t, "b", out["A"], "plaintext values must be preserved")
	require.Equal(t, "s3cr3t", out["TOKEN"], "decrypted secret must be merged")
}

func TestMergeDecryptedNilEncryptorFails(t *testing.T) {
	enc := testEncryptor(t)
	secret := encryptForTest(t, enc, "s3cr3t")

	_, err := mergeDecrypted(nil, nil, map[string]EncryptedSecret{"TOKEN": secret}, "env")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestMergeDecryptedNoSecrets(t *testing.T) {
	out, err := mergeDecrypted(nil, map[string]any{"A": "b"}, nil, "env")
	require.NoError(t, err)
	require.Equal(t, "b", out["A"])
}

// --- CreateServer target validation (early-return paths, no DB needed) ---

func newNoDBSvc(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(nil, nil, nil, slog.Default())
	require.NoError(t, err)
	return svc
}

func TestCreateServerInvalidTarget(t *testing.T) {
	svc := newNoDBSvc(t)
	cmd := "echo"
	_, err := svc.CreateServer(context.Background(), "p", &CreateMCPServerDTO{
		Name: "s", Type: ServerTypeStdio, Command: &cmd, Target: "bogus",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid target")
}

func TestCreateServerInstanceIDRequiresDevice(t *testing.T) {
	svc := newNoDBSvc(t)
	cmd := "echo"
	inst := "instance-1"
	_, err := svc.CreateServer(context.Background(), "p", &CreateMCPServerDTO{
		Name: "s", Type: ServerTypeStdio, Command: &cmd, Target: TargetServer, TargetInstanceID: &inst,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only valid when target is 'device'")
}

func TestCreateServerDeviceRequiresAuth(t *testing.T) {
	// A device-targeted create with no authenticated user must be refused
	// fail-closed (authorizeDeviceWrite → auth.RequireUser).
	svc := newNoDBSvc(t)
	cmd := "echo"
	_, err := svc.CreateServer(context.Background(), "p", &CreateMCPServerDTO{
		Name: "s", Type: ServerTypeStdio, Command: &cmd, Target: TargetDevice,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrDeviceTargetForbidden)
}

// --- resolveServerForTool excludes device rows ---

func TestResolveServerForTool_DeviceRowExcludedRawMode(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		{ID: "s1", ProjectID: "p1", Name: "devsrv", Enabled: true, Target: TargetDevice},
	}}
	pm := newTestProxyManager(repo)

	_, _, err := pm.resolveServerForTool(context.Background(), "p1", "devsrv_tool")
	require.Error(t, err, "a device-targeted row must never resolve for outbound calls")
	require.Contains(t, err.Error(), "not found")
}

func TestResolveServerForTool_DeviceRowExcludedSlugMode(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		{ID: "s1", ProjectID: "p1", Name: "My Device", Enabled: true, Target: TargetDevice},
	}}
	pm := newTestProxyManager(repo)

	_, _, err := pm.resolveServerForTool(context.Background(), "p1", "my_device_tool")
	require.Error(t, err, "slug-mode must also exclude device rows")
}

func TestResolveServerForTool_ServerRowStillResolves(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "myserver", true), // Target empty == server
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "myserver_search")
	require.NoError(t, err)
	require.Equal(t, "myserver", srv.Name)
	require.Equal(t, "search", tool)
}
