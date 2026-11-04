package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMCPGuideTestServer returns a server that answers GET /api/projects with a
// single project, so runMCPGuide can resolve a project ID without a real API.
func newMCPGuideTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "proj-123", "name": "Test Project"},
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// setupMCPGuideEnv points the guide at the mock server with the given account
// key and clears project-token env vars so the active key is deterministic.
func setupMCPGuideEnv(t *testing.T, serverURL, accountKey string) {
	t.Helper()
	mcpGuideShowSecret = false
	origNoColor := noColor
	noColor = true
	t.Cleanup(func() { noColor = origNoColor })

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_ACCOUNT_API_KEY", accountKey)
	t.Setenv("MEMORY_API_KEY", "")
	t.Setenv("MEMORY_PROJECT_API_KEY", "")
	t.Setenv("MEMORY_PROJECT_TOKEN", "")
}

func runMCPGuideCmd(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	// Use a fresh, parentless command wrapping runMCPGuide: cobra's Execute()
	// delegates to the root when the command HasParent(), which would parse the
	// test binary's os.Args instead of running runMCPGuide.
	cmd := &cobra.Command{Use: "mcp-guide", RunE: runMCPGuide}
	cmd.Flags().BoolVar(&mcpGuideShowSecret, "show-secret", false, "")
	cmd.Flags().String("config", "", "")
	cmd.SetArgs(args)
	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	require.NoError(t, err)
	stdout, stderr, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()
	return stdout, stderr
}

func TestMCPGuideRedactsSecretByDefault(t *testing.T) {
	server := newMCPGuideTestServer(t)
	setupMCPGuideEnv(t, server.URL, "dummy-key-123")

	stdout, stderr := runMCPGuideCmd(t)

	// The live credential must never reach stdout.
	assert.NotContains(t, stdout, "dummy-key-123", "account key must be redacted by default")

	// The Streamable HTTP endpoint must be the primary remote option.
	assert.Contains(t, stdout, server.URL+"/api/mcp", "streamable HTTP endpoint missing")
	assert.Contains(t, stdout, "X-Project-ID", "project resolution header missing")
	assert.Contains(t, stdout, "proj-123", "project ID missing from snippets")
	assert.Contains(t, stdout, "${MEMORY_PROJECT_TOKEN}", "placeholder missing")

	// No account-scoped warning without --show-secret.
	assert.NotContains(t, stderr, "account-scoped")
}

func TestMCPGuideShowSecretPrintsKeyAndWarns(t *testing.T) {
	server := newMCPGuideTestServer(t)
	setupMCPGuideEnv(t, server.URL, "dummy-key-123")

	stdout, stderr := runMCPGuideCmd(t, "--show-secret")

	assert.Contains(t, stdout, "dummy-key-123", "--show-secret must print the live key")
	assert.Contains(t, stdout, server.URL+"/api/mcp", "streamable HTTP endpoint missing")

	// The warning goes to stderr (never into the pasted snippet) and is
	// account-scoped because no project token is set.
	assert.Contains(t, stderr, "account-scoped", "account-scoped warning missing on stderr")
	assert.NotContains(t, stdout, "account-scoped", "warning must not pollute the snippet")
}

func TestMCPGuideProjectTokenDoesNotWarn(t *testing.T) {
	server := newMCPGuideTestServer(t)
	setupMCPGuideEnv(t, server.URL, "dummy-key-123")
	// A project token takes precedence over the account key and is project-scoped.
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_projsecret")

	stdout, stderr := runMCPGuideCmd(t, "--show-secret")

	assert.Contains(t, stdout, "emt_projsecret", "--show-secret must print the project token")
	assert.NotContains(t, stdout, "dummy-key-123", "account key must not leak when a project token is active")
	assert.NotContains(t, stderr, "account-scoped", "project token is not account-scoped")
}

// TestMCPGuideSnippetsUseClientConfigKeys pins the root key of every snippet to
// the one the target client reads. The SSE block shipped under a bare `servers`
// key, which Claude Desktop ignores silently: the client starts with no Memory
// server and nothing in the output hints at why (#2268).
func TestMCPGuideSnippetsUseClientConfigKeys(t *testing.T) {
	server := newMCPGuideTestServer(t)
	setupMCPGuideEnv(t, server.URL, "dummy-key-123")

	stdout, _ := runMCPGuideCmd(t)

	// Claude Desktop / Claude Code / Cursor read `mcpServers` — both the
	// Streamable HTTP snippet and the legacy SSE snippet target those clients.
	assert.Equal(t, 2, strings.Count(stdout, `"mcpServers":`),
		"the HTTP and SSE snippets must both use the mcpServers root key")
	// OpenCode's own config uses `mcp`.
	assert.Contains(t, stdout, `"mcp":`, "the OpenCode snippet must use the mcp root key")
	// A bare `servers` root key is not read by any client we emit a snippet for.
	assert.NotContains(t, stdout, `"servers":`, "no snippet may use a bare servers root key")

	// The SSE snippet still has to address a real endpoint for the project.
	assert.Contains(t, stdout, server.URL+"/api/mcp/sse/proj-123",
		"SSE endpoint missing the project-scoped path")
}
