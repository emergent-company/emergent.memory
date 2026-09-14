# Upstream SDK patch (proposed) — device-flow scopes include `offline_access`

Status: **opened as [emergent-company/emergent.memory#429](https://github.com/emergent-company/emergent.memory/pull/429)** (branch `fix/sdk-device-flow-offline-access`; may be adjusted during review). This is the single upstream change the connector depends on (design.md D11). It targets the Memory core repo, not this one; if it stalls, the fallback below applies.

- Target repo: `emergent-company/emergent.memory` (local checkout `/root/emergent.memory`)
- Package: `apps/server/pkg/sdk/auth`, plus a one-line pass-through in `apps/server/pkg/sdk`
- Suggested branch: `fix/sdk-device-flow-offline-access`
- Nature: additive, backward compatible

## Problem

`sdk/auth.OAuthProvider.InitiateDeviceFlow` hardcodes `scope = "openid profile email"`. Zitadel therefore returns **no refresh token**, so any client that relies on the SDK device flow for a long-running process (the connector daemon) cannot renew its session. The Memory CLI works around this by carrying its own `internal/auth` that passes `offline_access`; the SDK does not.

## Patch

`apps/server/pkg/sdk/auth/oauth.go`:

```diff
 type OAuthProvider struct {
 	mu          sync.RWMutex
 	oidcConfig  *OIDCConfig
 	credentials *Credentials
 	clientID    string
 	credsPath   string
+	scopes      []string
 }
 
+// DefaultDeviceFlowScopes requests a refresh token (offline_access) so a
+// stored session can be renewed without re-authentication.
+var DefaultDeviceFlowScopes = []string{"openid", "profile", "email", "offline_access"}
+
 // NewOAuthProvider creates a new OAuth provider.
 // If credentials exist at credsPath and are valid, they will be loaded.
 // Otherwise, device flow must be initiated manually.
 func NewOAuthProvider(oidcConfig *OIDCConfig, clientID, credsPath string) *OAuthProvider {
+	return NewOAuthProviderWithScopes(oidcConfig, clientID, credsPath, nil)
+}
+
+// NewOAuthProviderWithScopes is NewOAuthProvider with explicit device-flow
+// scopes; an empty/nil slice uses DefaultDeviceFlowScopes.
+func NewOAuthProviderWithScopes(oidcConfig *OIDCConfig, clientID, credsPath string, scopes []string) *OAuthProvider {
 	// Expand tilde in path
 	if strings.HasPrefix(credsPath, "~/") {
 		home, _ := os.UserHomeDir()
 		credsPath = filepath.Join(home, credsPath[2:])
 	}
 
 	provider := &OAuthProvider{
 		oidcConfig: oidcConfig,
 		clientID:   clientID,
 		credsPath:  credsPath,
+		scopes:     scopes,
 	}
 
 	// Try to load existing credentials
 	if creds, err := LoadCredentials(credsPath); err == nil {
 		provider.credentials = creds
 	}
 
 	return provider
 }
```

```diff
 func (p *OAuthProvider) InitiateDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
 	data := url.Values{}
 	data.Set("client_id", p.clientID)
-	data.Set("scope", "openid profile email")
+	scopes := p.scopes
+	if len(scopes) == 0 {
+		scopes = DefaultDeviceFlowScopes
+	}
+	data.Set("scope", strings.Join(scopes, " "))
 
 	req, err := http.NewRequestWithContext(ctx, "POST", p.oidcConfig.DeviceAuthorizationEndpoint, strings.NewReader(data.Encode()))
 	// ... unchanged ...
 }
```

`apps/server/pkg/sdk/sdk.go` (optional pass-through so `NewWithDeviceFlow` callers can override):

```diff
 type AuthConfig struct {
 	Mode      string // "apikey", "apitoken", or "oauth"
 	APIKey    string // For API key mode (standalone X-API-Key) or API token mode (emt_* Bearer token)
 	CredsPath string // For OAuth credential storage
 	ClientID  string // For OAuth mode
+	Scopes    []string // Optional: OAuth device-flow scopes (defaults to auth.DefaultDeviceFlowScopes)
 }
```

```diff
-	authProvider := auth.NewOAuthProvider(oidcConfig, cfg.Auth.ClientID, cfg.Auth.CredsPath)
+	authProvider := auth.NewOAuthProviderWithScopes(oidcConfig, cfg.Auth.ClientID, cfg.Auth.CredsPath, cfg.Auth.Scopes)
```

## Tests to add (`auth` package, `httptest`-based, no live provider)

1. `TestInitiateDeviceFlowRequestsOfflineAccess` — stand up a stub device-authorization endpoint that captures the posted form; assert the `scope` field contains `offline_access` and that `client_id` is sent.
2. `TestInitiateDeviceFlowCustomScopes` — construct via `NewOAuthProviderWithScopes(..., []string{"openid","custom"})`; assert the posted scope is exactly `openid custom`.
3. `TestNewOAuthProviderWithScopesEmptyUsesDefault` — nil and empty slices fall back to `DefaultDeviceFlowScopes`.
4. `TestPollForTokenPersistsRefreshToken` — stub token endpoint returns `{access_token, refresh_token, expires_in}`; after `PollForToken`, assert the credentials file exists with mode 0600 and contains the refresh token.

## PR

Title: `fix(sdk): request offline_access in OAuth device flow so sessions can refresh`

Body:

> `OAuthProvider.InitiateDeviceFlow` requests only `openid profile email`, so the provider issues no refresh token and long-running SDK clients cannot renew a session. This adds an explicit, overridable scope set whose default includes `offline_access`, preserving existing call sites via `NewOAuthProvider`, and threads optional scopes through `sdk.AuthConfig`. Additive and backward compatible; covered by unit tests with stubbed endpoints.

## Fallback if not merged

The token poll does not carry the scope; only the device-authorization **request** does. So a connector can issue the device-authorization request itself (with `offline_access`) and then reuse `OAuthProvider.PollForToken(deviceCode, ...)` unchanged, keeping SDK refresh and credential storage. Only `InitiateDeviceFlow` (~30 lines) is replaced. See design.md D11.
