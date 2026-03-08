# Authentication

The Go SDK supports three authentication modes, selected via `sdk.Config.Auth.Mode`.

## API Key Mode

Used for standalone Emergent deployments. Sets the `X-API-Key` header on every request.

```go
client, err := sdk.New(sdk.Config{
    ServerURL: "http://localhost:9090",
    Auth: sdk.AuthConfig{
        Mode:   "apikey",
        APIKey: "your-standalone-api-key",
    },
})
```

!!! tip "Auto-detection of API tokens"
    When `Mode: "apikey"` is used, the SDK automatically checks whether the key starts with
    `emt_`. If it does, the key is treated as an API token (Bearer auth) rather than a
    standalone API key (`X-API-Key`). This means you can use `Mode: "apikey"` for both
    key types and let `auth.IsAPIToken` handle the routing.

## API Token Mode

Project-scoped tokens with the `emt_*` prefix. Sets the `Authorization: Bearer <token>` header.

```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:   "apitoken",
        APIKey: "emt_abc123...",  // always an emt_* token in this mode
    },
})
```

This is equivalent to using `Mode: "apikey"` with an `emt_*` prefixed key, but makes the
intent explicit.

## OAuth Device Flow

For full Emergent deployments with OIDC/OAuth. Initiates an interactive device-code flow, then
polls for a token and stores credentials to disk.

```go
client, err := sdk.NewWithDeviceFlow(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        ClientID:  "emergent-sdk",
        CredsPath: "~/.emergent/credentials.json",
    },
})
```

`NewWithDeviceFlow` will:

1. Call `auth.DiscoverOIDC(serverURL)` to discover the OIDC configuration
2. Initiate the device flow and print the verification URL and user code to stdout
3. Poll for a token until the user completes authorization
4. Store the credentials at `CredsPath` for future use

!!! warning "OAuth not available via `sdk.New`"
    `Mode: "oauth"` in `sdk.New` returns an error; OAuth always requires `NewWithDeviceFlow`.

## Config Reference

```go
type Config struct {
    ServerURL  string       // Required: base URL of the Emergent server
    Auth       AuthConfig
    OrgID      string       // Optional: default organization ID
    ProjectID  string       // Optional: default project ID
    HTTPClient *http.Client // Optional: custom HTTP client (default: 30s timeout)
}

type AuthConfig struct {
    Mode      string // "apikey", "apitoken", or "oauth"
    APIKey    string // Key/token value for apikey or apitoken mode
    CredsPath string // File path for OAuth credential storage
    ClientID  string // OAuth client ID
}
```

## auth Package

For advanced use, the `auth` package is importable directly:

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
```

See the [auth reference](reference/auth.md) for the full `Provider` interface, credential
helpers, and OIDC discovery.
