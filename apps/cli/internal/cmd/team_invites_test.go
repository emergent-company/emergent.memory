package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Invite tokens are bearer-equivalent: the listing must not print any part of
// one. The old cell echoed the whole token below 13 characters and the first
// 12 characters above it, for every pending invite (#2131).
//
// The sentinels are deliberately low-entropy and obviously fake; .gitleaks.toml
// scans this repo.
func TestTeamInvitesListDoesNotPrintTokens(t *testing.T) {
	const (
		shortToken = "Q7Z"                                // 3 chars: echoed verbatim before the fix
		longToken  = "invite-do-not-use-0123456789abcdef" // 35 chars: 12-char prefix leaked before
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/invites/pending", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":             "inv-1",
				"projectName":    "Alpha",
				"organizationId": "org-0000",
				"role":           "project_viewer",
				"token":          shortToken,
			},
			{
				"id":             "inv-2",
				"projectName":    "Beta",
				"organizationId": "org-0000",
				"role":           "project_admin",
				"token":          longToken,
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", srv.URL)
	t.Setenv("MEMORY_ACCOUNT_API_KEY", "test-account-key-do-not-use-0000")
	t.Setenv("MEMORY_API_KEY", "")
	t.Setenv("MEMORY_PROJECT_TOKEN", "")
	t.Setenv("MEMORY_PROJECT_API_KEY", "")

	origJSON := teamInvitesJSON
	teamInvitesJSON = false
	t.Cleanup(func() { teamInvitesJSON = origJSON })

	// A fresh, parentless command: cobra delegates to the root command when a
	// command has a parent, which would parse the test binary's os.Args.
	cmd := &cobra.Command{Use: "invites", RunE: runTeamInvites}

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	stdout, _, readErr := capture.Read()
	capture.Restore()

	require.NoError(t, err)
	require.NoError(t, readErr)

	// The listing still lists both invites...
	assert.Contains(t, stdout, `Project "Alpha"`)
	assert.Contains(t, stdout, `Project "Beta"`)

	// ...without disclosing any part of either token.
	assert.NotContains(t, stdout, shortToken, "a token shorter than the disclosure threshold must not be printed at all")
	assert.NotContains(t, stdout, longToken, "the full token must not be printed")
	assert.NotContains(t, stdout, longToken[:12], "the old 12-character prefix must not be printed")
	assert.NotContains(t, stdout, "token:", "no per-invite token column")

	// ...and it says where the token comes from instead of leaving a dead end.
	assert.Contains(t, stdout, "token from the invitation email")
}
