package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	mcpsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	agentMCPTestProjectID = "11111111-1111-1111-1111-111111111111"
	agentMCPTestAgentID   = "22222222-2222-2222-2222-222222222222"
)

// --- command wiring ---------------------------------------------------------

func TestAgentMCPEndpointCommandStructure(t *testing.T) {
	// The group is registered under the agents command ...
	found := false
	for _, sub := range agentsCmd.Commands() {
		if sub.Name() == "mcp-endpoint" {
			found = true
			break
		}
	}
	assert.True(t, found, "mcp-endpoint should be registered under agents")

	// ... and offers the endpoint lifecycle plus keys and sessions.
	names := map[string]bool{}
	for _, sub := range agentMCPEndpointCmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"show", "create", "revoke", "keys", "sessions"} {
		assert.True(t, names[want], "expected mcp-endpoint subcommand %q", want)
	}

	keyNames := map[string]bool{}
	for _, sub := range agentMCPKeysCmd.Commands() {
		keyNames[sub.Name()] = true
	}
	for _, want := range []string{"create", "list", "revoke", "rotate"} {
		assert.True(t, keyNames[want], "expected keys subcommand %q", want)
	}
}

func TestAgentMCPRegistrySubgroupUntouched(t *testing.T) {
	// The MCP registry subgroup must not gain any endpoint command.
	for _, sub := range mcpServersCmd.Commands() {
		assert.NotEqual(t, "mcp-endpoint", sub.Name())
	}
	for _, sub := range mcpServersCmd.Commands() {
		for _, nested := range sub.Commands() {
			assert.NotEqual(t, "mcp-endpoint", nested.Name())
		}
	}
}

func TestAgentMCPKeysCreateRequiresLabel(t *testing.T) {
	flag := agentMCPKeysCreateCmd.Flags().Lookup("label")
	require.NotNil(t, flag, "--label should be defined")
	assert.Contains(t, flag.Annotations[cobra.BashCompOneRequiredFlag], "true",
		"--label should be marked required")
}

func TestAgentMCPReadCommandsHaveJSONFlag(t *testing.T) {
	for _, c := range []*cobra.Command{agentMCPEndpointShowCmd, agentMCPKeysListCmd, agentMCPSessionsCmd} {
		assert.NotNil(t, c.Flags().Lookup("json"), "%s should support --json", c.Name())
	}
}

func TestAgentMCPDestructiveCommandsHaveYesFlag(t *testing.T) {
	for _, c := range []*cobra.Command{agentMCPEndpointRevokeCmd, agentMCPKeysRevokeCmd, agentMCPKeysRotateCmd} {
		assert.NotNil(t, c.Flags().Lookup("yes"), "%s should support --yes", c.Name())
	}
}

func TestAgentMCPArgs(t *testing.T) {
	assert.NotNil(t, agentMCPEndpointShowCmd.Args)
	assert.NotNil(t, agentMCPEndpointCreateCmd.Args)
	assert.NotNil(t, agentMCPEndpointRevokeCmd.Args)

	// key revoke/rotate take exactly a key id
	assert.Error(t, agentMCPKeysRevokeCmd.Args(agentMCPKeysRevokeCmd, []string{}))
	assert.NoError(t, agentMCPKeysRevokeCmd.Args(agentMCPKeysRevokeCmd, []string{"key-1"}))
	assert.Error(t, agentMCPKeysRotateCmd.Args(agentMCPKeysRotateCmd, []string{}))
	assert.NoError(t, agentMCPKeysRotateCmd.Args(agentMCPKeysRotateCmd, []string{"key-1"}))
}

// --- agent slug -------------------------------------------------------------

func TestAgentSlug(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"My Agent", "my-agent"},
		{"Gamma Agent", "gamma-agent"},
		{"Hello,  World!", "hello-world"},
		{"  --Foo__Bar--  ", "foo-bar"},
		{"Café Agent", "caf-agent"},
		{"already-a-slug", "already-a-slug"},
		{"", ""},
		{"---", ""},
		{strings.Repeat("A", 80), strings.Repeat("a", 63)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, agentSlug(tt.in))
		})
	}
}

// --- secret rendering -------------------------------------------------------

func TestRenderAgentMCPKeySecret_ShowsSecretExactlyOnce(t *testing.T) {
	const token = "emt_very_secret_value"
	secret := &mcpsdk.AgentMCPKeySecret{
		AgentMCPKey: mcpsdk.AgentMCPKey{
			ID:     "key-1",
			Label:  "Claude Desktop",
			Status: "active",
		},
		Token:  token,
		MCPURL: "https://api.example.com/api/mcp/agents/" + agentMCPTestAgentID,
	}

	var buf bytes.Buffer
	require.NoError(t, renderAgentMCPKeySecret(&buf, secret))
	out := buf.String()

	assert.Equal(t, 1, strings.Count(out, token), "the secret must appear exactly once")
	assert.Contains(t, out, secret.MCPURL, "the MCP URL must be shown")
	assert.Contains(t, out, "will not be shown again", "the one-time warning must be present")
	assert.Contains(t, out, "Claude Desktop")
	assert.Contains(t, out, "key-1")
}

func TestRenderAgentMCPKeySecret_NilSecret(t *testing.T) {
	var buf bytes.Buffer
	assert.Error(t, renderAgentMCPKeySecret(&buf, nil))
}

// --- session status ---------------------------------------------------------

func TestValidAgentMCPSessionStatus(t *testing.T) {
	for _, s := range []string{"active", "running", "interrupted", "expired"} {
		assert.True(t, validAgentMCPSessionStatus(s), "%q should be valid", s)
	}
	for _, s := range []string{"", "nope", "ACTIVE", "done"} {
		assert.False(t, validAgentMCPSessionStatus(s), "%q should be invalid", s)
	}
}

// --- error mapping ----------------------------------------------------------

func TestMapAgentMCPError(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		code     string
		contains string
	}{
		{"duplicate label", http.StatusConflict, "agent_mcp_key_label_exists", "already exists"},
		{"endpoint exists", http.StatusConflict, "agent_mcp_endpoint_exists", "already exists"},
		{"not found", http.StatusNotFound, "not_found", "not found"},
		{"forbidden", http.StatusForbidden, "forbidden", "project admin permission"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapAgentMCPError(&sdkerrors.Error{StatusCode: tt.status, Code: tt.code, Message: "server said no"})
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.contains))
		})
	}

	assert.NoError(t, mapAgentMCPError(nil))

	plain := assert.AnError
	assert.Equal(t, plain, mapAgentMCPError(plain), "unrecognized errors pass through unchanged")
}

// --- destructive confirmation -----------------------------------------------

func TestConfirmDestructive(t *testing.T) {
	cmd := &cobra.Command{}

	// --yes always proceeds.
	assert.NoError(t, confirmDestructive(cmd, "Revoke thing", true))

	// Test runs are non-interactive (stdin is not a terminal): without --yes it
	// must refuse rather than silently proceed.
	err := confirmDestructive(cmd, "Revoke thing", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
}

// --- agent reference resolution ---------------------------------------------

func agentMCPMock(t *testing.T, runtimeAgents, definitions []map[string]any) *sdk.Client {
	t.Helper()
	handlers := map[string]http.HandlerFunc{
		"/api/projects/" + agentMCPTestProjectID + "/agents": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": runtimeAgents})
		},
		"/api/projects/" + agentMCPTestProjectID + "/agent-definitions": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": definitions})
		},
	}
	mock := testutil.NewMockServer(handlers)
	t.Cleanup(mock.Close)

	sdkClient, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		ProjectID: agentMCPTestProjectID,
	})
	require.NoError(t, err)
	return sdkClient
}

func TestResolveAgentsMCPReference(t *testing.T) {
	runtimeAgents := []map[string]any{
		{"id": "agent-1", "name": "Gamma Agent"},
		{"id": "agent-2", "name": "Beta"},
	}
	definitions := []map[string]any{
		{"id": "def-1", "name": "My Fancy Agent", "flowType": "graph_object_processor"},
	}
	client := agentMCPMock(t, runtimeAgents, definitions)

	t.Run("uuid passes through", func(t *testing.T) {
		got, err := resolveAgentsMCPReference(client, agentMCPTestAgentID)
		require.NoError(t, err)
		assert.Equal(t, agentMCPTestAgentID, got)
	})

	t.Run("runtime name is matched ignoring case", func(t *testing.T) {
		got, err := resolveAgentsMCPReference(client, "gamma agent")
		require.NoError(t, err)
		assert.Equal(t, "agent-1", got)
	})

	t.Run("definition slug is matched", func(t *testing.T) {
		got, err := resolveAgentsMCPReference(client, "my-fancy-agent")
		require.NoError(t, err)
		assert.Equal(t, "def-1", got)
	})

	t.Run("definition name is matched", func(t *testing.T) {
		got, err := resolveAgentsMCPReference(client, "My Fancy Agent")
		require.NoError(t, err)
		assert.Equal(t, "def-1", got)
	})

	t.Run("unknown reference is rejected", func(t *testing.T) {
		_, err := resolveAgentsMCPReference(client, "does-not-exist")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no agent matches")
	})

	t.Run("empty reference is rejected", func(t *testing.T) {
		_, err := resolveAgentsMCPReference(client, "   ")
		require.Error(t, err)
	})
}

func TestResolveAgentsMCPReference_Ambiguous(t *testing.T) {
	client := agentMCPMock(t, []map[string]any{
		{"id": "agent-1", "name": "Dup"},
		{"id": "agent-2", "name": "Dup"},
	}, nil)

	_, err := resolveAgentsMCPReference(client, "dup")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

// --- JSON DTO round-trip ----------------------------------------------------

func TestAgentMCPDescriptorJSON_NoSecretOnRead(t *testing.T) {
	// A listed key decodes without a token field: read paths cannot leak it.
	raw := `{"keys":[{"id":"key-1","endpointId":"ep-1","label":"ci","status":"active","createdAt":"2026-09-16T00:00:00Z","updatedAt":"2026-09-16T00:00:00Z"}],"total":1}`
	var list mcpsdk.AgentMCPKeyList
	require.NoError(t, json.Unmarshal([]byte(raw), &list))
	require.Len(t, list.Keys, 1)

	encoded, err := json.Marshal(list.Keys[0])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "token")
}
