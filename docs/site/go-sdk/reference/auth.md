# auth

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth`

The `auth` package provides pluggable authentication for the SDK. The SDK uses it internally, but you can import it directly for advanced scenarios.

## Provider Interface

```go
type Provider interface {
    Authenticate(req *http.Request) error
    Refresh(ctx context.Context) error
}
```

All auth providers implement `Provider`. `Authenticate` adds credentials to an outgoing request; `Refresh` updates stored credentials (used by OAuth for token renewal).

## Providers

### APIKeyProvider

Sets the `X-API-Key` header. Used for standalone Emergent deployments.

```go
provider := auth.NewAPIKeyProvider("your-api-key")
```

### APITokenProvider

Sets `Authorization: Bearer <token>`. Used for project-scoped `emt_*` tokens.

```go
provider := auth.NewAPITokenProvider("emt_abc123...")
```

### OAuthProvider

Implements OAuth 2.0 device flow with OIDC discovery and token refresh.

```go
oidcConfig, err := auth.DiscoverOIDC("https://api.emergent-company.ai")
provider := auth.NewOAuthProvider(oidcConfig, "emergent-sdk", "~/.emergent/credentials.json")
```

## IsAPIToken

```go
func IsAPIToken(key string) bool
```

Returns `true` if `key` starts with `"emt_"`. Used by `sdk.New` to auto-detect whether to use `APIKeyProvider` or `APITokenProvider` when `Mode: "apikey"` is set.

## Credentials

```go
type Credentials struct {
    AccessToken  string
    RefreshToken string
    ExpiresAt    time.Time
    TokenType    string
}

func (c *Credentials) IsExpired() bool
func LoadCredentials(path string) (*Credentials, error)
func SaveCredentials(creds *Credentials, path string) error
```

`LoadCredentials` reads OAuth credentials from a JSON file (expanding `~` in the path).
`SaveCredentials` writes them back.

## OIDC Discovery

```go
type OIDCConfig struct {
    Issuer                string
    AuthorizationEndpoint string
    TokenEndpoint         string
    DeviceAuthEndpoint    string
}

func DiscoverOIDC(issuerURL string) (*OIDCConfig, error)
```

Fetches and validates the OpenID Connect configuration document from `{issuerURL}/.well-known/openid-configuration`.

## OAuth Device Flow

```go
type DeviceCodeResponse struct {
    DeviceCode              string
    UserCode                string
    VerificationURI         string
    VerificationURIComplete string
    ExpiresIn               int
    Interval                int
}

func (p *OAuthProvider) InitiateDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error)
func (p *OAuthProvider) PollForToken(ctx context.Context, deviceCode string, interval, expiresIn int) error
```

`InitiateDeviceFlow` starts the device authorization flow; `PollForToken` polls the token endpoint until the user completes authorization or the code expires.

## See Also

- [Authentication guide](../authentication.md)
