package mcpregistry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
)

func TestResolveImportTarget(t *testing.T) {
	assert.Equal(t, TargetDevice, resolveImportTarget(importTargetAuto, mcpconfig.TransportStdio))
	assert.Equal(t, TargetServer, resolveImportTarget(importTargetAuto, mcpconfig.TransportHTTP))
	assert.Equal(t, TargetServer, resolveImportTarget(importTargetAuto, mcpconfig.TransportSSE))
	assert.Equal(t, TargetServer, resolveImportTarget(TargetServer, mcpconfig.TransportStdio))
	assert.Equal(t, TargetDevice, resolveImportTarget(TargetDevice, mcpconfig.TransportHTTP))
	assert.Equal(t, TargetDevice, resolveImportTarget("", mcpconfig.TransportStdio))
}

func TestBuildImportCreateDTOStdio(t *testing.T) {
	instance := "dev-1"
	dto, err := buildImportCreateDTO(mcpconfig.Server{
		Name:      "github",
		Transport: mcpconfig.TransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "pkg"},
		Env:       map[string]string{"GITHUB_TOKEN": "x", "PLAIN": "y"},
	}, importTargetAuto, &instance, true)
	require.NoError(t, err)

	assert.Equal(t, "github", dto.Name)
	assert.Equal(t, ServerTypeStdio, dto.Type)
	assert.Equal(t, TargetDevice, dto.Target)
	require.NotNil(t, dto.TargetInstanceID)
	assert.Equal(t, "dev-1", *dto.TargetInstanceID)
	require.NotNil(t, dto.Command)
	assert.Equal(t, "npx", *dto.Command)
	assert.Equal(t, []string{"-y", "pkg"}, dto.Args)
	assert.Equal(t, map[string]any{"GITHUB_TOKEN": "x", "PLAIN": "y"}, dto.Env)
	assert.Equal(t, []string{"GITHUB_TOKEN"}, dto.SecretEnvKeys)
	assert.Nil(t, dto.URL)
	assert.Nil(t, dto.Headers)
}

func TestBuildImportCreateDTORemote(t *testing.T) {
	dto, err := buildImportCreateDTO(mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   map[string]string{"Authorization": "Bearer x", "X-Other": "y"},
	}, importTargetAuto, nil, true)
	require.NoError(t, err)

	assert.Equal(t, ServerTypeHTTP, dto.Type)
	assert.Equal(t, TargetServer, dto.Target)
	assert.Nil(t, dto.TargetInstanceID)
	assert.Equal(t, []string{"Authorization", "X-Other"}, dto.SecretHeadersKeys)
	assert.Equal(t, map[string]any{"Authorization": "Bearer x", "X-Other": "y"}, dto.Headers)
	assert.Nil(t, dto.Command)
	assert.Nil(t, dto.Env)
}

func TestBuildImportCreateDTOWithoutEncryption(t *testing.T) {
	dto, err := buildImportCreateDTO(mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportSSE,
		URL:       "https://example.com/sse",
		Headers:   map[string]string{"Authorization": "Bearer x"},
	}, importTargetAuto, nil, false)
	require.NoError(t, err)
	assert.Empty(t, dto.SecretHeadersKeys, "no encryption means no secret keys")
	assert.Equal(t, map[string]any{"Authorization": "Bearer x"}, dto.Headers)
}

func TestUpdateDTOFromImportClearsStaleFields(t *testing.T) {
	command := "npx"
	create := &CreateMCPServerDTO{
		Name:    "x",
		Type:    ServerTypeStdio,
		Command: &command,
		Target:  TargetDevice,
	}
	update := updateDTOFromImport(create)

	require.NotNil(t, update.Name)
	assert.Equal(t, "x", *update.Name)
	require.NotNil(t, update.Type)
	assert.Equal(t, ServerTypeStdio, *update.Type)
	assert.NotNil(t, update.Args, "args must be non-nil to clear stale args")
	assert.NotNil(t, update.Env, "env must be non-nil to clear stale env")
	assert.NotNil(t, update.Headers, "headers must be non-nil to clear stale headers")
	require.NotNil(t, update.Target)
	assert.Equal(t, TargetDevice, *update.Target)
}

func TestSecretEnvKeys(t *testing.T) {
	got := secretEnvKeys(map[string]string{
		"GITHUB_TOKEN":     "x",
		"API_KEY":          "x",
		"DB_PASSWORD":      "x",
		"PLAIN_NAME":       "x",
		"OPENAI_APIKEY":    "x",
		"AUTH_HEADER":      "x",
		"PRIVATE_KEY_PATH": "x",
	})
	assert.Equal(t, []string{"API_KEY", "AUTH_HEADER", "DB_PASSWORD", "GITHUB_TOKEN", "OPENAI_APIKEY", "PRIVATE_KEY_PATH"}, got)
}
