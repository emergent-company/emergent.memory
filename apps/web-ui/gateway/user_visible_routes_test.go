package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- user-visible route registration guard ---
//
// main.go registers the gateway's routes inline in main(), and every other test
// builds its own echo.Echo with just the routes it exercises. That means a route
// line deleted from main.go leaves the whole suite green: the handler becomes
// dead code and the page 404s in production (see #43, where /orgs/:id/inbox was
// covered by a test helper that registered the route itself).
//
// These tests assert the user-visible registrations against the registration
// source text, mirroring TestAgentMCPRoutesRegistered in
// agent_mcp_endpoint_routes_test.go. They are the cheap interim guard until
// route registration is extracted from main() and can be asserted via
// e.Routes(); they deliberately do not try to cover the API/share surface.
//
// A surface may register through a registrar function instead of inline (the
// public deploy portal does, see registerDeployPortalRoutes) so its own tests can
// exercise the real wiring. The table then names the registrar's file, and
// TestDeployPortalRegistrarWired separately asserts main.go still calls it —
// otherwise a dropped call would 404 the page while the registrar's test passed.
//
// The public column is the second half of the guard: a route that is reachable
// before sign-in must say so here, and the classification is proven against
// publicAuthPath (auth.go) rather than assumed. That is the closest this
// source-text guard can get to the middleware-derived tiers of the server's
// Route Authority Guard (apps/server/domain only), which does not scan the
// gateway.

// gatewayRoute is one user-visible route that main.go must keep registered.
type gatewayRoute struct {
	method  string // HTTP method, as used by echo: GET, POST, ...
	path    string // echo path, e.g. "/orgs/:id/inbox"
	handler string // Server method name, without the "s." receiver
	// raw, when set, is the exact source fragment to require instead of the
	// constructed e.METHOD("path", s.handler) line. Used for inline handlers.
	raw string
	// file is the Go file expected to hold the registration. Empty means
	// main.go; a surface registered through a registrar (the deploy portal,
	// see registerDeployPortalRoutes) lives in the registrar's own file, and
	// TestDeployPortalRegistrarWired keeps main.go's call to it honest.
	file string
	// public marks a route reachable before sign-in. The classification is not
	// documentation: TestUserVisibleRoutePublicClassification proves it against
	// publicAuthPath, which is the single decision point authDispatch uses.
	public bool
}

// registrationSource is the file the table entry's registration must appear in.
func (r gatewayRoute) registrationSource() string {
	if r.file != "" {
		return r.file
	}
	return "main.go"
}

// registration is the exact source line this route must keep.
func (r gatewayRoute) registration() string {
	if r.raw != "" {
		return r.raw
	}
	return "e." + r.method + `("` + r.path + `", s.` + r.handler + ")"
}

// userVisibleRoutes is the table of routes the guard covers: the inbox surface
// (including the org-scoped entry point), every org-scoped route, the project
// settings/approvals hub, and the public deploy portal. It is derived from
// main.go (and from the deploy portal registrar in deploy_portal.go); update it
// when a route in one of those surfaces is added, renamed, or removed.
var userVisibleRoutes = []gatewayRoute{
	// Public deploy portal (deploy_portal.go registerDeployPortalRoutes). Every
	// route here is reachable before sign-in, which is why each carries
	// public: true — the classification is asserted against publicAuthPath, not
	// assumed. Logout is a POST because it clears the session cookie.
	{method: "GET", path: "/deploy", handler: "deployPortalPage", file: "deploy_portal.go", public: true},
	{method: "GET", path: "/deploy/oauth/start", handler: "deployOAuthStart", file: "deploy_portal.go", public: true},
	{method: "GET", path: "/deploy/oauth/callback", handler: "deployOAuthCallback", file: "deploy_portal.go", public: true},
	{method: "POST", path: "/deploy/oauth/logout", handler: "deployOAuthLogout", file: "deploy_portal.go", public: true},
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
	{method: "GET", path: "/orgs/:id/settings/oauth-apps/new", handler: "uiOrgOAuthAppNew"},
	{method: "POST", path: "/orgs/:id/settings/oauth-apps", handler: "uiOrgOAuthAppCreate"},
	{method: "GET", path: "/orgs/:id/settings/oauth-apps/:appId", handler: "uiOrgOAuthAppDetail"},
	{method: "POST", path: "/orgs/:id/settings/oauth-apps/:appId", handler: "uiOrgOAuthAppUpdate"},
	{method: "POST", path: "/orgs/:id/settings/oauth-apps/:appId/delete", handler: "uiOrgOAuthAppDelete"},
	{method: "GET", path: "/orgs/:id/settings/oauth-clients/:providerType", handler: "uiOrgOAuthClientDetail"},
	{method: "POST", path: "/orgs/:id/settings/oauth-clients/:providerType", handler: "uiOrgOAuthClientUpsert"},
	{method: "POST", path: "/orgs/:id/settings/oauth-clients/:providerType/delete", handler: "uiOrgOAuthClientDelete"},
	{method: "POST", path: "/orgs/:id/rename", handler: "uiOrgRename"},
	{method: "GET", path: "/orgs/:id/tool-settings", raw: `e.GET("/orgs/:id/tool-settings", func(c echo.Context) error {`},
	{method: "POST", path: "/orgs/:id/tool-settings/:toolName", handler: "uiOrgToolSettingUpdate"},
	{method: "POST", path: "/orgs/:id/tool-settings/:toolName/delete", handler: "uiOrgToolSettingDelete"},
	{method: "POST", path: "/orgs/:id/delete", handler: "uiDeleteOrg"},

	// Project settings hub and approvals.
	{method: "GET", path: "/settings/mcp-servers/import", handler: "uiMCPImportPage"},
	{method: "POST", path: "/settings/mcp-servers/import", handler: "uiMCPImport"},
	{method: "GET", path: "/settings", handler: "uiProjectSettings"},
	{method: "GET", path: "/settings/assistant", handler: "uiProjectAssistantSettings"},
	{method: "GET", path: "/settings/extraction", handler: "uiProjectExtractionSettings"},
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
	{method: "GET", path: "/settings/credentials/apps/new", handler: "uiProjectOAuthAppNew"},
	{method: "POST", path: "/settings/credentials/apps", handler: "uiProjectOAuthAppCreate"},
	{method: "GET", path: "/settings/credentials/apps/:appId", handler: "uiProjectOAuthAppDetail"},
	{method: "POST", path: "/settings/credentials/apps/:appId", handler: "uiProjectOAuthAppUpdate"},
	{method: "POST", path: "/settings/credentials/apps/:appId/delete", handler: "uiProjectOAuthAppDelete"},
	{method: "GET", path: "/settings/credentials/:providerType", handler: "uiProjectCredential"},
	{method: "POST", path: "/settings/credentials/:providerType", handler: "uiProjectOAuthClientUpsert"},
	{method: "POST", path: "/settings/credentials/:providerType/delete", handler: "uiProjectOAuthClientDelete"},
	{method: "GET", path: "/settings/oauth-clients", handler: "uiProjectOAuthClientsRedirect"},
	{method: "GET", path: "/settings/source-apps", handler: "uiSourceApps"},
	{method: "POST", path: "/settings/source-apps", handler: "uiSourceAppCreate"},
	{method: "GET", path: "/settings/source-apps/new", handler: "uiSourceAppNew"},
	{method: "GET", path: "/settings/source-apps/:appId", handler: "uiSourceAppDetail"},
	{method: "POST", path: "/settings/source-apps/:appId", handler: "uiSourceAppUpdate"},
	{method: "POST", path: "/settings/source-apps/:appId/delete", handler: "uiSourceAppDelete"},
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
	{method: "POST", path: "/settings/extraction/agent", handler: "uiProjectExtractionAgent"},
	{method: "POST", path: "/settings/extraction/whisper", handler: "uiProjectExtractionWhisper"},
	{method: "POST", path: "/settings/extraction/limits", handler: "uiProjectExtractionLimits"},
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
	{method: "GET", path: "/settings/mcp-servers/shares/:id/duplicate", handler: "uiMCPShareDuplicatePage"},
	{method: "GET", path: "/settings/mcp-servers/shares/:id/edit", handler: "uiMCPShareEditPage"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/update", handler: "uiMCPShareUpdate"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/revoke", handler: "uiMCPShareRevoke"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/rotate", handler: "uiMCPShareRotate"},
	{method: "POST", path: "/settings/mcp-servers/shares/:id/invite", handler: "uiMCPShareInvite"},
	{method: "GET", path: "/settings/mcp-servers/:id/edit", handler: "uiMCPEditPage"},
	{method: "GET", path: "/settings/mcp-servers/:id/tools", handler: "uiMCPServerToolsFragment"},
	{method: "POST", path: "/settings/mcp-servers/:id/update", handler: "uiMCPUpdate"},
	{method: "POST", path: "/settings/mcp-servers/:id/delete", handler: "uiMCPDelete"},
	{method: "POST", path: "/settings/mcp-servers/:id/toggle", handler: "uiMCPToggle"},
	{method: "GET", path: "/settings/mcp-servers/:id/oauth/start", handler: "uiMCPOAuthStart"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/disconnect", handler: "uiMCPOAuthDisconnect"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/client", handler: "uiMCPOAuthClient"},
	{method: "POST", path: "/settings/mcp-servers/:id/oauth/client/clear", handler: "uiMCPOAuthClientClear"},
}

// guardRegistrationFiles are the source files route registrations are read
// from: main.go for everything registered inline, plus the file holding a
// standalone registrar's routes (the deploy portal).
var guardRegistrationFiles = []string{"main.go", "deploy_portal.go"}

// TestUserVisibleRoutesRegistered asserts every route in userVisibleRoutes is
// still registered with the expected handler. Deleting the registration line, or
// renaming the handler without updating the route, fails with the route and the
// exact line the source must contain.
func TestUserVisibleRoutesRegistered(t *testing.T) {
	sources := make(map[string]string, len(guardRegistrationFiles))
	for _, name := range guardRegistrationFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(src)
	}

	for _, r := range userVisibleRoutes {
		file := r.registrationSource()
		src, ok := sources[file]
		if !ok {
			t.Fatalf("route %s %s names %q, which is not in guardRegistrationFiles", r.method, r.path, file)
		}
		want := r.registration()
		if !strings.Contains(src, want) {
			t.Errorf("route %s %s is not registered in %s (registration dropped or handler renamed).\nexpected %s line:\n\t%s", r.method, r.path, file, file, want)
		}
	}
}

// TestDeployPortalRegistrarWired closes the hole the table alone cannot: the
// deploy portal's four registrations moved out of main.go into
// registerDeployPortalRoutes, so a deleted *call* in main.go would drop the
// routes while their own test (which calls the registrar) stayed green.
func TestDeployPortalRegistrarWired(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	const want = "s.registerDeployPortalRoutes(e)"
	if !strings.Contains(string(src), want) {
		t.Fatalf("main.go no longer calls %s, so the public deploy portal routes are not wired in production", want)
	}
}

// TestUserVisibleRoutePublicClassification proves the public column rather than
// trusting it: publicAuthPath is the single decision point authDispatch uses to
// let a request through before sign-in, so every table entry's classification
// must match it exactly. A route that quietly becomes public (or stops being
// public) without the table agreeing fails here.
func TestUserVisibleRoutePublicClassification(t *testing.T) {
	for _, r := range userVisibleRoutes {
		if got := publicAuthPath(r.path); got != r.public {
			t.Errorf("%s %s: publicAuthPath = %v, table says public = %v; the table and auth.go's allowlist must agree", r.method, r.path, got, r.public)
		}
	}
}

// TestDeployPortalRoutesArePublic is the other half of the classification: it
// enumerates the routes the real registrar installs, so a route added to
// registerDeployPortalRoutes without a publicAuthPath entry (and therefore a
// sign-in redirect in session mode) fails here even if nobody updates the table.
func TestDeployPortalRoutesArePublic(t *testing.T) {
	s := &Server{cfg: deployTestConfig("https://railway.test/token", "https://railway.test/graphql")}
	e := echo.New()
	s.registerDeployPortalRoutes(e)

	routes := e.Routes()
	if len(routes) == 0 {
		t.Fatal("registerDeployPortalRoutes registered no routes")
	}
	seen := make(map[string]bool, len(routes))
	for _, rt := range routes {
		seen[rt.Method+" "+rt.Path] = true
		if !publicAuthPath(rt.Path) {
			t.Errorf("%s %s is registered by the deploy portal registrar but publicAuthPath rejects it; add it to auth.go's allowlist (the portal is reachable before sign-in)", rt.Method, rt.Path)
		}
	}
	for _, want := range []string{"GET /deploy", "GET /deploy/oauth/start", "GET /deploy/oauth/callback", "POST /deploy/oauth/logout"} {
		if !seen[want] {
			t.Errorf("deploy portal registrar is missing %s", want)
		}
	}
	// The dead allowlist entry this issue removed: /deploy/ has no handler.
	if publicAuthPath("/deploy/") {
		t.Error(`publicAuthPath("/deploy/") = true; no route serves /deploy/ — remove the dead allowlist entry`)
	}
}

// TestUserVisibleRouteTableExhaustive keeps the table above honest: every
// e.METHOD registration in a guard-covered surface and file must have a table
// entry. Without this, a newly added route would silently sit outside the guard,
// and a renamed path would only update one side.
func TestUserVisibleRouteTableExhaustive(t *testing.T) {
	tabled := make(map[string]bool, len(userVisibleRoutes))
	for _, r := range userVisibleRoutes {
		tabled[r.method+" "+r.path] = true
	}

	lineRE := regexp.MustCompile(`^\s*e\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"`)
	for _, name := range guardRegistrationFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
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
				t.Errorf("%s:%d registers %s %q but it is missing from userVisibleRoutes; add it so the guard stays exhaustive", name, i+1, method, path)
			}
		}
	}
}

// inRouteGuardScope reports whether a route path belongs to one of the surfaces
// userVisibleRoutes covers: org-scoped pages, the project settings/approvals
// hub, the inbox surface, and the public deploy portal. The rest of the UI, plus
// the /api and /share groups, are deliberately out of scope for this guard.
func inRouteGuardScope(path string) bool {
	switch path {
	case "/orgs", "/settings", "/inbox", "/partial/inbox-list", "/deploy":
		return true
	}
	return strings.HasPrefix(path, "/orgs/") ||
		strings.HasPrefix(path, "/settings/") ||
		strings.HasPrefix(path, "/inbox/") ||
		strings.HasPrefix(path, "/deploy/")
}
