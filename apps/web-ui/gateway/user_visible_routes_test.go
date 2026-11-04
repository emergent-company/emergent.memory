package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// --- user-visible route registration guard ---
//
// main.go registers the gateway's routes inline in main(), and every other test
// builds its own echo.Echo with just the routes it exercises. That means a route
// line deleted from main.go leaves the whole suite green: the handler becomes
// dead code and the page 404s in production (see #43, where /orgs/:id/inbox was
// covered by a test helper that registered the route itself).
//
// These tests assert the user-visible registrations against main.go's source
// text, mirroring TestAgentMCPRoutesRegistered in
// agent_mcp_endpoint_routes_test.go. They are the cheap interim guard until
// route registration is extracted from main() and can be asserted via
// e.Routes(); they deliberately do not try to cover the API/share surface.

// gatewayRoute is one user-visible route that main.go must keep registered.
type gatewayRoute struct {
	method  string // HTTP method, as used by echo: GET, POST, ...
	path    string // echo path, e.g. "/orgs/:id/inbox"
	handler string // Server method name, without the "s." receiver
	// raw, when set, is the exact source fragment to require instead of the
	// constructed e.METHOD("path", s.handler) line. Used for inline handlers.
	raw string
}

// registration is the exact main.go line this route must keep.
func (r gatewayRoute) registration() string {
	if r.raw != "" {
		return r.raw
	}
	return "e." + r.method + `("` + r.path + `", s.` + r.handler + ")"
}

// userVisibleRoutes is the table of routes the guard covers: the inbox surface
// (including the org-scoped entry point), every org-scoped route, and the
// project settings/approvals hub. It is derived from main.go; update it when a
// route in one of those surfaces is added, renamed, or removed.
var userVisibleRoutes = []gatewayRoute{
	// Inbox surface: the project inbox page, its partials and preferences, and
	// the org-scoped inbox entry point below (in the org-scoped routes).
	{method: "GET", path: "/inbox", handler: "uiInbox"},
	{method: "GET", path: "/partial/inbox-list", handler: "uiInboxList"},
	{method: "GET", path: "/inbox/preferences", handler: "uiNotificationPreferences"},
	{method: "POST", path: "/inbox/preferences", handler: "uiNotificationPreferencesSave"},
	{method: "GET", path: "/inbox/merge-preview", handler: "uiInboxMergePreview"},
	{method: "POST", path: "/inbox/merge", handler: "uiInboxMerge"},

	// Org-scoped routes: org pages, members/invites, org settings, and the
	// inbox entry point the topbar bell links to (/o/{org}/inbox rewrites here).
	{method: "GET", path: "/orgs", handler: "uiOrgs"},
	{method: "GET", path: "/orgs/new", handler: "uiOrgCreatePage"},
	{method: "POST", path: "/orgs", handler: "uiCreateOrg"},
	{method: "GET", path: "/orgs/:id", handler: "uiOrg"},
	{method: "GET", path: "/orgs/:id/inbox", handler: "uiOrgInbox"},
	{method: "GET", path: "/orgs/:id/members", handler: "uiOrgMembers"},
	{method: "GET", path: "/orgs/:id/members/add", handler: "uiOrgMemberAddPage"},
	{method: "POST", path: "/orgs/:id/members", handler: "uiOrgAddMember"},
	{method: "POST", path: "/orgs/:id/members/:userId/role", handler: "uiOrgChangeMemberRole"},
	{method: "POST", path: "/orgs/:id/members/:userId/remove", handler: "uiOrgRemoveMember"},
	{method: "POST", path: "/orgs/:id/invites/:inviteId/resend", handler: "uiOrgResendInvite"},
	{method: "POST", path: "/orgs/:id/invites/:inviteId/revoke", handler: "uiOrgRevokeInvite"},
	{method: "GET", path: "/orgs/:id/invite", handler: "uiOrgInvitePage"},
	{method: "POST", path: "/orgs/:id/invite", handler: "uiOrgInviteCreate"},
	{method: "GET", path: "/orgs/:id/settings", handler: "uiOrgSettings"},
	{method: "GET", path: "/orgs/:id/settings/general", handler: "uiOrgSettingsGeneral"},
	{method: "GET", path: "/orgs/:id/settings/danger-zone", handler: "uiOrgSettingsDangerZone"},
	{method: "GET", path: "/orgs/:id/settings/oauth-clients", handler: "uiOrgOAuthClients"},
	{method: "POST", path: "/orgs/:id/settings/oauth-clients/:providerType", handler: "uiOrgOAuthClientUpsert"},
	{method: "POST", path: "/orgs/:id/settings/oauth-clients/:providerType/delete", handler: "uiOrgOAuthClientDelete"},
	{method: "POST", path: "/orgs/:id/rename", handler: "uiOrgRename"},
	{method: "GET", path: "/orgs/:id/tool-settings", raw: `e.GET("/orgs/:id/tool-settings", func(c echo.Context) error {`},
	{method: "POST", path: "/orgs/:id/tool-settings/:toolName", handler: "uiOrgToolSettingUpdate"},
	{method: "POST", path: "/orgs/:id/tool-settings/:toolName/delete", handler: "uiOrgToolSettingDelete"},
	{method: "POST", path: "/orgs/:id/delete", handler: "uiDeleteOrg"},

	// Project settings hub and approvals.
	{method: "GET", path: "/settings", handler: "uiProjectSettings"},
	{method: "GET", path: "/settings/assistant", handler: "uiProjectAssistantSettings"},
	{method: "GET", path: "/settings/overrides", handler: "uiProjectOverridesSettings"},
	{method: "GET", path: "/settings/providers", handler: "uiProjectProvidersSettings"},
	{method: "GET", path: "/settings/providers/new", handler: "uiProjectProviderNew"},
	{method: "GET", path: "/settings/providers/:provider/edit", handler: "uiProjectProviderEdit"},
	{method: "GET", path: "/settings/voice", handler: "uiProjectVoiceSettings"},
	{method: "GET", path: "/settings/devices", handler: "uiConnectedDevices"},
	{method: "GET", path: "/settings/connected-apps", handler: "uiConnectedApps"},
	{method: "POST", path: "/settings/connected-apps/:id/revoke", handler: "uiRevokeConnectedApp"},
	{method: "GET", path: "/settings/budget", handler: "uiProjectBudgetSettings"},
	{method: "GET", path: "/settings/credentials", handler: "uiProjectCredentials"},
	{method: "GET", path: "/settings/credentials/saved/fields", handler: "uiSavedCredentialFields"},
	{method: "POST", path: "/settings/credentials/saved", handler: "uiSavedCredentialCreate"},
	{method: "POST", path: "/settings/credentials/saved/:id/update", handler: "uiSavedCredentialUpdate"},
	{method: "POST", path: "/settings/credentials/saved/:id/delete", handler: "uiSavedCredentialDelete"},
	{method: "GET", path: "/settings/credentials/:providerType", handler: "uiProjectCredential"},
	{method: "POST", path: "/settings/credentials/:providerType", handler: "uiProjectOAuthClientUpsert"},
	{method: "POST", path: "/settings/credentials/:providerType/delete", handler: "uiProjectOAuthClientDelete"},
	{method: "GET", path: "/settings/oauth-clients", handler: "uiProjectOAuthClientsRedirect"},
	{method: "GET", path: "/settings/approvals", handler: "uiApprovals"},
	{method: "GET", path: "/settings/mcp-nodes", handler: "uiMCPNodesRedirect"},
	{method: "POST", path: "/settings/mcp-nodes/remove", handler: "uiMCPNodesRemove"},
	{method: "POST", path: "/settings/approvals/:questionId/respond", handler: "uiApproveReject"},
	{method: "POST", path: "/settings/approvals/:questionId/cancel", handler: "uiCancelApproval"},
	{method: "POST", path: "/settings/project/:field", handler: "uiProjectSettingsProjectField"},
	{method: "POST", path: "/settings/overrides", handler: "uiProjectSettingsOverride"},
	{method: "POST", path: "/settings/overrides/:agentName/delete", handler: "uiProjectSettingsOverrideDelete"},
	{method: "POST", path: "/settings/remember/:field", handler: "uiProjectSettingsRememberField"},
	{method: "POST", path: "/settings/assistant", handler: "uiProjectSettingsAssistant"},
	{method: "POST", path: "/settings/editor", handler: "uiProjectSettingsEditor"},
	{method: "POST", path: "/settings/merge", handler: "uiProjectSettingsMerge"},
	{method: "POST", path: "/settings/voice", handler: "uiProjectSettingsVoice"},
	{method: "POST", path: "/settings/voice/:key", handler: "uiProjectVoiceField"},
	{method: "POST", path: "/settings/voice/group/:group", handler: "uiProjectVoiceGroup"},
	{method: "POST", path: "/settings/providers/:provider/:model", handler: "uiProjectSettingsProviderOverride"},
	{method: "POST", path: "/settings/providers/:provider/:model/delete", handler: "uiProjectSettingsProviderOverrideDelete"},
	{method: "POST", path: "/settings/providers/config", handler: "uiProjectProviderConfig"},
	{method: "POST", path: "/settings/providers/test", handler: "uiProjectProviderTestConnection"},
	{method: "POST", path: "/settings/providers/check-url", handler: "uiProjectProviderCheckBaseURL"},
	{method: "POST", path: "/settings/providers/model-config", handler: "uiProjectModelConfig"},
	{method: "POST", path: "/settings/providers/model-config/test", handler: "uiProjectDefaultModelTest"},
	{method: "POST", path: "/settings/providers/:provider/test", handler: "uiProjectProviderTest"},
	{method: "POST", path: "/settings/providers/:provider/remove", handler: "uiProjectProviderRemove"},
	{method: "POST", path: "/settings/devices/:id/revoke", handler: "uiRevokeDevice"},
	{method: "GET", path: "/settings/tokens", handler: "uiAPITokens"},
	{method: "GET", path: "/settings/tokens/new", handler: "uiAPITokensNewPage"},
	{method: "POST", path: "/settings/tokens/new", handler: "uiAPITokensCreate"},
	{method: "GET", path: "/settings/tokens/:tokenId/edit", handler: "uiAPITokensEditPage"},
	{method: "POST", path: "/settings/tokens/:tokenId/revoke", handler: "uiAPITokensRevoke"},
	{method: "POST", path: "/settings/tokens/:tokenId/scopes", handler: "uiAPITokensScopes"},
	{method: "POST", path: "/settings/tokens/:tokenId/regenerate", handler: "uiAPITokensRegenerate"},
	{method: "GET", path: "/settings/members", handler: "uiSettingsMembers"},
	{method: "GET", path: "/settings/members/new", handler: "uiSettingsMemberInvitePage"},
	{method: "POST", path: "/settings/members", handler: "uiSettingsMemberInviteCreate"},
	{method: "GET", path: "/settings/members/:userId", handler: "uiSettingsMemberDetails"},
	{method: "POST", path: "/settings/members/:userId/remove", handler: "uiSettingsRemoveMember"},
	{method: "POST", path: "/settings/members/:userId/role", handler: "uiSettingsChangeMemberRole"},
	{method: "GET", path: "/settings/mcp-servers", handler: "uiMCPServers"},
	{method: "GET", path: "/settings/mcp-servers/new", handler: "uiMCPNewPage"},
	{method: "POST", path: "/settings/mcp-servers/new", handler: "uiMCPCreate"},
	{method: "GET", path: "/settings/mcp-registry", handler: "uiMCPRegistry"},
	{method: "POST", path: "/settings/mcp-registry/install", handler: "uiMCPRegistryInstall"},
	{method: "POST", path: "/settings/mcp-registry/install-to-device", handler: "uiMCPRegistryInstallToDevice"},
	{method: "GET", path: "/settings/mcp-servers/shares", handler: "uiMCPShares"},
	{method: "GET", path: "/settings/mcp-servers/shares/new", handler: "uiMCPShareNewPage"},
	{method: "POST", path: "/settings/mcp-servers/shares/new", handler: "uiMCPShareCreate"},
	{method: "GET", path: "/settings/mcp-servers/shares/:id/edit", handler: "uiMCPShareEditPage"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/update", handler: "uiMCPShareUpdate"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/revoke", handler: "uiMCPShareRevoke"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/rotate", handler: "uiMCPShareRotate"},
	{method: "GET", path: "/settings/mcp-servers/:id/edit", handler: "uiMCPEditPage"},
	{method: "POST", path: "/settings/mcp-servers/:id/update", handler: "uiMCPUpdate"},
	{method: "POST", path: "/settings/mcp-servers/:id/delete", handler: "uiMCPDelete"},
	{method: "POST", path: "/settings/mcp-servers/:id/toggle", handler: "uiMCPToggle"},
	{method: "GET", path: "/settings/mcp-servers/:id/oauth/start", handler: "uiMCPOAuthStart"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/disconnect", handler: "uiMCPOAuthDisconnect"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/client", handler: "uiMCPOAuthClient"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/client/clear", handler: "uiMCPOAuthClientClear"},
}

// TestUserVisibleRoutesRegistered asserts every route in userVisibleRoutes is
// still registered in main.go with the expected handler. Deleting the
// registration line, or renaming the handler without updating the route, fails
// with the route and the exact line main.go must contain.
func TestUserVisibleRoutesRegistered(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	main := string(src)

	for _, r := range userVisibleRoutes {
		want := r.registration()
		if !strings.Contains(main, want) {
			t.Errorf("route %s %s is not registered in main.go (registration dropped or handler renamed).\nexpected main.go line:\n\t%s", r.method, r.path, want)
		}
	}
}

// TestUserVisibleRouteTableExhaustive keeps the table above honest: every
// e.METHOD("/orgs...", "/settings..." or "/inbox..." registration in main.go
// must have a table entry. Without this, a newly added route would silently sit
// outside the guard, and a renamed path would only update one side.
func TestUserVisibleRouteTableExhaustive(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}

	tabled := make(map[string]bool, len(userVisibleRoutes))
	for _, r := range userVisibleRoutes {
		tabled[r.method+" "+r.path] = true
	}

	lineRE := regexp.MustCompile(`^\s*e\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"`)
	for i, line := range strings.Split(string(src), "\n") {
		m := lineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method, path := m[1], m[2]
		if !inRouteGuardScope(path) {
			continue
		}
		if !tabled[method+" "+path] {
			t.Errorf("main.go:%d registers %s %q but it is missing from userVisibleRoutes; add it so the guard stays exhaustive", i+1, method, path)
		}
	}
}

// inRouteGuardScope reports whether a route path belongs to one of the surfaces
// userVisibleRoutes covers: org-scoped pages, the project settings/approvals
// hub, and the inbox surface. The rest of the UI, plus the /api and /share
// groups, are deliberately out of scope for this guard.
func inRouteGuardScope(path string) bool {
	switch path {
	case "/orgs", "/settings", "/inbox", "/partial/inbox-list":
		return true
	}
	return strings.HasPrefix(path, "/orgs/") ||
		strings.HasPrefix(path, "/settings/") ||
		strings.HasPrefix(path, "/inbox/")
}
