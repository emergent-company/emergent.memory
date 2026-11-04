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

// The tool-config cell used to echo the last four characters of each value and
// printed any value of four characters or fewer in full (#2131). It now shows
// the key name and a presence marker only, so no character of a configured
// value — short or long — reaches stdout.
//
// Exercised through `builtin-tools list`, which renders the same cell as
// `mcp-servers list`, so the assertion covers the printed output rather than a
// helper's return value.
func TestBuiltinToolsListOmitsToolConfigValues(t *testing.T) {
	// Deliberately low-entropy and obviously fake: .gitleaks.toml scans this
	// repo. `short` is the regression case (it used to print verbatim);
	// `long` used to disclose four trailing characters.
	const (
		short = "Q7Z"
		long  = "0123zzzzzzzzzzzzzzzzzzzzzzzzcdef" // fakeSecret(32)
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/builtin-tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{
				{
					"id":         "tool-1",
					"toolName":   "short_key_tool",
					"enabled":    true,
					"configKeys": []string{"api_key"},
					"config":     map[string]any{"api_key": short},
				},
				{
					"id":         "tool-2",
					"toolName":   "long_key_tool",
					"enabled":    true,
					"configKeys": []string{"api_key", "region"},
					"config":     map[string]any{"api_key": long},
				},
				{
					"id":         "tool-3",
					"toolName":   "unconfigured_tool",
					"enabled":    true,
					"configKeys": []string{"api_key"},
					"config":     map[string]any{},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", srv.URL)
	t.Setenv("MEMORY_PROJECT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("MEMORY_PROJECT_TOKEN", "emt_test-project-token-0000")
	t.Setenv("MEMORY_PROJECT_API_KEY", "")
	t.Setenv("MEMORY_API_KEY", "")
	t.Setenv("MEMORY_ACCOUNT_API_KEY", "")
	t.Setenv("MEMORY_PROJECT", "")

	origProjectID := agentProjectID
	agentProjectID = ""
	origLimit, origPage := builtinListLimit, builtinListPage
	builtinListLimit, builtinListPage = 0, 0
	t.Cleanup(func() {
		agentProjectID = origProjectID
		builtinListLimit, builtinListPage = origLimit, origPage
	})

	cmd := &cobra.Command{Use: "list", RunE: runListBuiltinTools}

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	stdout, _, readErr := capture.Read()
	capture.Restore()

	require.NoError(t, err)
	require.NoError(t, readErr)

	// The cell still reports configuration state for every tool...
	assert.Contains(t, stdout, "short_key_tool")
	assert.Contains(t, stdout, "long_key_tool")
	assert.Contains(t, stdout, "api_key=has_value", "a configured key must be marked present")
	assert.Contains(t, stdout, "region=?", "a missing key must still be flagged")
	assert.Contains(t, stdout, "unconfigured_tool [setup required: api_key]")

	// ...but never any character of a configured value.
	assert.NotContains(t, stdout, short, "a 3-character value used to print in full")
	assert.NotContains(t, stdout, long, "the full value must never print")
	assert.NotContains(t, stdout, long[:4], "no prefix of the value may print")
	assert.NotContains(t, stdout, long[len(long)-4:], "no suffix of the value may print")
	assert.NotContains(t, stdout, "…", "the old ellipsis+suffix cell is gone")

	assert.Equal(t, 2, strings.Count(stdout, "has_value"), "one marker per configured key")
}
