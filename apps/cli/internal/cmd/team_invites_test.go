package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/invitations"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Invite tokens are bearer-equivalent. Both output paths are pinned here so the
// split stays a decision rather than drifting into an accident (#2134):
//
//   - the human listing must not print any part of a token (#2131, #2133);
//   - --json must keep returning the token, because it is explicit machine
//     output and the only programmatic way to recover an accept link.
//
// The sentinels are deliberately low-entropy and obviously fake; .gitleaks.toml
// scans this repo.
const (
	invitesShortToken = "Q7Z"                                // 3 chars: echoed verbatim before the fix
	invitesLongToken  = "invite-do-not-use-0123456789abcdef" // 35 chars: 12-char prefix leaked before
)

// newInvitesTestServer serves two pending invitations, one short token and one
// long token, and points the CLI's account client at it.
func newInvitesTestServer(t *testing.T) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/invites/pending", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":             "inv-1",
				"projectName":    "Alpha",
				"organizationId": "org-0000",
				"role":           "project_viewer",
				"token":          invitesShortToken,
			},
			{
				"id":             "inv-2",
				"projectName":    "Beta",
				"organizationId": "org-0000",
				"role":           "project_admin",
				"token":          invitesLongToken,
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
}

// runTeamInvitesForTest executes runTeamInvites with teamInvitesJSON forced to
// jsonMode and returns captured stdout. A fresh, parentless command is used:
// cobra delegates to the root command when a command has a parent, which would
// parse the test binary's os.Args.
func runTeamInvitesForTest(t *testing.T, jsonMode bool) string {
	t.Helper()

	origJSON := teamInvitesJSON
	teamInvitesJSON = jsonMode
	t.Cleanup(func() { teamInvitesJSON = origJSON })

	cmd := &cobra.Command{Use: "invites", RunE: runTeamInvites}

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	stdout, _, readErr := capture.Read()
	capture.Restore()

	require.NoError(t, err)
	require.NoError(t, readErr)

	return stdout
}

// The old cell echoed the whole token below 13 characters and the first 12
// characters above it, for every pending invite (#2131).
func TestTeamInvitesListDoesNotPrintTokens(t *testing.T) {
	newInvitesTestServer(t)

	stdout := runTeamInvitesForTest(t, false)

	// The listing still lists both invites...
	assert.Contains(t, stdout, `Project "Alpha"`)
	assert.Contains(t, stdout, `Project "Beta"`)

	// ...without disclosing any part of either token.
	assert.NotContains(t, stdout, invitesShortToken, "a token shorter than the disclosure threshold must not be printed at all")
	assert.NotContains(t, stdout, invitesLongToken, "the full token must not be printed")
	assert.NotContains(t, stdout, invitesLongToken[:12], "the old 12-character prefix must not be printed")
	assert.NotContains(t, stdout, "token:", "no per-invite token column")

	// ...and it says where the token comes from instead of leaving a dead end.
	assert.Contains(t, stdout, "token from the invitation email")
}

// --json is the deliberate exception: it is explicit machine output and the
// only programmatic source of the accept token, so it returns the API response
// verbatim. This pins that contract — if someone redacts the token here, the
// human-listing guarantee above is no reason for it, and this test says so
// explicitly rather than letting the exposure look accidental (#2134).
func TestTeamInvitesJSONIncludesToken(t *testing.T) {
	newInvitesTestServer(t)

	stdout := runTeamInvitesForTest(t, true)

	var got []invitations.PendingInvite
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must be one JSON array")
	require.Len(t, got, 2)

	assert.Equal(t, "inv-1", got[0].ID)
	assert.Equal(t, invitesShortToken, got[0].Token, "--json must not redact the token")
	assert.Equal(t, invitesLongToken, got[1].Token, "--json must not truncate the token")
	assert.Contains(t, stdout, invitesLongToken, "the full token must survive serialization")
}
