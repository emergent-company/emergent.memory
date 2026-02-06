# Design: CLI Tool for Server Control (Go Implementation)

## Context

Users need a command-line interface to control the Spec Server remotely for:

- Automation scripts
- CI/CD pipelines
- Server administration
- Debugging and troubleshooting
- Bulk operations (document imports, batch processing)

Current limitations:

- Admin UI requires browser/GUI access
- No programmatic API access from command line
- Manual workflows can't be scripted/automated
- Remote server management requires SSH + direct DB/API access

## Goals

1. **Primary**: Enable remote server control via CLI commands
2. **Authentication**: Solve credential management for CLI context
3. **Scope**: Support key operations (documents, chat, extraction, admin)
4. **UX**: Provide intuitive command structure with helpful error messages
5. **Distribution**: Single binary deployment with no runtime dependencies

## Non-Goals

- Building a complete alternative to the Admin UI
- Supporting every possible API endpoint (focus on high-value operations)
- Replacing existing workspace CLI (process management remains separate)
- Real-time streaming/interactive features (initial version)

## Phase 2: Authentication - OAuth Device Flow with Zitadel

### Decision: OAuth Device Flow (RFC 8628)

**Rationale**:

- Zitadel natively supports RFC 8628 Device Authorization Grant
- Works with ALL Zitadel authentication methods (password, MFA, social login, passkeys)
- Industry standard pattern (GitHub CLI, Azure CLI, Heroku CLI use this)
- No backend changes needed - Zitadel handles everything
- Secure: User authorizes in browser, CLI polls for token

### Architecture Overview

Since Zitadel natively supports device flow, the CLI communicates directly with Zitadel:

```
┌──────────┐                    ┌─────────┐                    ┌─────────┐
│   CLI    │                    │ Zitadel │                    │ Browser │
└────┬─────┘                    └────┬────┘                    └────┬────┘
     │                               │                               │
     │ 1. POST /oauth/v2/device_authorization                        │
     │─────────────────────────────>│                               │
     │                               │                               │
     │ 2. user_code, device_code, verification_uri                  │
     │<─────────────────────────────│                               │
     │                               │                               │
     │ 3. Open browser to verification_uri_complete                 │
     │───────────────────────────────────────────────────────────>│
     │                               │                               │
     │                               │ 4. GET /device?user_code=ABCD │
     │                               │<──────────────────────────────│
     │                               │                               │
     │                               │ 5. User authenticates         │
     │                               │ (MFA/social/password via      │
     │                               │  Zitadel UI - any method)     │
     │                               │<──────────────────────────────│
     │                               │                               │
     │                               │ 6. User authorizes CLI        │
     │                               │<──────────────────────────────│
     │                               │                               │
     │ 7. Poll: POST /oauth/v2/token (every 5s)                     │
     │─────────────────────────────>│                               │
     │                               │                               │
     │ 8. {access_token, refresh_token}                             │
     │<─────────────────────────────│                               │
     │                               │                               │
    ✓ Logged in (tokens saved)       │                               │
```

### What We Build vs What Zitadel Provides

| Component                         | Owner        | What It Does                                               |
| --------------------------------- | ------------ | ---------------------------------------------------------- |
| **CLI Device Flow Client**        | **We Build** | Go code to call Zitadel's device endpoints, poll for token |
| **Device Authorization Endpoint** | **Zitadel**  | Issues device_code + user_code                             |
| **Token Endpoint (polling)**      | **Zitadel**  | Returns tokens when approved                               |
| **Device Activation UI**          | **Zitadel**  | Browser page at `/device` for user authorization           |
| **Credential Storage**            | **We Build** | `~/.emergent/credentials.json` file management             |
| **Auto-Refresh Logic**            | **We Build** | Retry on 401 with refresh token                            |

**Advantage**: Zero backend work needed. Zitadel handles all OAuth complexity.

### CLI Setup in Zitadel

**Registration Steps**:

1. Go to Organization > Projects > [Your Project]
2. Applications > New
3. Select "Native"
4. Name: "Emergent CLI"
5. Grant Types: Check "Device Code"
6. Continue > Create
7. Copy Client ID (e.g., "232685602728952637@emergent_cli")

**Automated Bootstrap** (add to existing Zitadel bootstrap script):

```bash
# scripts/bootstrap-zitadel-cli-client.sh
# Use Zitadel Management API to create native app with device_code grant
# Store client ID in .env for build-time injection
```

### CLI Implementation

**File**: `pkg/auth/config.go`

```go
package auth

const (
    // Hardcoded in binary (public client, no secret)
    ClientID = "emergent-cli"

    // Scopes to request
    Scopes = "openid profile email offline_access"
)

// IssuerURL can be overridden at build time
var IssuerURL = "https://auth.dev.emergent-company.ai"
```

**Build-time Environment Override**:

```bash
# Dev build
go build -ldflags="-X 'github.com/emergent/cli/pkg/auth.IssuerURL=https://dev.emergent-company.ai'"

# Production build
go build -ldflags="-X 'github.com/emergent/cli/pkg/auth.IssuerURL=https://auth.emergent-company.ai'"
```

### Command: `emergent-cli login`

**User Flow**:

```bash
$ emergent-cli login

Requesting authorization from Zitadel...

Please visit: https://auth.dev.emergent-company.ai/device

And enter code: GQWC-FWFK

Opening browser automatically...
⠋ Waiting for authorization... (expires in 14:23)

✓ Successfully logged in as user@example.com
```

**Implementation (pseudo-Go)**:

```go
// cmd/auth/login.go
package auth

import (
    "fmt"
    "github.com/pkg/browser"
    "github.com/spf13/cobra"
)

func RunLogin(cmd *cobra.Command, args []string) error {
    // 1. Discover Zitadel endpoints
    config, err := auth.DiscoverOIDC(auth.IssuerURL)
    if err != nil {
        return fmt.Errorf("failed to discover OIDC endpoints: %w", err)
    }

    // 2. Request device code
    deviceResp, err := auth.RequestDeviceCode(config, auth.ClientID, auth.Scopes)
    if err != nil {
        return fmt.Errorf("failed to request device code: %w", err)
    }

    // 3. Display instructions
    fmt.Printf("\nPlease visit: %s\n", deviceResp.VerificationURI)
    fmt.Printf("And enter code: %s\n\n", deviceResp.UserCode)

    // 4. Open browser
    if err := browser.OpenURL(deviceResp.VerificationURIComplete); err != nil {
        fmt.Printf("Failed to open browser automatically: %v\n", err)
    }

    // 5. Poll for token
    fmt.Println("Waiting for authorization...")
    spinner := NewSpinner()
    spinner.Start()
    defer spinner.Stop()

    token, err := auth.PollForToken(
        config,
        deviceResp.DeviceCode,
        deviceResp.Interval,
        deviceResp.ExpiresIn,
    )
    if err != nil {
        return fmt.Errorf("authorization failed: %w", err)
    }

    // 6. Fetch user info
    userInfo, err := auth.GetUserInfo(config, token.AccessToken)
    if err != nil {
        return fmt.Errorf("failed to fetch user info: %w", err)
    }

    // 7. Save credentials
    creds := &auth.Credentials{
        AccessToken:  token.AccessToken,
        RefreshToken: token.RefreshToken,
        ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
        UserEmail:    userInfo.Email,
        IssuerURL:    auth.IssuerURL,
    }

    if err := auth.SaveCredentials(creds); err != nil {
        return fmt.Errorf("failed to save credentials: %w", err)
    }

    fmt.Printf("\n✓ Successfully logged in as %s\n", userInfo.Email)
    return nil
}
```

### Token Storage

**File**: `~/.emergent/credentials.json` (0600 permissions)

```json
{
  "issuer_url": "https://auth.dev.emergent-company.ai",
  "access_token": "eyJhbGc...",
  "refresh_token": "Rft...xyz",
  "expires_at": "2026-02-05T12:30:00Z",
  "user_email": "user@example.com"
}
```

**Implementation**:

```go
// pkg/auth/credentials.go
package auth

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

type Credentials struct {
    IssuerURL    string    `json:"issuer_url"`
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    UserEmail    string    `json:"user_email"`
}

func SaveCredentials(creds *Credentials) error {
    homeDir, _ := os.UserHomeDir()
    configDir := filepath.Join(homeDir, ".emergent")

    // Create directory with secure permissions
    if err := os.MkdirAll(configDir, 0700); err != nil {
        return err
    }

    credPath := filepath.Join(configDir, "credentials.json")
    data, _ := json.MarshalIndent(creds, "", "  ")

    // Write with restricted permissions (0600 = rw-------)
    return os.WriteFile(credPath, data, 0600)
}

func LoadCredentials() (*Credentials, error) {
    homeDir, _ := os.UserHomeDir()
    credPath := filepath.Join(homeDir, ".emergent", "credentials.json")

    data, err := os.ReadFile(credPath)
    if err != nil {
        return nil, err
    }

    var creds Credentials
    if err := json.Unmarshal(data, &creds); err != nil {
        return nil, err
    }

    return &creds, nil
}
```

### Auto-Refresh on 401

**Implementation**:

```go
// pkg/client/auth_middleware.go
package client

import (
    "fmt"
    "net/http"
    "github.com/emergent/cli/pkg/auth"
)

func (c *Client) Do(req *http.Request) (*http.Response, error) {
    // Add current access token
    creds, err := auth.LoadCredentials()
    if err != nil {
        return nil, fmt.Errorf("not authenticated: %w", err)
    }

    req.Header.Set("Authorization", "Bearer "+creds.AccessToken)

    // Make request
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }

    // If 401, try refresh once
    if resp.StatusCode == http.StatusUnauthorized {
        resp.Body.Close()

        // Refresh token
        if err := c.refreshToken(creds); err != nil {
            return nil, fmt.Errorf("token refresh failed: %w", err)
        }

        // Retry with new token
        creds, _ = auth.LoadCredentials()
        req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
        return c.httpClient.Do(req)
    }

    return resp, nil
}

func (c *Client) refreshToken(creds *auth.Credentials) error {
    config, _ := auth.DiscoverOIDC(creds.IssuerURL)

    // Call Zitadel token endpoint with refresh_token grant
    newToken, err := auth.RefreshAccessToken(config, creds.RefreshToken)
    if err != nil {
        return err
    }

    // Update credentials
    creds.AccessToken = newToken.AccessToken
    creds.ExpiresAt = time.Now().Add(time.Duration(newToken.ExpiresIn) * time.Second)
    // Note: refresh_token may rotate, update if provided
    if newToken.RefreshToken != "" {
        creds.RefreshToken = newToken.RefreshToken
    }

    return auth.SaveCredentials(creds)
}
```

### Other Auth Commands

**`emergent-cli logout`**:

```go
func RunLogout(cmd *cobra.Command, args []string) error {
    homeDir, _ := os.UserHomeDir()
    credPath := filepath.Join(homeDir, ".emergent", "credentials.json")

    if err := os.Remove(credPath); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to delete credentials: %w", err)
    }

    fmt.Println("✓ Logged out successfully")
    return nil
}
```

**`emergent-cli auth status`**:

```go
func RunAuthStatus(cmd *cobra.Command, args []string) error {
    creds, err := auth.LoadCredentials()
    if err != nil {
        fmt.Println("Status: Not logged in")
        return nil
    }

    fmt.Printf("Status: Logged in\n")
    fmt.Printf("User: %s\n", creds.UserEmail)
    fmt.Printf("Expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))

    if time.Now().After(creds.ExpiresAt) {
        fmt.Println("Warning: Access token expired (will auto-refresh on next API call)")
    }

    return nil
}
```

### Dependencies (Go Modules)

```go
// go.mod additions
require (
    github.com/coreos/go-oidc/v3 v3.9.0  // OIDC discovery
    golang.org/x/oauth2 v0.16.0           // OAuth2 client
    github.com/pkg/browser v0.0.0         // Open browser
    github.com/briandowns/spinner v1.23.0 // CLI spinner
)
```

### Security Considerations

**1. No Client Secret**:

- CLI is a **public client** (cannot securely store secrets)
- Zitadel device flow doesn't require client secrets
- Security relies on user authorization step in browser

**2. Token Storage**:

- Credentials file has `0600` permissions (owner read/write only)
- Stored in `~/.emergent/` (standard config location)
- Future: Consider OS keychain integration (macOS Keychain, Windows Credential Manager)

**3. Token Lifetime**:

- Access tokens: Short-lived (default 1 hour)
- Refresh tokens: Long-lived (default 30 days)
- Automatic refresh on API calls prevents disruption

**4. Scope Management**:
**MVP Scopes**:

- `openid` - Required for OIDC
- `profile` - User name
- `email` - User email
- `offline_access` - Refresh token

**Future**: Request only needed scopes per command (principle of least privilege)

### Testing Strategy

**Unit Tests**:

```go
// pkg/auth/device_flow_test.go
func TestRequestDeviceCode(t *testing.T) {
    // Mock Zitadel endpoints
    server := httptest.NewServer(...)
    defer server.Close()

    resp, err := RequestDeviceCode(...)
    assert.NoError(t, err)
    assert.NotEmpty(t, resp.DeviceCode)
    assert.NotEmpty(t, resp.UserCode)
}

func TestPollForToken_Success(t *testing.T) {
    // Mock successful authorization
}

func TestPollForToken_Timeout(t *testing.T) {
    // Mock expiration
}
```

**Integration Tests** (against real Zitadel):

```go
func TestRealDeviceFlow(t *testing.T) {
    t.Skip("Requires manual browser interaction")

    // This would be run manually during development
    // to verify against real Zitadel instance
}
```

**E2E Manual Test**:

```bash
# 1. Build CLI
go build -o emergent-cli

# 2. Run login
./emergent-cli login
# (Complete auth in browser)

# 3. Verify token saved
cat ~/.emergent/credentials.json | jq

# 4. Test API call
./emergent-cli documents list

# 5. Test token refresh
# (Wait for expiration or manually edit credentials.json to set past expiry)
./emergent-cli documents list  # Should auto-refresh

# 6. Logout
./emergent-cli logout
```

### Benefits Over Password Grant

| Aspect          | Password Grant                       | Device Flow                        |
| --------------- | ------------------------------------ | ---------------------------------- |
| MFA Support     | ❌ Cannot handle MFA prompts         | ✅ Full MFA support in browser     |
| Social Login    | ❌ Cannot handle OAuth redirects     | ✅ Works with Google, GitHub, etc. |
| Passkeys        | ❌ CLI cannot interact with hardware | ✅ Full passkey support in browser |
| Security        | ⚠️ Password in CLI memory            | ✅ No credentials in CLI           |
| User Experience | ⚠️ Must enter password in terminal   | ✅ Use familiar browser login      |
| Implementation  | ⚠️ Custom backend endpoints          | ✅ Zitadel handles everything      |

## Phase 1: Config Management

### Purpose

Configuration management provides foundation for all commands by storing:

- Server connection settings
- Authentication credentials
- Default context (org/project)
- Output preferences
- Per-environment profiles (dev, staging, prod)

### File Structure

**Location**: `~/.emergent/` (user home directory)

```
~/.emergent/
├── config.yaml          # Main configuration file (0644 permissions)
├── credentials.json     # OAuth tokens (0600 permissions - owner only)
└── cli-audit.log       # Command audit trail (0600 permissions - owner only)
```

### Config File Format

**File**: `~/.emergent/config.yaml`

```yaml
# Server configuration
server_url: https://api.dev.emergent-company.ai
zitadel_issuer: https://auth.dev.emergent-company.ai
zitadel_client_id: emergent-cli

# Default context (remembered from last use)
default_org: org_123
default_project: proj_456

# Output preferences
output_format: table # table | json | yaml | csv
color: auto # auto | always | never
verbose: false

# Server profiles (optional, for multi-environment)
profiles:
  dev:
    server_url: https://api.dev.emergent-company.ai
    zitadel_issuer: https://auth.dev.emergent-company.ai
  staging:
    server_url: https://api.staging.emergent-company.ai
    zitadel_issuer: https://auth.staging.emergent-company.ai
  prod:
    server_url: https://api.emergent-company.ai
    zitadel_issuer: https://auth.emergent-company.ai
active_profile: dev
```

### Configuration Precedence

**Order** (highest to lowest priority):

1. **Command-line flags**: `--server-url`, `--org`, `--project`
2. **Environment variables**: `EMERGENT_SERVER_URL`, `EMERGENT_ORG`, `EMERGENT_PROJECT`
3. **Active profile** (if set): `config.profiles[active_profile].*`
4. **Top-level config**: `config.server_url`, `config.default_org`
5. **Built-in defaults**: Hardcoded fallbacks

### Environment Variables

```bash
# Connection
EMERGENT_SERVER_URL="https://api.dev.emergent-company.ai"
EMERGENT_ZITADEL_ISSUER="https://auth.dev.emergent-company.ai"

# Context
EMERGENT_ORG="org_123"
EMERGENT_PROJECT="proj_456"

# Output
EMERGENT_OUTPUT_FORMAT="json"
EMERGENT_COLOR="always"
EMERGENT_VERBOSE="true"

# Authentication (for CI/CD, headless environments)
EMERGENT_ACCESS_TOKEN="eyJhbGc..."
EMERGENT_REFRESH_TOKEN="..."
```

### Config Commands

**View Configuration**:

```bash
emergent-cli config show
# Output (masked):
# Server URL: https://api.dev.emergent-company.ai
# Zitadel Issuer: https://auth.dev.emergent-company.ai
# Default Org: org_123
# Default Project: proj_456
# Output Format: table
# Active Profile: dev
# Authenticated: Yes (expires: 2026-02-10T15:30:00Z)

emergent-cli config show --profile staging
# Shows staging profile settings

emergent-cli config show --format json
# Outputs config as JSON (useful for scripts)
```

**Set Configuration Values**:

```bash
# Set server URL
emergent-cli config set server-url https://api.dev.emergent-company.ai

# Set default org/project
emergent-cli config set default-org org_123
emergent-cli config set default-project proj_456

# Set output preferences
emergent-cli config set output-format json
emergent-cli config set color always
emergent-cli config set verbose true

# Profile management
emergent-cli config use-profile prod
emergent-cli config create-profile staging --server-url https://api.staging.example.com
```

**Reset Configuration**:

```bash
# Reset all config to defaults
emergent-cli config reset

# Reset specific value
emergent-cli config unset default-org
```

### Config Validation

**On Startup** (every command):

1. Check if `~/.emergent/` directory exists (create if missing)
2. Check if `config.yaml` exists (create with defaults if missing)
3. Validate YAML syntax (fail fast with helpful error)
4. Validate required fields (server_url, zitadel_issuer)
5. Validate enum values (output_format, color)

**Validation Rules**:

- `server_url`: Must be valid HTTP/HTTPS URL
- `output_format`: Must be one of: table, json, yaml, csv
- `color`: Must be one of: auto, always, never
- `default_org` / `default_project`: Must be valid IDs (alphanumeric + dashes)

### Profile Management

**Use Cases**:

- Developers working across dev/staging/prod environments
- Teams with multiple deployment regions
- Testing against different server versions

**Profile Structure**:

```yaml
profiles:
  dev:
    server_url: https://api.dev.emergent-company.ai
    zitadel_issuer: https://auth.dev.emergent-company.ai
    # Optionally override other settings
    output_format: json
    verbose: true
  prod:
    server_url: https://api.emergent-company.ai
    zitadel_issuer: https://auth.emergent-company.ai
    output_format: table
    verbose: false
active_profile: dev
```

**Profile Commands**:

```bash
# Switch profiles
emergent-cli config use-profile prod

# List all profiles
emergent-cli config list-profiles

# Create new profile
emergent-cli config create-profile staging \
  --server-url https://api.staging.example.com \
  --zitadel-issuer https://auth.staging.example.com

# Delete profile
emergent-cli config delete-profile staging
```

### Default Context Management

**Behavior**:

- Commands remember last-used org/project
- Stored in `default_org` and `default_project` config keys
- Updated automatically when org/project flags are used
- Can be overridden via `--org` / `--project` flags

**Example Flow**:

```bash
# First time - must specify org/project
emergent-cli documents list --org acme --project docs
# Config updated: default_org=acme, default_project=docs

# Next time - uses defaults
emergent-cli documents list
# (Uses acme org, docs project automatically)

# Override defaults temporarily
emergent-cli documents list --org example --project eng
# (Uses example org, eng project for this command only)
# Config still has default_org=acme, default_project=docs
```

### Config File Initialization

**On First Run**:

```go
// pkg/config/config.go

func EnsureConfigExists() error {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        return err
    }

    configDir := filepath.Join(homeDir, ".emergent")
    configPath := filepath.Join(configDir, "config.yaml")

    // Create directory if missing
    if _, err := os.Stat(configDir); os.IsNotExist(err) {
        if err := os.MkdirAll(configDir, 0700); err != nil {
            return err
        }
    }

    // Create default config if missing
    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        defaultConfig := Config{
            ServerURL:      "https://api.dev.emergent-company.ai",
            ZitadelIssuer:  "https://auth.dev.emergent-company.ai",
            ZitadelClientID: "emergent-cli",
            OutputFormat:   "table",
            Color:          "auto",
            Verbose:        false,
        }

        data, err := yaml.Marshal(defaultConfig)
        if err != nil {
            return err
        }

        return os.WriteFile(configPath, data, 0644)
    }

    return nil
}
```

### Implementation Pattern

**Config Loading**:

```go
// Load in priority order
func LoadConfig() (*Config, error) {
    // 1. Load from file
    config, err := loadConfigFile()
    if err != nil {
        return nil, err
    }

    // 2. Apply active profile (if set)
    if config.ActiveProfile != "" {
        applyProfile(config, config.ActiveProfile)
    }

    // 3. Override with environment variables
    applyEnvVars(config)

    // 4. Command-line flags applied by Cobra automatically

    return config, nil
}
```

**Config Access**:

```go
// Global config available to all commands
var GlobalConfig *Config

func init() {
    EnsureConfigExists()
    GlobalConfig, _ = LoadConfig()
}

// Commands access via GlobalConfig
func RunListDocuments(cmd *cobra.Command, args []string) error {
    serverURL := GlobalConfig.ServerURL
    org := GlobalConfig.DefaultOrg
    // ...
}
```

### Benefits

**For Users**:

- No need to specify server URL every time
- No need to remember org/project IDs
- Easy to switch between environments (profiles)
- Preferences persist across sessions
- CI/CD friendly (environment variables)

**For Developers**:

- Clear precedence rules
- Easy to test (override with env vars)
- Profile system for multi-environment workflows
- Audit trail for troubleshooting

**For Operations**:

- Centralized configuration location (`~/.emergent/`)
- Standard YAML format (grep-able, diff-able)
- Version-controllable profiles (teams can share)
- Environment variable overrides (no file edits in CI/CD)

### Testing Strategy

**Unit Tests**:

```go
func TestConfigPrecedence(t *testing.T) {
    // Test: CLI flag > env var > profile > top-level > default
}

func TestProfileSwitching(t *testing.T) {
    // Test: Active profile overrides top-level config
}

func TestEnvVarOverrides(t *testing.T) {
    // Test: Env vars override file config
}
```

**Integration Tests**:

```bash
# Test config creation
emergent-cli config show  # Should create ~/.emergent/config.yaml

# Test profile switching
emergent-cli config use-profile staging
emergent-cli config show  # Should show staging URL

# Test env var override
EMERGENT_SERVER_URL="http://localhost" emergent-cli config show
# Should show localhost, not file config
```

### Dependencies

```go
// go.mod additions
require (
    github.com/spf13/viper v1.18.2  // Config management
    gopkg.in/yaml.v3 v3.0.1          // YAML parsing
)
```

## Phase 3: Documents Commands

### Purpose

Provide document management commands for the user's **upload → extract → query** workflow. Enable users to:

- Upload individual documents to the knowledge base
- List documents with status (pending, indexed, failed)
- Retrieve document details and check processing progress
- Prepare for extraction operations (Phase 5)

### Core Commands

#### `documents upload <file>`

**Purpose**: Upload a single document to the user's knowledge base (project-scoped).

**Usage**:

```bash
# Upload with auto-detected org/project from config
emergent-cli documents upload ./report.pdf

# Upload with explicit org/project
emergent-cli documents upload ./report.pdf --org acme --project eng

# Upload with custom title
emergent-cli documents upload ./spec.docx --title "Product Requirements v2.0"
```

**Behavior**:

1. Validate file exists and is readable
2. Check file size (warn if > 50MB, error if > 100MB based on server limits)
3. Detect MIME type from extension (pdf, txt, docx, html, md, etc.)
4. Construct multipart form-data request with:
   - `file`: Binary file content
   - `title`: Filename (default) or `--title` flag value
   - `source`: "cli" (to distinguish from web uploads)
5. Send to `POST /api/documents` with:
   - `X-Org-ID` header (from config/flag)
   - `X-Project-ID` header (from config/flag)
   - `Authorization: Bearer {token}` (from Phase 2 credentials)
6. Handle response:
   - Success (201): Print document ID and status message
   - Validation error (400): Display specific validation message
   - Unauthorized (401): Trigger token refresh and retry once
   - Too large (413): Error message with size limit
   - Network error: Retry with exponential backoff (3 attempts max)

**Flags**:

- `--title <string>`: Custom document title (default: filename)
- `--org <id>`: Target organization ID (overrides config)
- `--project <id>`: Target project ID (overrides config)
- `--format <json|yaml|table>`: Output format (default: table)

**Success Output** (table format):

```
✓ Document uploaded successfully

ID:        doc_abc123xyz
Title:     report.pdf
Status:    pending
Created:   2025-02-05 10:35:14 UTC

Next steps:
1. Check processing status: emergent-cli documents get doc_abc123xyz
2. Start extraction: emergent-cli extraction start doc_abc123xyz --template-pack <name>
```

**Error Output** (validation):

```
✗ Upload failed

Error:  File type not supported
Code:   invalid-file-type
Detail: Only PDF, TXT, DOCX, HTML, and Markdown files are supported
File:   report.xlsx (application/vnd.openxmlformats-officedocument.spreadsheetml.sheet)
```

**Implementation Notes**:

- Use `mime` package (or similar) for type detection: `mime.TypeByExtension(filepath.Ext(filename))`
- Multipart construction: Go's `mime/multipart.Writer` for form-data
- Progress bar: Optional enhancement (note for future, not MVP requirement)
- Single file only: No bulk upload, no directory support in MVP

---

#### `documents list`

**Purpose**: List documents in the user's project with pagination and filtering.

**Usage**:

```bash
# List first 20 documents (default limit)
emergent-cli documents list

# List with custom limit
emergent-cli documents list --limit 50

# List with pagination
emergent-cli documents list --offset 20 --limit 20

# Filter by status
emergent-cli documents list --status indexed
emergent-cli documents list --status failed

# Filter by upload date
emergent-cli documents list --created-after 2025-02-01

# Combine filters
emergent-cli documents list --status pending --limit 10

# JSON output for scripting
emergent-cli documents list --format json > documents.json
```

**Behavior**:

1. Build query parameters from flags (limit, offset, status, created-after)
2. Send to `GET /api/documents` with:
   - `X-Org-ID` and `X-Project-ID` headers
   - `Authorization: Bearer {token}`
   - Query params: `?limit=20&offset=0&status=indexed`
3. Handle response:
   - Success (200): Format and display table or JSON
   - Unauthorized (401): Trigger token refresh and retry
   - Network error: Retry with backoff
4. Parse pagination metadata from response:
   - Total count
   - Current offset
   - Returned count
5. Display next page instructions if more results exist

**Flags**:

- `--limit <int>`: Maximum results per page (1-100, default: 20)
- `--offset <int>`: Skip N results (for pagination, default: 0)
- `--status <string>`: Filter by processing status (pending, indexed, failed, processing)
- `--created-after <date>`: Filter by creation date (ISO 8601: YYYY-MM-DD)
- `--org <id>`: Target organization ID (overrides config)
- `--project <id>`: Target project ID (overrides config)
- `--format <json|yaml|table|csv>`: Output format (default: table)

**Table Output**:

```
Documents (Showing 1-5 of 127)

ID              Title                Status     Created              Size
──────────────  ───────────────────  ─────────  ───────────────────  ──────
doc_abc123xyz   Product Roadmap      indexed    2025-02-05 10:35:14  2.4 MB
doc_def456uvw   API Documentation    indexed    2025-02-04 14:22:08  1.8 MB
doc_ghi789rst   Design Spec          processing 2025-02-04 09:15:33  3.1 MB
doc_jkl012mno   Meeting Notes        pending    2025-02-03 16:42:19  0.5 MB
doc_pqr345stu   Technical Report     failed     2025-02-02 11:08:47  5.2 MB

Next page: emergent-cli documents list --offset 5 --limit 5
```

**JSON Output** (for scripting):

```json
{
  "documents": [
    {
      "id": "doc_abc123xyz",
      "title": "Product Roadmap",
      "status": "indexed",
      "created_at": "2025-02-05T10:35:14Z",
      "size_bytes": 2516582,
      "mime_type": "application/pdf"
    }
  ],
  "pagination": {
    "total": 127,
    "limit": 5,
    "offset": 0,
    "has_more": true
  }
}
```

**Status Values** (from server API):

- `pending`: Upload complete, not yet processing
- `processing`: Currently being indexed
- `indexed`: Successfully processed and searchable
- `failed`: Processing failed (check error details with `documents get <id>`)

**Implementation Notes**:

- Pagination: Server-side with offset/limit (not cursor-based for MVP)
- Sorting: Fixed by `created_at DESC` (newest first, not configurable in MVP)
- Date parsing: Use Go's `time.Parse("2006-01-02", input)` for `--created-after`
- Table formatter: Use `github.com/olekukonko/tablewriter` or similar
- CSV output: Simple comma-separated, escape fields with quotes if needed

---

#### `documents get <id>`

**Purpose**: Retrieve detailed information about a specific document, including processing status and error messages.

**Usage**:

```bash
# Get document details
emergent-cli documents get doc_abc123xyz

# JSON output
emergent-cli documents get doc_abc123xyz --format json

# Check status in script
STATUS=$(emergent-cli documents get doc_abc123xyz --format json | jq -r '.status')
if [ "$STATUS" = "indexed" ]; then
  echo "Ready for extraction"
fi
```

**Behavior**:

1. Validate document ID format (non-empty string)
2. Send to `GET /api/documents/{id}` with:
   - `X-Org-ID` and `X-Project-ID` headers
   - `Authorization: Bearer {token}`
3. Handle response:
   - Success (200): Display document details
   - Not found (404): Error message with document ID
   - Unauthorized (401): Trigger token refresh and retry
   - Network error: Retry with backoff

**Flags**:

- `--org <id>`: Target organization ID (overrides config)
- `--project <id>`: Target project ID (overrides config)
- `--format <json|yaml|table>`: Output format (default: table)

**Table Output** (successful):

```
Document Details

ID:          doc_abc123xyz
Title:       Product Roadmap Q1 2025
Status:      indexed
Created:     2025-02-05 10:35:14 UTC
Updated:     2025-02-05 10:36:22 UTC
Size:        2.4 MB (2,516,582 bytes)
Type:        application/pdf
Source:      cli
Chunks:      42 text chunks indexed

Processing Info:
  Started:   2025-02-05 10:35:18 UTC
  Completed: 2025-02-05 10:36:22 UTC
  Duration:  1m 4s

Ready for extraction. Use: emergent-cli extraction start doc_abc123xyz --template-pack <name>
```

**Table Output** (failed):

```
Document Details

ID:          doc_pqr345stu
Title:       Corrupted File
Status:      failed
Created:     2025-02-02 11:08:47 UTC
Updated:     2025-02-02 11:09:12 UTC
Size:        5.2 MB (5,452,800 bytes)
Type:        application/pdf

Processing Error:
  Code:    pdf-parse-error
  Message: Unable to extract text from PDF
  Detail:  File appears to be corrupted or encrypted

Action: Re-upload the document or try a different file format.
```

**JSON Output**:

```json
{
  "id": "doc_abc123xyz",
  "title": "Product Roadmap Q1 2025",
  "status": "indexed",
  "created_at": "2025-02-05T10:35:14Z",
  "updated_at": "2025-02-05T10:36:22Z",
  "size_bytes": 2516582,
  "mime_type": "application/pdf",
  "source": "cli",
  "chunk_count": 42,
  "processing": {
    "started_at": "2025-02-05T10:35:18Z",
    "completed_at": "2025-02-05T10:36:22Z",
    "duration_seconds": 64
  }
}
```

**Error Response** (404):

```
✗ Document not found

ID:     doc_invalid123
Org:    acme
Project: eng

Possible causes:
- Document ID is incorrect
- Document was deleted
- Wrong org/project context

List available documents: emergent-cli documents list
```

**Implementation Notes**:

- Duration calculation: `completed_at - started_at` in human-readable format (1m 4s)
- Size formatting: Use `humanize` package for "2.4 MB" display
- Error details: Always display if status is "failed"
- Timestamps: Display in UTC with "UTC" suffix, allow local time with `--local-time` flag (future enhancement)

---

### API Client Pattern

All document commands use a shared HTTP client with standard patterns:

**Client Construction**:

```go
// pkg/api/client.go

type Client struct {
    BaseURL    string
    HTTPClient *http.Client
    Credentials *auth.Credentials  // From Phase 2
}

func NewClient(config *config.Config) *Client {
    return &Client{
        BaseURL: config.ServerURL,
        HTTPClient: &http.Client{Timeout: 30 * time.Second},
        Credentials: auth.LoadCredentials(),
    }
}

// Add required headers to every request
func (c *Client) addHeaders(req *http.Request, orgID, projectID string) {
    // Authentication (from Phase 2 credentials)
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Credentials.AccessToken))

    // Tenant context (from config or flags)
    req.Header.Set("X-Org-ID", orgID)
    req.Header.Set("X-Project-ID", projectID)

    // User agent
    req.Header.Set("User-Agent", "emergent-cli/1.0.0")
}
```

**Request Pattern**:

```go
// Upload example
func (c *Client) UploadDocument(ctx context.Context, file io.Reader, filename, orgID, projectID string) (*Document, error) {
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    // Add file part
    part, err := writer.CreateFormFile("file", filename)
    if err != nil {
        return nil, err
    }
    if _, err := io.Copy(part, file); err != nil {
        return nil, err
    }

    // Add metadata
    writer.WriteField("title", filename)
    writer.WriteField("source", "cli")
    writer.Close()

    // Create request
    req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/documents", body)
    if err != nil {
        return nil, err
    }

    req.Header.Set("Content-Type", writer.FormDataContentType())
    c.addHeaders(req, orgID, projectID)

    // Send request
    resp, err := c.HTTPClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("network error: %w", err)
    }
    defer resp.Body.Close()

    // Handle response
    if resp.StatusCode != http.StatusCreated {
        return nil, handleErrorResponse(resp)
    }

    var doc Document
    if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
        return nil, err
    }

    return &doc, nil
}
```

**Error Handling**:

```go
func handleErrorResponse(resp *http.Response) error {
    var apiErr struct {
        Error struct {
            Code    string `json:"code"`
            Message string `json:"message"`
            Details string `json:"details,omitempty"`
        } `json:"error"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
        return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
    }

    switch resp.StatusCode {
    case http.StatusUnauthorized:
        return &AuthError{Message: apiErr.Error.Message, Code: apiErr.Error.Code}
    case http.StatusBadRequest:
        return &ValidationError{Message: apiErr.Error.Message, Code: apiErr.Error.Code, Details: apiErr.Error.Details}
    case http.StatusNotFound:
        return &NotFoundError{Message: apiErr.Error.Message}
    case http.StatusPayloadTooLarge:
        return &FileTooLargeError{Message: apiErr.Error.Message}
    default:
        return fmt.Errorf("%s (%s)", apiErr.Error.Message, apiErr.Error.Code)
    }
}
```

**Token Refresh on 401**:

```go
// Automatic retry with refreshed token
func (c *Client) doRequestWithRetry(req *http.Request) (*http.Response, error) {
    resp, err := c.HTTPClient.Do(req)
    if err != nil {
        return nil, err
    }

    // If unauthorized, try refreshing token once
    if resp.StatusCode == http.StatusUnauthorized {
        resp.Body.Close()

        // Refresh token (Phase 2 pattern)
        newCreds, err := auth.RefreshToken(c.Credentials.RefreshToken)
        if err != nil {
            return nil, fmt.Errorf("token refresh failed: %w", err)
        }

        // Update credentials
        c.Credentials = newCreds
        auth.SaveCredentials(newCreds)

        // Retry request with new token
        req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", newCreds.AccessToken))
        return c.HTTPClient.Do(req)
    }

    return resp, nil
}
```

---

### File Upload Implementation

**Multipart Form-Data Construction**:

Go's standard library provides `mime/multipart` for constructing multipart requests:

```go
func buildUploadRequest(file *os.File, title, orgID, projectID string) (*http.Request, error) {
    // Create buffer for request body
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    // Add file part
    part, err := writer.CreateFormFile("file", filepath.Base(file.Name()))
    if err != nil {
        return nil, err
    }

    // Copy file content
    if _, err := io.Copy(part, file); err != nil {
        return nil, err
    }

    // Add metadata fields
    writer.WriteField("title", title)
    writer.WriteField("source", "cli")

    // Close multipart writer (adds boundary trailer)
    if err := writer.Close(); err != nil {
        return nil, err
    }

    // Create HTTP request
    req, err := http.NewRequest("POST", "/api/documents", body)
    if err != nil {
        return nil, err
    }

    // Set Content-Type with boundary
    req.Header.Set("Content-Type", writer.FormDataContentType())

    return req, nil
}
```

**File Size Validation**:

Client-side validation before upload to provide early feedback:

```go
func validateFile(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        if os.IsNotExist(err) {
            return fmt.Errorf("file not found: %s", path)
        }
        return fmt.Errorf("cannot access file: %w", err)
    }

    if info.IsDir() {
        return fmt.Errorf("path is a directory, not a file: %s", path)
    }

    // Warn if file is large (> 50MB)
    if info.Size() > 50*1024*1024 {
        fmt.Fprintf(os.Stderr, "⚠ Warning: Large file (%s). Upload may take several minutes.\n", humanize.Bytes(uint64(info.Size())))
    }

    // Error if file exceeds server limit (100MB)
    if info.Size() > 100*1024*1024 {
        return fmt.Errorf("file too large (%s). Maximum size: 100 MB", humanize.Bytes(uint64(info.Size())))
    }

    return nil
}
```

**MIME Type Detection**:

Use file extension to determine MIME type:

```go
import (
    "mime"
    "path/filepath"
)

func detectMIMEType(filename string) string {
    ext := filepath.Ext(filename)
    mimeType := mime.TypeByExtension(ext)

    // Fallback for common document types if mime package doesn't have them
    if mimeType == "" {
        switch ext {
        case ".pdf":
            return "application/pdf"
        case ".docx":
            return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        case ".md":
            return "text/markdown"
        default:
            return "application/octet-stream"
        }
    }

    return mimeType
}
```

**Supported Formats** (validate before upload):

```go
var supportedExtensions = map[string]bool{
    ".pdf":  true,
    ".txt":  true,
    ".docx": true,
    ".html": true,
    ".htm":  true,
    ".md":   true,
    ".markdown": true,
}

func isSupportedFile(filename string) bool {
    ext := strings.ToLower(filepath.Ext(filename))
    return supportedExtensions[ext]
}
```

---

### Pagination Response Handling

**API Response Structure**:

Server returns paginated lists with metadata:

```json
{
  "documents": [
    { "id": "doc_abc", "title": "...", "status": "...", ... }
  ],
  "pagination": {
    "total": 127,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

**Client Parsing**:

```go
type ListDocumentsResponse struct {
    Documents  []Document         `json:"documents"`
    Pagination PaginationMetadata `json:"pagination"`
}

type PaginationMetadata struct {
    Total   int  `json:"total"`
    Limit   int  `json:"limit"`
    Offset  int  `json:"offset"`
    HasMore bool `json:"has_more"`
}
```

**Display Next Page Instructions**:

```go
func displayPaginationFooter(meta PaginationMetadata) {
    if !meta.HasMore {
        return
    }

    nextOffset := meta.Offset + meta.Limit
    fmt.Fprintf(os.Stderr, "\nNext page: emergent-cli documents list --offset %d --limit %d\n", nextOffset, meta.Limit)
}
```

---

### Error Handling Strategy

**Error Types**:

```go
// Custom error types for better handling
type AuthError struct {
    Message string
    Code    string
}

func (e *AuthError) Error() string {
    return fmt.Sprintf("authentication failed: %s (%s)", e.Message, e.Code)
}

type ValidationError struct {
    Message string
    Code    string
    Details string
}

func (e *ValidationError) Error() string {
    if e.Details != "" {
        return fmt.Sprintf("%s: %s\nDetails: %s", e.Code, e.Message, e.Details)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type NotFoundError struct {
    Message string
}

func (e *NotFoundError) Error() string {
    return fmt.Sprintf("not found: %s", e.Message)
}

type FileTooLargeError struct {
    Message string
}

func (e *FileTooLargeError) Error() string {
    return fmt.Sprintf("file too large: %s", e.Message)
}
```

**Retry Logic with Exponential Backoff**:

```go
func (c *Client) doRequestWithBackoff(req *http.Request, maxRetries int) (*http.Response, error) {
    var resp *http.Response
    var err error

    for attempt := 0; attempt <= maxRetries; attempt++ {
        resp, err = c.HTTPClient.Do(req)

        // Success or non-retryable error
        if err == nil && resp.StatusCode < 500 {
            return resp, nil
        }

        // Last attempt
        if attempt == maxRetries {
            break
        }

        // Calculate backoff: 1s, 2s, 4s
        backoff := time.Duration(1<<uint(attempt)) * time.Second
        fmt.Fprintf(os.Stderr, "⚠ Request failed (attempt %d/%d). Retrying in %s...\n", attempt+1, maxRetries, backoff)
        time.Sleep(backoff)

        // Close response body if it exists
        if resp != nil {
            resp.Body.Close()
        }
    }

    if err != nil {
        return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, err)
    }

    return resp, nil
}
```

**User-Friendly Error Messages**:

```go
func formatError(err error) string {
    switch e := err.(type) {
    case *AuthError:
        return fmt.Sprintf("✗ Authentication failed\n\nError: %s\nCode:  %s\n\nAction: Run 'emergent-cli auth login' to re-authenticate", e.Message, e.Code)

    case *ValidationError:
        msg := fmt.Sprintf("✗ Validation error\n\nError: %s\nCode:  %s", e.Message, e.Code)
        if e.Details != "" {
            msg += fmt.Sprintf("\nDetail: %s", e.Details)
        }
        return msg

    case *NotFoundError:
        return fmt.Sprintf("✗ Resource not found\n\n%s", e.Message)

    case *FileTooLargeError:
        return fmt.Sprintf("✗ File too large\n\n%s\n\nMaximum file size: 100 MB", e.Message)

    default:
        return fmt.Sprintf("✗ Error: %s", err.Error())
    }
}
```

---

### Output Formatting

**Table Formatter**:

Use `github.com/olekukonko/tablewriter` or similar:

```go
import "github.com/olekukonko/tablewriter"

func formatDocumentsTable(docs []Document) {
    table := tablewriter.NewWriter(os.Stdout)
    table.SetHeader([]string{"ID", "Title", "Status", "Created", "Size"})

    // Table styling
    table.SetBorder(false)
    table.SetHeaderLine(false)
    table.SetColumnSeparator(" ")
    table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
    table.SetAlignment(tablewriter.ALIGN_LEFT)

    for _, doc := range docs {
        table.Append([]string{
            doc.ID,
            truncate(doc.Title, 30),
            doc.Status,
            doc.CreatedAt.Format("2006-01-02 15:04:05"),
            humanize.Bytes(uint64(doc.SizeBytes)),
        })
    }

    table.Render()
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

**JSON/YAML Output**:

```go
func formatOutput(data interface{}, format string) error {
    switch format {
    case "json":
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(data)

    case "yaml":
        enc := yaml.NewEncoder(os.Stdout)
        return enc.Encode(data)

    case "csv":
        // CSV formatting for documents list
        docs := data.([]Document)
        w := csv.NewWriter(os.Stdout)
        w.Write([]string{"ID", "Title", "Status", "Created", "Size"})
        for _, doc := range docs {
            w.Write([]string{
                doc.ID,
                doc.Title,
                doc.Status,
                doc.CreatedAt.Format(time.RFC3339),
                strconv.Itoa(doc.SizeBytes),
            })
        }
        w.Flush()
        return w.Error()

    default:
        return fmt.Errorf("unsupported format: %s", format)
    }
}
```

---

### Testing Strategy

**Unit Tests**:

```go
func TestUploadDocument(t *testing.T) {
    // Test successful upload
    // Test file validation (too large, unsupported type)
    // Test network errors with retry
    // Test 401 with token refresh
}

func TestListDocuments(t *testing.T) {
    // Test pagination metadata parsing
    // Test filter parameters construction
    // Test empty result handling
}

func TestGetDocument(t *testing.T) {
    // Test success response
    // Test 404 handling
    // Test error details display
}

func TestErrorHandling(t *testing.T) {
    // Test error type detection
    // Test error message formatting
    // Test retry logic with backoff
}
```

**Integration Tests**:

```bash
# Test upload → list → get workflow
emergent-cli documents upload ./test-file.pdf
ID=$(emergent-cli documents list --format json | jq -r '.documents[0].id')
emergent-cli documents get "$ID"

# Test pagination
emergent-cli documents list --limit 5
emergent-cli documents list --offset 5 --limit 5

# Test filters
emergent-cli documents list --status indexed
emergent-cli documents list --created-after 2025-02-01

# Test error handling
emergent-cli documents upload ./nonexistent.pdf  # Should fail with file not found
emergent-cli documents get invalid_id  # Should fail with 404
```

**E2E Test with Mock Server**:

```go
func TestE2EDocumentWorkflow(t *testing.T) {
    // Start mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Mock /api/documents POST, GET, LIST responses
    }))
    defer server.Close()

    // Create client with mock server URL
    client := api.NewClient(&config.Config{ServerURL: server.URL})

    // Test upload
    file, _ := os.Open("testdata/sample.pdf")
    defer file.Close()
    doc, err := client.UploadDocument(context.Background(), file, "sample.pdf", "org1", "proj1")
    assert.NoError(t, err)
    assert.Equal(t, "indexed", doc.Status)

    // Test list
    docs, meta, err := client.ListDocuments(context.Background(), "org1", "proj1", ListOptions{Limit: 20})
    assert.NoError(t, err)
    assert.Equal(t, 1, len(docs))

    // Test get
    retrieved, err := client.GetDocument(context.Background(), doc.ID, "org1", "proj1")
    assert.NoError(t, err)
    assert.Equal(t, doc.ID, retrieved.ID)
}
```

---

### Dependencies

```go
// go.mod additions for Phase 3

require (
    github.com/olekukonko/tablewriter v0.0.5  // Table formatting
    github.com/dustin/go-humanize v1.0.1      // Human-readable sizes/dates
    gopkg.in/yaml.v3 v3.0.1                    // YAML output
)
```

---

### Integration with Phase 2 (Authentication)

Documents commands depend on Phase 2 authentication:

```go
// cmd/documents/root.go

var documentsCmd = &cobra.Command{
    Use:   "documents",
    Short: "Manage documents in your knowledge base",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Check if user is authenticated (Phase 2)
        creds := auth.LoadCredentials()
        if creds == nil || creds.AccessToken == "" {
            return fmt.Errorf("not authenticated. Run 'emergent-cli auth login' first")
        }

        // Check token expiry
        if time.Now().After(creds.ExpiresAt) {
            fmt.Fprintln(os.Stderr, "⚠ Token expired. Refreshing...")
            newCreds, err := auth.RefreshToken(creds.RefreshToken)
            if err != nil {
                return fmt.Errorf("token refresh failed: %w. Run 'emergent-cli auth login' to re-authenticate", err)
            }
            auth.SaveCredentials(newCreds)
        }

        return nil
    },
}
```

---

### Integration with Phase 1 (Config)

Documents commands read org/project context from config:

```go
// cmd/documents/upload.go

func runUploadCommand(cmd *cobra.Command, args []string) error {
    // Get org/project from flags or config
    orgID := cmd.Flag("org").Value.String()
    projectID := cmd.Flag("project").Value.String()

    if orgID == "" {
        orgID = config.GlobalConfig.DefaultOrg
        if orgID == "" {
            return fmt.Errorf("--org flag or default_org config required")
        }
    }

    if projectID == "" {
        projectID = config.GlobalConfig.DefaultProject
        if projectID == "" {
            return fmt.Errorf("--project flag or default_project config required")
        }
    }

    // Update config defaults if flags were used
    if cmd.Flag("org").Changed {
        config.GlobalConfig.DefaultOrg = orgID
        config.SaveConfig(config.GlobalConfig)
    }
    if cmd.Flag("project").Changed {
        config.GlobalConfig.DefaultProject = projectID
        config.SaveConfig(config.GlobalConfig)
    }

    // ... proceed with upload
}
```

---

## Phase 5: Extraction Commands

### Purpose

Provide extraction job management commands to complete the user's **upload → extract → query** workflow. Enable users to:

- Start extraction jobs on uploaded documents with specific template packs
- Monitor job progress with polling and status updates
- Retrieve extraction results (entities, relationships, properties)
- Handle extraction errors and timeouts gracefully

This phase connects **Phase 3 (Documents)** to **Phase 4 (Chat)**:

1. User uploads document → gets document ID (Phase 3)
2. User runs extraction on document → gets job ID (Phase 5)
3. User queries extracted knowledge → leverages results (Phase 4)

---

### Core Commands

#### `extraction start <doc-id> --template-pack <name>`

**Purpose**: Initiate extraction job on a specific document using a template pack.

**Usage**:

```bash
# Start extraction with template pack
emergent-cli extraction start doc_abc123 --template-pack customer-discovery-v1

# With explicit org/project
emergent-cli extraction start doc_abc123 --template-pack product-specs --org acme --project eng

# Shorthand
emergent-cli extraction start doc_abc123 -t customer-discovery-v1
```

**Behavior**:

1. Validate document exists (call `GET /api/documents/{id}`)
2. Validate template pack exists (call `GET /api/template-packs?name={name}`)
3. POST to `/api/extraction/jobs` with `{ document_id, template_pack_name }`
4. Returns job ID immediately (job runs asynchronously on server)
5. Suggests next command: `extraction status <job-id>` to monitor progress

**API Endpoint**: `POST /api/extraction/jobs`

**Request**:

```json
{
  "document_id": "doc_abc123",
  "template_pack_name": "customer-discovery-v1"
}
```

**Response**:

```json
{
  "job_id": "job_xyz789",
  "document_id": "doc_abc123",
  "template_pack": "customer-discovery-v1",
  "status": "pending",
  "created_at": "2025-02-05T10:45:00Z"
}
```

**Output**:

```
✓ Extraction job started successfully

Job ID:        job_xyz789
Document ID:   doc_abc123
Template Pack: customer-discovery-v1
Status:        pending
Created At:    2025-02-05 10:45:00 UTC

Monitor progress: emergent-cli extraction status job_xyz789
```

---

#### `extraction status <job-id>`

**Purpose**: Poll extraction job status and show progress updates. Blocks until completion or timeout.

**Usage**:

```bash
# Poll status (blocks until complete)
emergent-cli extraction status job_xyz789

# With explicit org/project
emergent-cli extraction status job_xyz789 --org acme --project eng

# With custom timeout (default 10 minutes)
emergent-cli extraction status job_xyz789 --timeout 15m
```

**Behavior**:

1. Initial request: `GET /api/extraction/jobs/{id}`
2. If status is `pending` or `processing`:
   - Poll every 5 seconds (first minute)
   - Poll every 10 seconds (after 1 minute)
   - Show elapsed time: `⠋ Processing... (1m 23s elapsed)`
3. If status is `completed` or `failed`:
   - Show final status and duration
   - For `completed`: suggest `extraction get` to view results
   - For `failed`: show error message
4. Timeout after 10 minutes (configurable via `--timeout` flag)

**API Endpoint**: `GET /api/extraction/jobs/{id}`

**Response** (processing):

```json
{
  "job_id": "job_xyz789",
  "document_id": "doc_abc123",
  "template_pack": "customer-discovery-v1",
  "status": "processing",
  "progress": {
    "total_items": 42,
    "processed_items": 15,
    "percentage": 35
  },
  "created_at": "2025-02-05T10:45:00Z",
  "started_at": "2025-02-05T10:45:03Z"
}
```

**Response** (completed):

```json
{
  "job_id": "job_xyz789",
  "document_id": "doc_abc123",
  "template_pack": "customer-discovery-v1",
  "status": "completed",
  "progress": {
    "total_items": 42,
    "processed_items": 42,
    "percentage": 100
  },
  "results": {
    "entities": 42,
    "relationships": 67,
    "properties": 128
  },
  "created_at": "2025-02-05T10:45:00Z",
  "started_at": "2025-02-05T10:45:03Z",
  "completed_at": "2025-02-05T10:47:32Z",
  "duration_seconds": 149
}
```

**Output** (processing):

```
⠋ Processing... (1m 23s elapsed)

Job ID:     job_xyz789
Status:     processing
Progress:   15 / 42 items (35%)
Started:    2025-02-05 10:45:03 UTC
Elapsed:    1m 23s
```

**Output** (completed):

```
✓ Extraction completed successfully!

Job ID:        job_xyz789
Document ID:   doc_abc123
Template Pack: customer-discovery-v1
Duration:      2m 32s

Results:
  Entities:      42
  Relationships: 67
  Properties:    128

View results: emergent-cli extraction get job_xyz789
```

**Output** (failed):

```
✗ Extraction failed

Job ID:     job_xyz789
Status:     failed
Error:      Template pack validation failed: missing required field 'entity_types'
Started:    2025-02-05 10:45:03 UTC
Failed At:  2025-02-05 10:45:12 UTC
Duration:   9s
```

**Output** (timeout):

```
⚠ Extraction timeout (10m exceeded)

Job ID:     job_xyz789
Status:     processing (still running on server)
Progress:   38 / 42 items (90%)
Elapsed:    10m 5s

Note: Job continues running on server. Check status later:
  emergent-cli extraction status job_xyz789
```

---

#### `extraction get <job-id>`

**Purpose**: Retrieve detailed extraction results after completion.

**Usage**:

```bash
# Get results (default table format)
emergent-cli extraction get job_xyz789

# JSON output for scripting
emergent-cli extraction get job_xyz789 --format json

# YAML output
emergent-cli extraction get job_xyz789 --format yaml

# CSV output (entities only)
emergent-cli extraction get job_xyz789 --format csv > entities.csv
```

**Behavior**:

1. Call `GET /api/extraction/jobs/{id}/results`
2. If job not completed, show error: "Job must complete before results are available"
3. If completed, display:
   - Job metadata (ID, document, template pack, duration)
   - Entity count by type
   - Relationship count by type
   - Top entities (preview)

**API Endpoint**: `GET /api/extraction/jobs/{id}/results`

**Response**:

```json
{
  "job_id": "job_xyz789",
  "document_id": "doc_abc123",
  "template_pack": "customer-discovery-v1",
  "status": "completed",
  "results": {
    "entities": [
      {
        "id": "ent_001",
        "type": "Person",
        "name": "John Doe",
        "properties": {
          "role": "CEO",
          "company": "Acme Corp"
        }
      },
      {
        "id": "ent_002",
        "type": "Company",
        "name": "Acme Corp",
        "properties": {
          "industry": "Technology",
          "size": "500-1000"
        }
      }
      // ... 40 more entities
    ],
    "relationships": [
      {
        "id": "rel_001",
        "type": "works_at",
        "source_id": "ent_001",
        "target_id": "ent_002",
        "properties": {
          "since": "2020"
        }
      }
      // ... 66 more relationships
    ]
  },
  "duration_seconds": 149
}
```

**Output** (table format):

```
Extraction Results: job_xyz789
─────────────────────────────────────────────────────────────────
Document:      doc_abc123
Template Pack: customer-discovery-v1
Status:        completed
Duration:      2m 32s

Entity Summary
─────────────────────────────────────────────────────────────────
TYPE            COUNT
Person          15
Company         8
Product         12
Location        7

Relationship Summary
─────────────────────────────────────────────────────────────────
TYPE            COUNT
works_at        15
located_in      12
manufactures    10
partners_with   30

Top Entities (preview)
─────────────────────────────────────────────────────────────────
ID          TYPE        NAME
ent_001     Person      John Doe
ent_002     Company     Acme Corp
ent_003     Product     Widget Pro
ent_004     Location    San Francisco
ent_005     Person      Jane Smith
... (37 more)

Full results: emergent-cli extraction get job_xyz789 --format json > results.json
```

**Output** (JSON format):

```json
{
  "job_id": "job_xyz789",
  "document_id": "doc_abc123",
  "template_pack": "customer-discovery-v1",
  "status": "completed",
  "results": {
    "entities": [...],
    "relationships": [...]
  },
  "duration_seconds": 149
}
```

---

### Polling Pattern Implementation

**Key Requirements**:

- Poll every 5 seconds for first minute (fast feedback)
- Poll every 10 seconds after 1 minute (reduce load)
- Show progress indicator (spinner + elapsed time)
- Support graceful cancellation (Ctrl+C)
- Timeout after 10 minutes (configurable)
- Preserve job on server (continues running after timeout)

**Implementation**:

```go
// cmd/extraction/status.go

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func pollJobStatus(ctx context.Context, jobID string, timeout time.Duration) (*ExtractionJob, error) {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // Handle Ctrl+C gracefully
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    defer signal.Stop(sigChan)

    startTime := time.Now()
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            if ctx.Err() == context.DeadlineExceeded {
                return nil, fmt.Errorf("extraction timeout after %v", timeout)
            }
            return nil, ctx.Err()

        case <-sigChan:
            fmt.Fprintln(os.Stderr, "\n⚠ Polling cancelled (job continues on server)")
            return nil, fmt.Errorf("polling cancelled by user")

        case <-ticker.C:
            job, err := client.GetExtractionJob(ctx, jobID, orgID, projectID)
            if err != nil {
                return nil, err
            }

            elapsed := time.Since(startTime)

            // Show progress
            if job.Status == "processing" {
                if job.Progress != nil && job.Progress.Percentage > 0 {
                    fmt.Printf("\r⠋ Processing... %d%% (%s elapsed)", job.Progress.Percentage, formatDuration(elapsed))
                } else {
                    fmt.Printf("\r⠋ Processing... (%s elapsed)", formatDuration(elapsed))
                }
            }

            // Job completed or failed
            if job.Status == "completed" || job.Status == "failed" {
                fmt.Println() // Clear progress line
                return job, nil
            }

            // Adjust polling interval after 1 minute
            if elapsed > 1*time.Minute && ticker.C == (5 * time.Second) {
                ticker.Reset(10 * time.Second)
            }
        }
    }
}

func formatDuration(d time.Duration) string {
    minutes := int(d.Minutes())
    seconds := int(d.Seconds()) % 60
    return fmt.Sprintf("%dm %ds", minutes, seconds)
}
```

---

### Error Handling

**Common Error Scenarios**:

| Error                    | HTTP Status | User Message                                       | Suggested Action                                         |
| ------------------------ | ----------- | -------------------------------------------------- | -------------------------------------------------------- |
| Document not found       | 404         | "Document doc_abc123 not found"                    | "Check document ID with: emergent-cli documents list"    |
| Template pack not found  | 400         | "Template pack 'customer-discovery-v1' not found"  | "List available packs: emergent-cli template-packs list" |
| Job not found            | 404         | "Extraction job job_xyz789 not found"              | "Check job ID from extraction start output"              |
| Job already running      | 409         | "Extraction already in progress for this document" | "Wait for current job to complete or cancel it"          |
| Insufficient permissions | 403         | "Insufficient permissions to run extraction"       | "Contact org admin to grant extraction:write scope"      |
| Server error             | 500         | "Server error during extraction"                   | "Check logs or contact support"                          |
| Timeout                  | -           | "Extraction timeout (10m exceeded)"                | "Job continues on server. Check status later."           |
| Network error            | -           | "Network error: connection refused"                | "Retry with: emergent-cli extraction status <job-id>"    |

**Retry Logic**:

- Network errors: Retry 3 times with exponential backoff (1s, 2s, 4s)
- 401 Unauthorized: Attempt token refresh, then retry once
- 5xx Server errors: Retry once after 5 seconds
- 4xx Client errors: No retry (user action required)

---

### Integration with Phase 3 (Documents)

**Workflow Continuity**:

```bash
# Step 1: Upload document (Phase 3)
emergent-cli documents upload ./customer-interview.pdf
✓ Document uploaded successfully

Document ID:   doc_abc123
Title:         customer-interview.pdf
Status:        indexed
Size:          2.4 MB
Created At:    2025-02-05 10:45:00 UTC

Ready for extraction: emergent-cli extraction start doc_abc123 --template-pack customer-discovery-v1

# Step 2: Start extraction (Phase 5)
emergent-cli extraction start doc_abc123 --template-pack customer-discovery-v1
✓ Extraction job started successfully

Job ID:        job_xyz789
Document ID:   doc_abc123
Template Pack: customer-discovery-v1
Status:        pending

Monitor progress: emergent-cli extraction status job_xyz789

# Step 3: Monitor status (Phase 5)
emergent-cli extraction status job_xyz789
⠋ Processing... 35% (1m 23s elapsed)
...
✓ Extraction completed successfully!

View results: emergent-cli extraction get job_xyz789

# Step 4: Get results (Phase 5)
emergent-cli extraction get job_xyz789 --format json > results.json
```

**Document Status Integration**:

- `documents get <id>` shows extraction status:

  ```
  Document ID:   doc_abc123
  Title:         customer-interview.pdf
  Status:        indexed
  Chunks:        42

  Extractions:
    Latest Job:  job_xyz789 (completed)
    Entities:    42
    Last Run:    2025-02-05 10:47:32 UTC
  ```

---

### Integration with Phase 7 (Template Packs)

**Template Pack Validation**:

```bash
# List available template packs
emergent-cli template-packs list
NAME                       VERSION  DESCRIPTION
customer-discovery-v1      1.0.0    Extract customer insights from interviews
product-specs-v2           2.1.0    Extract product requirements and features
contract-analysis-v1       1.0.0    Extract contract terms and obligations

# View template pack details before using
emergent-cli template-packs get customer-discovery-v1
Name:         customer-discovery-v1
Version:      1.0.0
Description:  Extract customer insights from discovery calls

Entity Types:
  - Person (name, role, company)
  - Company (name, industry, size)
  - Product (name, category, features)
  - Pain Point (description, severity)
  - Goal (description, priority)

Relationship Types:
  - works_at (Person → Company)
  - uses_product (Company → Product)
  - has_pain_point (Company → Pain Point)
  - wants_to_achieve (Company → Goal)

Use with: emergent-cli extraction start <doc-id> --template-pack customer-discovery-v1
```

**Template Pack Auto-Complete**:

- Shell completion suggests available template pack names
- Use `$(emergent-cli template-packs list --format json | jq -r '.[] .name')` for bash completion

---

### Integration with Phase 4 (Chat)

**Extracted Knowledge Querying**:

After extraction completes, extracted entities/relationships are available in chat:

```bash
# Query extracted knowledge (Phase 4)
emergent-cli chat send "Who are the key stakeholders at Acme Corp?"
Response: Based on the customer interview extraction, the key stakeholders are:
- John Doe (CEO)
- Jane Smith (VP of Product)
- Bob Johnson (CTO)

# Query relationships
emergent-cli chat send "What products does Acme Corp use?"
Response: Acme Corp uses the following products:
- Widget Pro (manufacturing automation)
- Cloud Platform X (infrastructure)
- Analytics Suite Y (business intelligence)
```

---

### Testing Strategy

**Unit Tests**:

```go
func TestStartExtraction(t *testing.T) {
    // Test successful job creation
    // Test document not found (404)
    // Test template pack not found (400)
    // Test network errors with retry
    // Test 401 with token refresh
}

func TestPollStatus(t *testing.T) {
    // Test polling interval (5s → 10s after 1min)
    // Test completion detection
    // Test timeout handling
    // Test Ctrl+C cancellation
    // Test progress percentage display
}

func TestGetResults(t *testing.T) {
    // Test completed job results
    // Test job not completed error
    // Test JSON/YAML/CSV output formats
    // Test entity/relationship parsing
}

func TestErrorHandling(t *testing.T) {
    // Test 404 handling
    // Test 409 (already running)
    // Test 403 (insufficient permissions)
    // Test 500 (server error)
    // Test timeout after 10 minutes
}
```

**Integration Tests**:

```bash
# Test full extraction workflow
DOC_ID=$(emergent-cli documents upload ./test-file.pdf --format json | jq -r '.id')
JOB_ID=$(emergent-cli extraction start "$DOC_ID" --template-pack test-pack --format json | jq -r '.job_id')
emergent-cli extraction status "$JOB_ID"  # Should complete or timeout
emergent-cli extraction get "$JOB_ID" --format json > results.json

# Test polling with mock server (fast-forward time)
# Test timeout handling
# Test Ctrl+C cancellation
# Test error scenarios (404, 400, 500)
```

**E2E Test with Mock Server**:

```go
func TestE2EExtractionWorkflow(t *testing.T) {
    // Start mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/extraction/jobs":
            // Return job created response
            json.NewEncoder(w).Encode(map[string]interface{}{
                "job_id": "job_test123",
                "status": "pending",
            })
        case "/api/extraction/jobs/job_test123":
            // Return processing → completed sequence
            // (simulate status changes over time)
        case "/api/extraction/jobs/job_test123/results":
            // Return extraction results
        }
    }))
    defer server.Close()

    // Test start
    job, err := client.StartExtraction(ctx, "doc_test", "test-pack", "org1", "proj1")
    assert.NoError(t, err)
    assert.Equal(t, "job_test123", job.JobID)

    // Test status polling
    finalJob, err := pollJobStatus(ctx, job.JobID, 1*time.Minute)
    assert.NoError(t, err)
    assert.Equal(t, "completed", finalJob.Status)

    // Test results retrieval
    results, err := client.GetExtractionResults(ctx, job.JobID, "org1", "proj1")
    assert.NoError(t, err)
    assert.Greater(t, len(results.Entities), 0)
}
```

---

### Dependencies

```go
// go.mod additions for Phase 5

require (
    github.com/briandowns/spinner v1.23.0  // Terminal spinner (⠋ animation)
    github.com/fatih/color v1.16.0          // Colored output (✓, ✗, ⚠ symbols)
)
```

---

### Integration with Phase 2 (Authentication)

Extraction commands depend on Phase 2 authentication (same pattern as Phase 3):

```go
// cmd/extraction/root.go

var extractionCmd = &cobra.Command{
    Use:   "extraction",
    Short: "Manage extraction jobs",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Check authentication (Phase 2)
        creds := auth.LoadCredentials()
        if creds == nil || creds.AccessToken == "" {
            return fmt.Errorf("not authenticated. Run 'emergent-cli auth login' first")
        }

        // Check token expiry and refresh if needed
        if time.Now().After(creds.ExpiresAt) {
            fmt.Fprintln(os.Stderr, "⚠ Token expired. Refreshing...")
            newCreds, err := auth.RefreshToken(creds.RefreshToken)
            if err != nil {
                return fmt.Errorf("token refresh failed: %w", err)
            }
            auth.SaveCredentials(newCreds)
        }

        return nil
    },
}
```

---

### Integration with Phase 1 (Config)

Extraction commands read org/project context from config (same pattern as Phase 3):

```go
// cmd/extraction/start.go

func runStartCommand(cmd *cobra.Command, args []string) error {
    // Get org/project from flags or config
    orgID := cmd.Flag("org").Value.String()
    projectID := cmd.Flag("project").Value.String()

    if orgID == "" {
        orgID = config.GlobalConfig.DefaultOrg
        if orgID == "" {
            return fmt.Errorf("--org flag or default_org config required")
        }
    }

    if projectID == "" {
        projectID = config.GlobalConfig.DefaultProject
        if projectID == "" {
            return fmt.Errorf("--project flag or default_project config required")
        }
    }

    // Update config defaults if flags were used
    if cmd.Flag("org").Changed {
        config.GlobalConfig.DefaultOrg = orgID
        config.SaveConfig(config.GlobalConfig)
    }
    if cmd.Flag("project").Changed {
        config.GlobalConfig.DefaultProject = projectID
        config.SaveConfig(config.GlobalConfig)
    }

    // ... proceed with extraction
}
```

---

## Phase 7: Template Packs Commands

**Purpose**: Enable users to discover and inspect template packs before using them in extractions. Template packs define entity types, relationships, and properties that guide LLM-based knowledge extraction from documents.

**Core Philosophy**:

- **Simplified MVP**: No local caching, no install/uninstall, no version pinning
- **Server-authoritative**: Backend manages template packs; CLI only views/queries them
- **Always latest**: CLI always uses the latest version available on server
- **Discovery-first**: Users need to see what packs exist and what they contain before extraction

---

### Commands Overview

```bash
# List all available template packs
emergent-cli template-packs list [--format json|yaml|csv]

# View detailed template pack schema (entity types, relationships)
emergent-cli template-packs info <name>
```

**Note**: No `install`, `uninstall`, or `validate` commands in MVP. These may be added in future phases if needed.

---

### Command 1: `template-packs list`

**Purpose**: List all available template packs from the server

**Usage**:

```bash
# Table format (default)
emergent-cli template-packs list

# JSON format (for scripting)
emergent-cli template-packs list --format json

# YAML format (for config generation)
emergent-cli template-packs list --format yaml

# CSV format (for spreadsheet import)
emergent-cli template-packs list --format csv
```

**API Call**: `GET /api/template-packs`

**Output (Table)**:

```
NAME                       VERSION  DESCRIPTION
customer-discovery-v1      1.0.0    Extract customer insights from interviews
product-specs-v2           2.1.0    Extract product requirements and features
contract-analysis-v1       1.0.0    Extract contract terms and obligations
```

**Output (JSON)**:

```json
{
  "template_packs": [
    {
      "name": "customer-discovery-v1",
      "version": "1.0.0",
      "description": "Extract customer insights from discovery calls and interviews",
      "entity_types_count": 5,
      "relationship_types_count": 8,
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "name": "product-specs-v2",
      "version": "2.1.0",
      "description": "Extract product requirements and features",
      "entity_types_count": 7,
      "relationship_types_count": 12,
      "created_at": "2025-01-20T14:00:00Z"
    }
  ]
}
```

**Error Handling**:

```bash
# Network error
Error: Failed to fetch template packs
Details: Could not connect to https://api.emergent-company.ai/api/template-packs
Suggestion: Check your internet connection and server_url in ~/.emergent/config.yaml

# Authentication error
Error: Unauthorized
Details: Your authentication token may have expired
Suggestion: Run 'emergent-cli config set-credentials' to re-authenticate

# Empty response
No template packs available on this server.
```

**Implementation Notes**:

- Always fetch from server (no local cache)
- Use authentication token from credentials file
- Support all 4 output formats (table, JSON, YAML, CSV)
- Table format: columns are NAME, VERSION, DESCRIPTION (truncate description at 60 chars)
- Sort by `name` alphabetically (case-insensitive)

---

### Command 2: `template-packs info <name>`

**Purpose**: View detailed schema of a template pack (entity types with properties, relationship types)

**Usage**:

```bash
# View detailed schema
emergent-cli template-packs info customer-discovery-v1

# JSON output for programmatic use
emergent-cli template-packs info customer-discovery-v1 --format json
```

**API Call**: `GET /api/template-packs/{name}`

**Output (Human-Readable)**:

```
Template Pack: customer-discovery-v1
Version:       1.0.0
Description:   Extract customer insights from discovery calls and interviews
Created:       2025-01-15 10:30:00 UTC

Entity Types (5):
  Person
    - name (string, required) - Full name of the person
    - role (string) - Job title or role
    - company (string) - Company affiliation
    - email (string) - Contact email

  Company
    - name (string, required) - Company name
    - industry (string) - Industry sector
    - size (string) - Company size (e.g., "50-200 employees")
    - location (string) - Primary location

  Product
    - name (string, required) - Product name
    - category (string) - Product category
    - features (array) - List of key features
    - pricing (string) - Pricing model

  Pain Point
    - description (string, required) - Description of the problem
    - severity (string) - Impact level (low/medium/high)
    - frequency (string) - How often it occurs

  Goal
    - description (string, required) - Desired outcome
    - timeline (string) - Expected timeframe
    - success_criteria (string) - How success is measured

Relationship Types (8):
  works_at (Person → Company)
    - Description: Person is employed by or affiliated with Company

  uses_product (Company → Product)
    - Description: Company is a user/customer of Product

  has_pain_point (Company → Pain Point)
    - Description: Company experiences this problem

  wants_to_achieve (Company → Goal)
    - Description: Company has this objective

  addresses_pain_point (Product → Pain Point)
    - Description: Product solves or mitigates this problem

  supports_goal (Product → Goal)
    - Description: Product helps achieve this objective

  mentions (Person → Product)
    - Description: Person mentioned or discussed Product

  identifies_need (Person → Pain Point)
    - Description: Person identified or described this need

Use with: emergent-cli extraction start <doc-id> --template-pack customer-discovery-v1
```

**Output (JSON)**:

```json
{
  "name": "customer-discovery-v1",
  "version": "1.0.0",
  "description": "Extract customer insights from discovery calls and interviews",
  "created_at": "2025-01-15T10:30:00Z",
  "entity_types": [
    {
      "name": "Person",
      "description": "Individual person mentioned in the document",
      "properties": [
        {
          "name": "name",
          "type": "string",
          "required": true,
          "description": "Full name of the person"
        },
        {
          "name": "role",
          "type": "string",
          "required": false,
          "description": "Job title or role"
        },
        {
          "name": "company",
          "type": "string",
          "required": false,
          "description": "Company affiliation"
        },
        {
          "name": "email",
          "type": "string",
          "required": false,
          "description": "Contact email"
        }
      ]
    },
    {
      "name": "Company",
      "description": "Organization or business entity",
      "properties": [
        {
          "name": "name",
          "type": "string",
          "required": true,
          "description": "Company name"
        },
        {
          "name": "industry",
          "type": "string",
          "required": false,
          "description": "Industry sector"
        },
        {
          "name": "size",
          "type": "string",
          "required": false,
          "description": "Company size (e.g., \"50-200 employees\")"
        },
        {
          "name": "location",
          "type": "string",
          "required": false,
          "description": "Primary location"
        }
      ]
    }
  ],
  "relationship_types": [
    {
      "name": "works_at",
      "source_type": "Person",
      "target_type": "Company",
      "description": "Person is employed by or affiliated with Company"
    },
    {
      "name": "uses_product",
      "source_type": "Company",
      "target_type": "Product",
      "description": "Company is a user/customer of Product"
    }
  ]
}
```

**Error Handling**:

```bash
# Template pack not found
Error: Template pack 'invalid-pack' not found
Available packs: customer-discovery-v1, product-specs-v2, contract-analysis-v1
Suggestion: Run 'emergent-cli template-packs list' to see all available packs

# Network error
Error: Failed to fetch template pack details
Details: Could not connect to server
Suggestion: Check your internet connection and server configuration

# Malformed response
Error: Invalid template pack data received from server
Details: Response missing required fields (entity_types or relationship_types)
Suggestion: Contact your system administrator
```

**Implementation Notes**:

- Fetch full schema from server (no caching)
- Human-readable format:
  - Clear section headers ("Entity Types", "Relationship Types")
  - Indent properties under entity types
  - Show required/optional status clearly
  - Include property descriptions when available
  - Show relationship direction with arrow (→)
- JSON format: Return raw API response for scripting
- If entity/relationship has no description, omit that field
- Include usage hint at the end of human-readable output

---

### Workflow Example

**Scenario**: User wants to extract customer insights from an interview transcript

**Step 1: Discover available packs**

```bash
$ emergent-cli template-packs list

NAME                       VERSION  DESCRIPTION
customer-discovery-v1      1.0.0    Extract customer insights from interviews
product-specs-v2           2.1.0    Extract product requirements and features
contract-analysis-v1       1.0.0    Extract contract terms and obligations
```

**Step 2: View pack details**

```bash
$ emergent-cli template-packs info customer-discovery-v1

Template Pack: customer-discovery-v1
Version:       1.0.0
Description:   Extract customer insights from discovery calls and interviews

Entity Types (5):
  Person
    - name (string, required) - Full name of the person
    - role (string) - Job title or role
    ...

  Company
    - name (string, required) - Company name
    ...

Relationship Types (8):
  works_at (Person → Company)
    - Description: Person is employed by or affiliated with Company
  ...

Use with: emergent-cli extraction start <doc-id> --template-pack customer-discovery-v1
```

**Step 3: Use pack in extraction** (Phase 5 command)

```bash
$ emergent-cli extraction start doc_abc123 --template-pack customer-discovery-v1

Started extraction job: job_def456
Use 'emergent-cli extraction status job_def456' to monitor progress
```

---

### Integration with Phase 5 (Extraction)

Phase 5 `extraction start` command validates template pack before starting job:

```go
// cmd/extraction/start.go

func validateTemplatePack(packName string) error {
    // Call template-packs info endpoint
    resp, err := api.GetTemplatePack(packName)
    if err != nil {
        return fmt.Errorf("template pack '%s' not found. Run 'emergent-cli template-packs list' to see available packs", packName)
    }

    // Validate schema has required fields
    if len(resp.EntityTypes) == 0 {
        return fmt.Errorf("template pack '%s' has no entity types defined", packName)
    }

    return nil
}
```

**Validation Rules**:

- Template pack must exist on server (404 → error)
- Must have at least 1 entity type defined
- Must have valid JSON schema structure
- Version doesn't matter (always use latest from server)

---

### API Integration

**Endpoint 1: List Template Packs**

```
GET /api/template-packs
Headers:
  Authorization: Bearer <token>
  X-Org-ID: <org-id>
  X-Project-ID: <project-id>

Response (200 OK):
{
  "template_packs": [
    {
      "name": "customer-discovery-v1",
      "version": "1.0.0",
      "description": "...",
      "entity_types_count": 5,
      "relationship_types_count": 8,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ]
}
```

**Endpoint 2: Get Template Pack Details**

```
GET /api/template-packs/{name}
Headers:
  Authorization: Bearer <token>
  X-Org-ID: <org-id>
  X-Project-ID: <project-id>

Response (200 OK):
{
  "name": "customer-discovery-v1",
  "version": "1.0.0",
  "description": "...",
  "entity_types": [...],
  "relationship_types": [...]
}

Response (404 Not Found):
{
  "error": {
    "code": "template_pack_not_found",
    "message": "Template pack 'invalid-pack' not found"
  }
}
```

**Authentication**: Uses same token refresh pattern as Phase 2 (OAuth Device Flow)

**Authorization**: Requires `template-packs:read` scope (future enhancement - currently open to authenticated users)

---

### Testing Strategy

**Unit Tests**:

```go
// internal/templatepacks/list_test.go
func TestListTemplatePacks_Success(t *testing.T) {
    // Mock HTTP server returning pack list
    // Assert correct table formatting
    // Assert JSON output matches API response
}

func TestListTemplatePacks_EmptyResponse(t *testing.T) {
    // Mock empty pack list
    // Assert user-friendly "No packs available" message
}

func TestListTemplatePacks_NetworkError(t *testing.T) {
    // Mock network failure
    // Assert error message includes troubleshooting guidance
}

// internal/templatepacks/info_test.go
func TestInfo_PackDetails_Success(t *testing.T) {
    // Mock pack with 3 entities, 5 relationships
    // Assert formatted output includes all sections
    // Assert properties are indented correctly
}

func TestInfo_PackNotFound(t *testing.T) {
    // Mock 404 response
    // Assert error includes list of available packs
}
```

**Integration Tests**:

```bash
# Test against mock server
go test -tags=integration ./cmd/template-packs/...

# Test list command
./emergent-cli template-packs list --server http://localhost:8080

# Test info command
./emergent-cli template-packs info test-pack-v1 --server http://localhost:8080
```

**E2E Tests** (Phase 5 integration):

```bash
# Workflow test
1. List packs
2. View pack info
3. Use pack in extraction start
4. Verify extraction job uses correct schema
```

---

### Configuration Integration (Phase 1)

Template pack commands respect global config settings:

```yaml
# ~/.emergent/config.yaml
server_url: https://api.emergent-company.ai
default_org: org_123
default_project: proj_456
output_format: table # Used by 'list' command if --format not specified
```

**Flag Priority** (same as Phase 3):

1. Command-line flags (`--format json`)
2. Environment variables (`EMERGENT_OUTPUT_FORMAT=json`)
3. Config file (`output_format: json`)
4. Default value (`table`)

---

### Error Handling Patterns

**Network Errors**:

```go
if err := client.Get(url); err != nil {
    return fmt.Errorf("failed to connect to server\n" +
        "URL: %s\n" +
        "Suggestion: Check your internet connection and server_url in config", url)
}
```

**Authentication Errors (401)**:

```go
if resp.StatusCode == 401 {
    return fmt.Errorf("authentication failed\n" +
        "Your token may have expired\n" +
        "Suggestion: Run 'emergent-cli config set-credentials' to re-authenticate")
}
```

**Not Found Errors (404)**:

```go
if resp.StatusCode == 404 {
    availablePacks := listPackNames() // Call list API
    return fmt.Errorf("template pack '%s' not found\n" +
        "Available packs: %s\n" +
        "Suggestion: Run 'emergent-cli template-packs list' to see all packs",
        packName, strings.Join(availablePacks, ", "))
}
```

**Malformed Response**:

```go
if resp.EntityTypes == nil || resp.RelationshipTypes == nil {
    return fmt.Errorf("invalid template pack data received from server\n" +
        "Missing required fields: entity_types or relationship_types\n" +
        "Suggestion: Contact your system administrator")
}
```

---

### Future Enhancements (Out of Scope for MVP)

**Not Implementing Now**:

1. **Local Cache**: All fetches go to server (simplifies implementation)
2. **Install/Uninstall**: No local storage of packs (server-authoritative)
3. **Version Pinning**: Always use latest from server (no `--version` flag)
4. **Pack Creation**: `template-packs create` would require complex validation (defer to backend/UI)
5. **Pack Validation**: `template-packs validate` redundant if no local creation (backend validates)
6. **Compiled Types**: `template-packs compiled-types` useful for SDK generation (future phase)
7. **Custom Pack Paths**: No `~/.emergent/template-packs/` directory (no local storage)

**Why Simplified Approach**:

- **Faster MVP delivery**: Focus on core discovery/inspection workflow
- **Server manages complexity**: Backend handles versioning, validation, storage
- **Reduced CLI size**: No local pack management = smaller binary
- **Always fresh data**: No cache staleness issues
- **Easier maintenance**: Fewer moving parts, less state to manage

**When to Add Features**:

- **Install/Uninstall**: If users need offline template pack access
- **Caching**: If network latency becomes a bottleneck (measure first!)
- **Version Pinning**: If reproducibility becomes critical (e.g., CI/CD)
- **Local Creation**: If power users want CLI-based pack authoring (consider UI-first approach)

---

### Documentation Requirements

**User-Facing Docs** (`docs/template-packs.md`):

- Purpose of template packs (what they define, why they matter)
- How to discover packs (`list` command usage)
- How to inspect pack schema (`info` command usage)
- Integration with extraction workflow (list → info → extract)
- Common use cases (customer discovery, contract analysis, etc.)
- Troubleshooting guide (network errors, auth errors, pack not found)

**Developer Docs** (`internal/templatepacks/README.md`):

- API client implementation (`ListTemplatePacks`, `GetTemplatePack`)
- Output formatting (table renderer, JSON/YAML/CSV serializers)
- Error handling patterns (network, auth, not found)
- Testing approach (unit, integration, E2E)

**Help Text** (Built-in):

```bash
$ emergent-cli template-packs --help

Manage and view template packs for knowledge extraction

Usage:
  emergent-cli template-packs [command]

Available Commands:
  list        List all available template packs
  info        View detailed schema of a template pack

Flags:
  -h, --help   help for template-packs

Use "emergent-cli template-packs [command] --help" for more information about a command.

$ emergent-cli template-packs list --help

List all available template packs from the server

Usage:
  emergent-cli template-packs list [flags]

Flags:
      --format string   Output format (table|json|yaml|csv) (default "table")
  -h, --help            help for list

$ emergent-cli template-packs info --help

View detailed schema of a template pack (entity types, relationships)

Usage:
  emergent-cli template-packs info <name> [flags]

Flags:
      --format string   Output format (text|json) (default "text")
  -h, --help            help for info

Examples:
  # List all packs
  emergent-cli template-packs list

  # View pack details
  emergent-cli template-packs info customer-discovery-v1

  # Get JSON for scripting
  emergent-cli template-packs info customer-discovery-v1 --format json
```

---

## Phase 4: Chat Commands

### Overview

**Purpose**: Enable users to query extracted knowledge using natural language chat, supporting the primary workflow: **upload → extract → query**.

**MVP Scope**: Basic request/response chat. Single query per command, plain text output, optional document scoping.

**Out of Scope (MVP)**:

- Streaming responses (request/response only)
- Markdown rendering in terminal (plain text)
- Conversation history management
- Interactive chat mode (REPL)
- Advanced filtering (date ranges, entity types)

### Core Command: `chat send`

**Synopsis**:

```bash
emergent-cli chat send "query" [--document <id>] [--conversation <id>]
```

**Description**: Send a natural language query to the knowledge base and receive a text response.

**Arguments**:

- `query` (required, positional): The question or query string (enclose in quotes)

**Flags**:

- `--document <id>` (optional): Scope query to specific document
- `--conversation <id>` (optional, deferred to future): Continue existing conversation (not implemented in MVP)

**Configuration**:

- Respects org/project context from `~/.emergent/config.yaml`
- Uses `server_url` for API endpoint
- Requires valid credentials (OAuth token)

### API Endpoint

**Request**: `POST /api/chat/send`

**Headers**:

- `Authorization: Bearer <token>`
- `Content-Type: application/json`
- `X-Org-ID: <org-id>` (from config)
- `X-Project-ID: <project-id>` (from config)

**Request Body**:

```json
{
  "query": "What are the main customer pain points?",
  "document_id": "doc_abc123", // optional
  "conversation_id": null, // MVP: always null
  "max_tokens": 500 // optional
}
```

**Response** (200 OK):

```json
{
  "response": "Based on the extracted knowledge, the main customer pain points are:\n\n1. Slow onboarding process - Customers report taking 2-3 weeks to get started\n2. Complex pricing structure - Multiple customers expressed confusion about tiers\n3. Lack of mobile support - Field teams unable to access system on-the-go\n\nThese themes appeared consistently across interviews and support tickets.",
  "sources": [
    {
      "document_id": "doc_abc123",
      "document_name": "Customer Interview - Acme Corp",
      "relevance_score": 0.92
    },
    {
      "document_id": "doc_def456",
      "document_name": "Support Tickets Q1",
      "relevance_score": 0.85
    }
  ],
  "conversation_id": "conv_def456",
  "tokens_used": 342
}
```

### Command Implementation

**File**: `cmd/chat/send.go`

**Pattern** (Go/Cobra):

```go
package chat

import (
    "encoding/json"
    "fmt"
    "github.com/spf13/cobra"
    "github.com/emergent-cli/internal/api"
    "github.com/emergent-cli/internal/config"
    "github.com/emergent-cli/internal/output"
)

var sendCmd = &cobra.Command{
    Use:   "send [query]",
    Short: "Send a chat query to the knowledge base",
    Long: `Send a natural language query and receive a response from extracted knowledge.

Examples:
  emergent-cli chat send "What are the main customer pain points?"
  emergent-cli chat send "What are the key requirements?" --document doc_abc123
  emergent-cli chat send "Summarize findings" --document doc_def456`,
    Args: cobra.ExactArgs(1),
    RunE: runSendCommand,
}

func init() {
    sendCmd.Flags().String("document", "", "Scope query to specific document ID")
    sendCmd.Flags().String("conversation", "", "Continue existing conversation (future)")
}

func runSendCommand(cmd *cobra.Command, args []string) error {
    // Get query from positional arg
    query := args[0]

    // Get optional flags
    documentID, _ := cmd.Flags().GetString("document")
    conversationID, _ := cmd.Flags().GetString("conversation")

    // Get org/project context (Phase 1 pattern)
    cfg := config.Get()
    orgID := cfg.GetString("active_org")
    projectID := cfg.GetString("active_project")

    if orgID == "" || projectID == "" {
        return fmt.Errorf("no active organization or project selected\n" +
            "Run 'emergent-cli config set --org <org> --project <project>' to select context")
    }

    // Build request
    req := api.ChatSendRequest{
        Query:          query,
        DocumentID:     documentID,
        ConversationID: conversationID,
        MaxTokens:      500,
    }

    // Call API (Phase 2: auto-refresh token)
    client := api.NewClient()
    resp, err := client.SendChatQuery(req, orgID, projectID)
    if err != nil {
        return handleChatError(err)  // Smart error messages
    }

    // Format and display response (plain text)
    fmt.Printf("Query: %s\n\n", query)
    fmt.Printf("Response:\n%s\n\n", resp.Response)

    // Show sources (if any)
    if len(resp.Sources) > 0 {
        fmt.Printf("Sources: ")
        for i, src := range resp.Sources {
            if i > 0 {
                fmt.Printf(", ")
            }
            fmt.Printf("%s (%s)", src.DocumentID, src.DocumentName)
        }
        fmt.Println()
    }

    return nil
}

func handleChatError(err error) error {
    // Parse API error response
    apiErr, ok := err.(*api.Error)
    if !ok {
        return fmt.Errorf("chat query failed: %w", err)
    }

    // Provide specific guidance based on error code
    switch apiErr.Code {
    case "document_not_found":
        return fmt.Errorf("document not found\n" +
            "Document '%s' does not exist or you don't have access to it\n" +
            "Run 'emergent-cli documents list' to see available documents", apiErr.Details)

    case "no_extraction_data":
        return fmt.Errorf("no extracted knowledge available\n" +
            "The knowledge base is empty or no extractions have completed\n" +
            "Run 'emergent-cli extraction start <doc-id>' to extract knowledge first")

    case "query_timeout":
        return fmt.Errorf("query timed out after 30 seconds\n" +
            "Try a more specific question or check server status:\n" +
            "  emergent-cli server health")

    case "unauthorized":
        return fmt.Errorf("authentication failed\n" +
            "Your token may have expired\n" +
            "Run 'emergent-cli config set-credentials' to re-authenticate")

    default:
        return fmt.Errorf("chat error [%s]: %s\n%s",
            apiErr.Code, apiErr.Message, apiErr.Details)
    }
}
```

### Output Format

**Success (Plain Text)**:

```
Query: What are the main customer pain points?

Response:
Based on the extracted knowledge, the main customer pain points are:

1. Slow onboarding process - Customers report taking 2-3 weeks to get started
2. Complex pricing structure - Multiple customers expressed confusion about tiers
3. Lack of mobile support - Field teams unable to access system on-the-go

Sources: doc_abc123 (Customer Interview - Acme Corp), doc_def456 (Support Tickets Q1)
```

**Error (Document Not Found)**:

```
Error: document not found
Document 'doc_xyz' does not exist or you don't have access to it
Run 'emergent-cli documents list' to see available documents
```

**Error (No Extraction Data)**:

```
Error: no extracted knowledge available
The knowledge base is empty or no extractions have completed
Run 'emergent-cli extraction start <doc-id>' to extract knowledge first
```

**Error (Query Timeout)**:

```
Error: query timed out after 30 seconds
Try a more specific question or check server status:
  emergent-cli server health
```

### Error Handling

**Error Code Decision Tree**:

```
HTTP Status / Error Code                         → User-Facing Message
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
404 document_not_found                           → "Document 'X' not found. Run 'documents list'."
400 no_extraction_data                           → "No extracted knowledge. Run 'extraction start <doc-id>' first."
408 query_timeout                                → "Query timed out. Try a more specific question."
401 unauthorized                                 → "Authentication failed. Run 'config set-credentials'."
500 internal_server_error                        → "Server error. Check 'server health' or contact admin."
```

**Network Errors**:

```go
if err := client.SendRequest(req); err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        return fmt.Errorf("request timed out\n" +
            "Check your internet connection or server_url in config")
    }
    return fmt.Errorf("network error: %w", err)
}
```

### Integration Points

**Phase 1 (Config)**:

- Uses `active_org`, `active_project` from config
- Uses `server_url` for API endpoint
- Respects `output_format` for future JSON output

**Phase 2 (Auth)**:

- Uses OAuth token from credentials file
- Applies `PersistentPreRunE` for automatic token refresh
- Handles 401 errors with re-auth guidance

**Phase 3 (Documents)**:

- Validates `--document` flag via GET `/api/documents/{id}` (optional pre-check)
- Error guidance points to `documents list` command

**Phase 5 (Extraction)**:

- Queries leverage entities/relationships extracted by jobs
- Error guidance points to `extraction start` command

**Phase 7 (Template Packs)**:

- LLM uses entity types/relationships from template pack schema
- Query results reference entities defined by chosen pack

### Example Workflows

**Workflow 1: Query Entire Knowledge Base**

```bash
$ emergent-cli chat send "What products does the company offer?"

Query: What products does the company offer?

Response:
The company offers three main product lines:

1. Enterprise Platform (SaaS) - Cloud-based solution for large organizations
2. Small Business Edition - Simplified version with core features
3. API Access (Developer) - RESTful API for custom integrations

All products include customer support and regular updates.

Sources: doc_abc123 (Product Overview 2024), doc_def456 (Sales Deck)
```

**Workflow 2: Query Specific Document**

```bash
$ emergent-cli chat send "What are the key requirements?" --document doc_abc123

Query: What are the key requirements?

Response:
The key requirements for this project are:

1. Multi-tenant architecture with isolated data
2. Real-time collaboration features
3. GDPR compliance for European customers
4. Mobile-first responsive design
5. API-first development approach

Sources: doc_abc123 (Requirements Document v3)
```

**Workflow 3: Continue Conversation (Future)**

```bash
# Not implemented in MVP
$ emergent-cli chat send "Tell me more about requirement #3" --conversation conv_xyz

Error: conversation history not supported yet
Use separate queries for now. Future versions will support conversation continuity.
```

### Testing Strategy

**Unit Tests** (`cmd/chat/send_test.go`):

- Query formatting (quotes, special characters)
- Response parsing (valid JSON, missing fields)
- Error message formatting (all error codes)
- Source attribution rendering

**Integration Tests** (`tests/integration/chat_test.go`):

- Mock API server responses (200, 404, 400, 408, 401, 500)
- Token refresh on 401 (Phase 2 integration)
- Config context loading (Phase 1 integration)

**E2E Tests** (`tests/e2e/chat_test.go`):

- Full workflow: upload doc → extract → chat query
- Document scoping: query specific doc
- Error handling: query with no extractions
- Auth flow: expired token → refresh → retry

**Test Fixtures**:

```go
// tests/fixtures/chat_responses.go
var ValidChatResponse = `{
  "response": "Test response",
  "sources": [{"document_id": "doc_123", "document_name": "Test Doc"}],
  "conversation_id": "conv_456",
  "tokens_used": 100
}`

var DocumentNotFoundError = `{
  "error": {
    "code": "document_not_found",
    "message": "Document not found",
    "details": "doc_xyz"
  }
}`
```

### CLI Help Text

**Parent Command** (`chat --help`):

```
Query extracted knowledge using natural language

Usage:
  emergent-cli chat [command]

Available Commands:
  send        Send a chat query to the knowledge base

Flags:
  -h, --help   help for chat

Use "emergent-cli chat [command] --help" for more information about a command.
```

**Send Command** (`chat send --help`):

```
Send a natural language query and receive a response from extracted knowledge

Usage:
  emergent-cli chat send [query] [flags]

Flags:
      --document string        Scope query to specific document ID
      --conversation string    Continue existing conversation (future)
  -h, --help                   help for send

Examples:
  # Query entire knowledge base
  emergent-cli chat send "What are the main customer pain points?"

  # Query specific document
  emergent-cli chat send "What are the key requirements?" --document doc_abc123

  # Continue conversation (future)
  emergent-cli chat send "Tell me more about requirement #3" --conversation conv_xyz
```

### Performance Considerations

**Request Timeout**: 30 seconds default (configurable via flag or config)

```go
client.SetTimeout(30 * time.Second)
```

**Response Size**: Backend should limit response length

- Default: 500 tokens (~375 words, ~2000 characters)
- Prevents terminal overflow
- User can increase via API directly if needed

**Rate Limiting**: Respect server rate limits

- 429 status → exponential backoff with retry
- Display wait time to user

### Future Enhancements (Out of Scope for MVP)

**Not Implementing Now**:

1. **Streaming Responses**: `chat send --stream` (real-time token streaming)
2. **Conversation History**: Automatic conversation context management
3. **Interactive Mode**: `chat interactive` (REPL-style continuous chat)
4. **Markdown Rendering**: Format response with bold/italic/lists in terminal
5. **Advanced Filtering**: `--entity-type`, `--date-range`, `--confidence-min`
6. **Export Results**: `--output results.json` (save query/response to file)
7. **Batch Queries**: `chat batch questions.txt` (process multiple queries)
8. **Query Templates**: `chat send @template:customer-feedback` (predefined queries)

**Why Simplified Approach**:

- **Faster MVP delivery**: Focus on core query/response workflow
- **Clear UX**: Single query, single response, no state management
- **Easy testing**: No conversation state, no streaming complexity
- **Proven pattern**: Most CLI tools start with request/response (curl, httpie)

**When to Add Features**:

- **Streaming**: If users complain about slow responses (measure latency first!)
- **Conversations**: If users need multi-turn context (track feature requests)
- **Interactive Mode**: If users prefer REPL over shell history (measure usage)
- **Markdown**: If plain text feels insufficient (gather feedback first)

---

### Documentation Requirements

**User-Facing Docs** (`docs/chat.md`):

- Purpose of chat queries (how LLM uses extracted knowledge)
- Basic usage (`send` command with examples)
- Document scoping (when/why to use `--document`)
- Understanding sources (how to interpret source attribution)
- Troubleshooting guide (no extractions, timeout, auth errors)
- Integration with extraction workflow (extract first, then query)

**Developer Docs** (`internal/chat/README.md`):

- API client implementation (`SendChatQuery`)
- Request/response types (`ChatSendRequest`, `ChatSendResponse`)
- Error handling patterns (network, auth, not found, timeout)
- Testing approach (unit, integration, E2E)

**Help Text** (Built-in):

```bash
$ emergent-cli chat --help

Query extracted knowledge using natural language

Usage:
  emergent-cli chat [command]

Available Commands:
  send        Send a chat query to the knowledge base

Flags:
  -h, --help   help for chat

Use "emergent-cli chat [command] --help" for more information about a command.

$ emergent-cli chat send --help

Send a natural language query and receive a response from extracted knowledge

Usage:
  emergent-cli chat send [query] [flags]

Flags:
      --document string        Scope query to specific document ID
      --conversation string    Continue existing conversation (future)
  -h, --help                   help for send

Examples:
  # Query entire knowledge base
  emergent-cli chat send "What are the main customer pain points?"

  # Query specific document
  emergent-cli chat send "What are the key requirements?" --document doc_abc123

  # Continue conversation (future)
  emergent-cli chat send "Tell me more about requirement #3" --conversation conv_xyz
```

---

## CLI Architecture

### Technology Stack

**Framework**: [Cobra](https://github.com/spf13/cobra)

- Industry standard (used by kubectl, gh, hugo, docker)
- Go native CLI framework
- Automatic help generation
- Flag parsing built-in
- Subcommand management
- Shell completion support (bash, zsh, fish, powershell)

**HTTP Client**: [go-resty](https://github.com/go-resty/resty) or stdlib net/http

- Clean API (similar to axios/got)
- Automatic retry with exponential backoff
- Request/response middleware
- JSON marshaling built-in
- Timeout handling
- TLS configuration support

**Configuration**: [Viper](https://github.com/spf13/viper)

- YAML/JSON/TOML/ENV support
- Environment variable integration
- Live watching (optional)
- Cross-platform config paths (~/.emergent/config.yaml)
- Nested configuration structures
- Config file precedence (defaults → config → env → flags)

**Credential Storage**: Standard file permissions (no encryption library needed)

- `~/.emergent/credentials.json` (0600 permissions)
- OS-level protection via file permissions
- Standard practice (Docker, kubectl, AWS CLI, gcloud, GitHub CLI)
- Simple implementation, no external dependencies
- Works everywhere (SSH sessions, containers, CI/CD)

### Command Structure

```bash
emergent-cli <command> <subcommand> [options]

# Configuration
emergent-cli config set-server --url https://api.example.com
emergent-cli config set-credentials --email user@example.com
emergent-cli config show

# Documents
emergent-cli documents list [--org ORG] [--project PROJECT]
emergent-cli documents create --file path/to/doc.pdf
emergent-cli documents delete <document-id>
emergent-cli documents search "query text"

# Chat
emergent-cli chat send "What is in the knowledge base?"
emergent-cli chat history [--limit 50]

# Extraction
emergent-cli extraction run <document-id>
emergent-cli extraction status <job-id>
emergent-cli extraction list-jobs [--status running]

# Admin
emergent-cli admin orgs list
emergent-cli admin projects list --org <org-id>
emergent-cli admin users list

# Server
emergent-cli server health
emergent-cli server info

# Template Packs
emergent-cli template-packs list
emergent-cli template-packs get <pack-id>
emergent-cli template-packs validate --file pack.json  # Validate pack JSON structure before creation
emergent-cli template-packs create --file pack.json
emergent-cli template-packs installed
emergent-cli template-packs install <pack-id>
emergent-cli template-packs uninstall <pack-id>
emergent-cli template-packs compiled-types

# Serve Mode (Docs + MCP Proxy)
emergent-cli serve --docs-port 8080                    # Docs server only
emergent-cli serve --mcp-stdio                         # MCP over stdio (Claude Desktop)
emergent-cli serve --mcp-port 3100                     # MCP over HTTP/SSE
emergent-cli serve --docs-port 8080 --mcp-port 3100   # Both servers
```

### Configuration File Structure

**File**: `~/.emergent/config.yaml` (permissions: 0644)

```yaml
# Server configuration
server_url: https://api.dev.emergent-company.ai
zitadel_issuer: https://auth.dev.emergent-company.ai
zitadel_client_id: emergent-cli

# Default context (remembered from last use)
default_org: org_123
default_project: proj_456

# Output preferences
output_format: table # table | json | yaml | csv
color: auto # auto | always | never
verbose: false

# Server profiles (optional, for multi-environment)
profiles:
  dev:
    server_url: https://api.dev.emergent-company.ai
    zitadel_issuer: https://auth.dev.emergent-company.ai
  prod:
    server_url: https://api.emergent-company.ai
    zitadel_issuer: https://auth.emergent-company.ai
active_profile: dev
```

### Credential Storage

**File**: `~/.emergent/credentials.json` (permissions: 0600 - owner read/write only)

**Security Model**: OS-level file permissions (industry standard pattern used by Docker, kubectl, AWS CLI, gcloud, GitHub CLI)

**Format**:

```json
{
  "email": "user@example.com",
  "password": "user-password",
  "token_cache": {
    "access_token": "eyJhbGc...",
    "refresh_token": "eyJhbGc...",
    "expires_at": "2026-02-05T12:00:00Z"
  }
}
```

**Fallback**: Environment variables for CI/CD and headless environments

```bash
export EMERGENT_SERVER_URL="https://api.dev.emergent-company.ai"
export EMERGENT_EMAIL="ci-bot@example.com"
export EMERGENT_PASSWORD="ci-password"
```

**Precedence** (highest to lowest):

1. Command-line flags (`--email`, `--password`)
2. Environment variables (`EMERGENT_EMAIL`, `EMERGENT_PASSWORD`)
3. Credentials file (`~/.emergent/credentials.json`)

**Permission Verification** (on startup):

```go
fileInfo, _ := os.Stat(credsPath)
if fileInfo.Mode().Perm() != 0600 {
    fmt.Fprintf(os.Stderr, "⚠️  Warning: credentials file has insecure permissions\n")
    fmt.Fprintf(os.Stderr, "Run: chmod 0600 %s\n", credsPath)
}
```

### Project Structure

```
tools/emergent-cli/
├── cmd/
│   └── emergent-cli/
│       └── main.go              # Entry point
├── internal/
│   ├── auth/
│   │   ├── manager.go           # OAuth password grant flow
│   │   ├── token_cache.go       # Token caching with expiry
│   │   └── credentials.go       # File-based credential storage (0600)
│   ├── api/
│   │   ├── client.go            # Base API client (go-resty)
│   │   ├── documents.go         # Document API methods
│   │   ├── chat.go              # Chat API methods
│   │   ├── extraction.go        # Extraction API methods
│   │   └── admin.go             # Admin API methods
│   ├── cmd/
│   │   ├── root.go              # Root command setup (Cobra)
│   │   ├── config.go            # config set/show commands
│   │   ├── documents.go         # documents commands
│   │   ├── chat.go              # chat commands
│   │   ├── extraction.go        # extraction commands
│   │   ├── admin.go             # admin commands
│   │   ├── server.go            # server health commands
│   │   ├── template_packs.go    # template-packs commands
│   │   └── serve.go             # serve command (docs + MCP)
│   ├── config/
│   │   └── config.go            # Config management (Viper)
│   ├── output/
│   │   ├── formatter.go         # Output formatter interface
│   │   ├── table.go             # Table formatter (tablewriter)
│   │   ├── json.go              # JSON formatter
│   │   ├── yaml.go              # YAML formatter
│   │   └── csv.go               # CSV formatter
│   ├── prompt/
│   │   └── interactive.go       # Interactive prompts (survey)
│   ├── docs/                    # Documentation server
│   │   ├── server.go            # HTTP server for docs
│   │   ├── render.go            # HTML generation from Cobra
│   │   └── templates/           # Embedded HTML templates
│   │       ├── index.html
│   │       ├── command.html
│   │       └── style.css
│   └── mcp/                     # MCP proxy server
│       ├── server.go            # MCP server implementation
│       ├── tools.go             # Tool generation from Cobra commands
│       ├── stdio.go             # stdio transport (Claude Desktop)
│       └── http.go              # HTTP/SSE transport
├── go.mod
├── go.sum
├── Makefile                     # Build targets for all platforms
├── README.md
└── project.json                 # Nx configuration
```

## Serve Mode Architecture

The CLI operates in multiple modes from a single binary:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         emergent-cli                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐    │
│   │   CLI Mode  │    │ Docs Server │    │    MCP Server       │    │
│   │             │    │             │    │                     │    │
│   │ $ emergent  │    │ :8080/      │    │ stdio / SSE / HTTP  │    │
│   │   documents │    │             │    │                     │    │
│   │   list      │    │ Live HTML   │    │ tools:              │    │
│   │             │    │ from Cobra  │    │ - documents_list    │    │
│   └─────────────┘    └─────────────┘    │ - extraction_run    │    │
│          │                  │           │ - template_packs_*  │    │
│          │                  │           └─────────────────────┘    │
│          └──────────────────┴────────────────────┘                  │
│                             │                                       │
│                    ┌────────▼────────┐                              │
│                    │  Shared Core    │                              │
│                    │ • API Client    │                              │
│                    │ • Auth Manager  │                              │
│                    │ • Config        │                              │
│                    └─────────────────┘                              │
└─────────────────────────────────────────────────────────────────────┘
```

### Serve Command Options

```bash
# Documentation server only
emergent-cli serve --docs-port 8080
# Serves: http://localhost:8080

# MCP server over stdio (for Claude Desktop)
emergent-cli serve --mcp-stdio
# Reads/writes JSON-RPC over stdin/stdout

# MCP server over HTTP/SSE (for web clients)
emergent-cli serve --mcp-port 3100
# Serves: http://localhost:3100/mcp

# Both servers simultaneously
emergent-cli serve --docs-port 8080 --mcp-port 3100
```

### Documentation Server

#### Architecture Overview

The documentation server generates static HTML pages from the Cobra command tree at runtime, ensuring documentation always reflects the actual CLI implementation.

```
Cobra Command Tree
       ↓
Generator (cmd/docs/generator.go)
  ├─ Walks command tree via cmd.Commands()
  ├─ Extracts: name, usage, flags, examples, subcommands
  └─ Builds CommandDoc structs
       ↓
HTTP Server (cmd/docs/server.go)
  ├─ Routes:
  │   └─ GET /           → Homepage (all commands)
  │   └─ GET /cmd/{name} → Individual command page
  │   └─ GET /schema.json → JSON schema for tooling
  └─ Renders HTML templates with Tailwind classes
       ↓
Templates (cmd/docs/templates/)
  ├─ layout.html    (shell with <head>, sidebar, main slot)
  ├─ index.html     (homepage: command grid/list)
  ├─ command.html   (command detail: usage, flags, examples)
  └─ partials/      (sidebar, header, command-card)
```

#### Technical Stack

- **Language**: Go (matches CLI implementation)
- **Templating**: `html/template` (server-side rendering, no JavaScript framework)
- **Styling**: Tailwind CSS via CDN (zero build step, can migrate to embedded later)
- **HTTP**: Standard library `net/http` (sufficient for static content)
- **Embedding**: Go `embed` package for bundling templates into binary
- **Introspection**: Cobra's built-in command tree traversal

#### HTTP Endpoints

| Route          | Method | Response | Purpose                                     |
| -------------- | ------ | -------- | ------------------------------------------- |
| `/`            | GET    | HTML     | Homepage with command grid/cards            |
| `/cmd/{name}`  | GET    | HTML     | Individual command documentation page       |
| `/schema.json` | GET    | JSON     | Machine-readable command schema for tooling |

#### Template Structure

```
cmd/docs/templates/
  ├─ layout.html              # Shell template
  │   ├─ <head> with Tailwind CDN
  │   ├─ Dark mode toggle (System/Dark/Light)
  │   ├─ Sidebar (desktop) / Hamburger menu (mobile)
  │   └─ {{ template "content" . }} slot
  │
  ├─ index.html               # Homepage template
  │   ├─ Command grid (3 columns on desktop)
  │   ├─ Command cards with icon, name, description
  │   └─ Responsive layout (1 column on mobile)
  │
  ├─ command.html             # Command detail template
  │   ├─ Command name and synopsis
  │   ├─ Usage section
  │   ├─ Flags table (name, type, default, description)
  │   ├─ Examples section (from cmd.Example)
  │   └─ Subcommands list (if any)
  │
  └─ partials/
      ├─ sidebar.html         # Navigation sidebar
      │   ├─ Logo/branding
      │   ├─ Command tree (collapsible sections)
      │   └─ Dark mode toggle
      │
      ├─ header.html          # Mobile header
      │   ├─ Hamburger menu button
      │   └─ Logo/title
      │
      └─ command-card.html    # Command card component
          ├─ Icon (if provided)
          ├─ Command name
          ├─ Short description
          └─ Link to detail page
```

#### Tailwind Integration

**CDN Approach** (MVP):

```html
<!-- In layout.html <head> -->
<link href="https://cdn.tailwindcss.com" rel="stylesheet" />
<script>
  tailwind.config = {
    darkMode: 'class',
    theme: {
      extend: {
        colors: {
          primary: '#3b82f6', // blue-500
          secondary: '#64748b', // slate-500
        },
      },
    },
  };
</script>
```

**Utility Classes**:

- Layout: `flex`, `grid`, `gap-*`, `p-*`, `m-*`
- Responsive: `sm:`, `md:`, `lg:` prefixes
- Dark mode: `dark:bg-*`, `dark:text-*`
- Typography: `text-*`, `font-*`, `leading-*`
- Interactive: `hover:*`, `focus:*`, `transition-*`

**Example Card Component**:

```html
<div
  class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 hover:shadow-lg transition-shadow"
>
  <h3 class="text-xl font-semibold text-gray-900 dark:text-white">
    {{ .Name }}
  </h3>
  <p class="mt-2 text-gray-600 dark:text-gray-300">{{ .Short }}</p>
</div>
```

#### Dark Mode Implementation

**3-Way Toggle** (System / Dark / Light):

```html
<!-- In sidebar.html partial -->
<div class="dark-mode-toggle flex gap-2">
  <button onclick="setTheme('system')" class="btn-theme" data-theme="system">
    System
  </button>
  <button onclick="setTheme('dark')" class="btn-theme" data-theme="dark">
    Dark
  </button>
  <button onclick="setTheme('light')" class="btn-theme" data-theme="light">
    Light
  </button>
</div>

<script>
  function setTheme(theme) {
    localStorage.setItem('theme', theme);
    applyTheme();
  }

  function applyTheme() {
    const theme = localStorage.getItem('theme') || 'system';
    const prefersDark = window.matchMedia(
      '(prefers-color-scheme: dark)'
    ).matches;

    if (theme === 'dark' || (theme === 'system' && prefersDark)) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }

    // Highlight active button
    document.querySelectorAll('.btn-theme').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.theme === theme);
    });
  }

  // Apply on load
  applyTheme();

  // Watch for system preference changes
  window
    .matchMedia('(prefers-color-scheme: dark)')
    .addEventListener('change', () => {
      if (localStorage.getItem('theme') === 'system') {
        applyTheme();
      }
    });
</script>
```

**CSS Classes**:

```css
.btn-theme {
  @apply px-3 py-1 rounded text-sm transition-colors;
  @apply bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300;
}
.btn-theme.active {
  @apply bg-primary text-white;
}
```

#### Mobile Layout

**Responsive Breakpoints**:

- Mobile: `< 768px` (1 column, hamburger menu)
- Tablet: `768px - 1024px` (2 columns, collapsible sidebar)
- Desktop: `> 1024px` (3 columns, persistent sidebar)

**Hamburger Menu Pattern**:

```html
<!-- Mobile header (visible on < 768px) -->
<header class="lg:hidden flex justify-between items-center p-4 border-b">
  <h1 class="text-xl font-bold">CLI Docs</h1>
  <button onclick="toggleSidebar()" class="hamburger">
    <svg><!-- hamburger icon --></svg>
  </button>
</header>

<!-- Sidebar (hidden on mobile by default) -->
<aside id="sidebar" class="sidebar-mobile lg:sidebar-desktop">
  <!-- Navigation content -->
</aside>

<style>
  .sidebar-mobile {
    @apply fixed inset-y-0 left-0 transform -translate-x-full transition-transform;
    @apply lg:relative lg:translate-x-0 lg:block;
  }
  .sidebar-mobile.open {
    @apply translate-x-0;
  }
</style>

<script>
  function toggleSidebar() {
    document.getElementById('sidebar').classList.toggle('open');
  }
</script>
```

#### Cobra Introspection

**Command Tree Walking**:

```go
// cmd/docs/generator.go

type CommandDoc struct {
    Name        string
    Use         string
    Short       string
    Long        string
    Example     string
    Flags       []FlagDoc
    Subcommands []CommandDoc
}

type FlagDoc struct {
    Name        string
    Shorthand   string
    Type        string
    Default     string
    Usage       string
    Required    bool
}

func GenerateCommandDocs(rootCmd *cobra.Command) []CommandDoc {
    var docs []CommandDoc

    // Walk command tree recursively
    for _, cmd := range rootCmd.Commands() {
        if cmd.Hidden {
            continue // Skip hidden commands
        }

        doc := CommandDoc{
            Name:    cmd.Name(),
            Use:     cmd.Use,
            Short:   cmd.Short,
            Long:    cmd.Long,
            Example: cmd.Example,
        }

        // Extract flags
        cmd.Flags().VisitAll(func(flag *pflag.Flag) {
            doc.Flags = append(doc.Flags, FlagDoc{
                Name:      flag.Name,
                Shorthand: flag.Shorthand,
                Type:      flag.Value.Type(),
                Default:   flag.DefValue,
                Usage:     flag.Usage,
                Required:  isRequiredFlag(cmd, flag.Name),
            })
        })

        // Recursively process subcommands
        doc.Subcommands = GenerateCommandDocs(cmd)
        docs = append(docs, doc)
    }

    return docs
}
```

**Example Integration**:

```go
// Cobra's cmd.Example field contains usage examples
var documentsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List documents",
    Long:  "List all documents in your project",
    Example: `  # List all documents
  emergent-cli documents list

  # List with filtering
  emergent-cli documents list --org acme

  # List with pagination
  emergent-cli documents list --limit 50 --offset 100`,
    RunE: func(cmd *cobra.Command, args []string) error {
        // Command implementation
    },
}
```

#### Server Implementation

**HTTP Handler Pseudo-code**:

```go
// cmd/docs/server.go

func StartDocsServer(rootCmd *cobra.Command, port int) error {
    // Generate command documentation
    docs := GenerateCommandDocs(rootCmd)

    // Setup templates with embed
    //go:embed templates/*
    var templatesFS embed.FS

    tmpl := template.Must(template.ParseFS(templatesFS,
        "templates/*.html",
        "templates/partials/*.html"))

    // Route handlers
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        data := struct {
            Commands []CommandDoc
        }{Commands: docs}

        tmpl.ExecuteTemplate(w, "index.html", data)
    })

    http.HandleFunc("/cmd/", func(w http.ResponseWriter, r *http.Request) {
        name := strings.TrimPrefix(r.URL.Path, "/cmd/")
        cmd := findCommand(docs, name)

        if cmd == nil {
            http.NotFound(w, r)
            return
        }

        tmpl.ExecuteTemplate(w, "command.html", cmd)
    })

    http.HandleFunc("/schema.json", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(docs)
    })

    log.Printf("Documentation server running on http://localhost:%d\n", port)
    return http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}
```

#### Example HTML Templates

**layout.html**:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>{{ if .Title }}{{ .Title }} - {{ end }}CLI Documentation</title>
    <link href="https://cdn.tailwindcss.com" rel="stylesheet" />
    <script>
      tailwind.config = { darkMode: 'class' };
    </script>
  </head>
  <body class="bg-gray-50 dark:bg-gray-900">
    <div class="flex min-h-screen">
      {{ template "sidebar" . }}

      <main class="flex-1 p-6">{{ template "content" . }}</main>
    </div>

    <script src="/static/theme.js"></script>
  </body>
</html>
```

**command.html**:

```html
{{ define "content" }}
<article class="max-w-4xl">
  <h1 class="text-4xl font-bold text-gray-900 dark:text-white mb-4">
    {{ .Name }}
  </h1>

  <p class="text-xl text-gray-600 dark:text-gray-400 mb-8">{{ .Short }}</p>

  {{ if .Long }}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Description</h2>
    <p class="text-gray-700 dark:text-gray-300">{{ .Long }}</p>
  </section>
  {{ end }} {{ if .Flags }}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Flags</h2>
    <table class="min-w-full bg-white dark:bg-gray-800 rounded-lg">
      <thead>
        <tr class="border-b dark:border-gray-700">
          <th class="px-4 py-2 text-left">Flag</th>
          <th class="px-4 py-2 text-left">Type</th>
          <th class="px-4 py-2 text-left">Default</th>
          <th class="px-4 py-2 text-left">Description</th>
        </tr>
      </thead>
      <tbody>
        {{ range .Flags }}
        <tr class="border-b dark:border-gray-700">
          <td class="px-4 py-2 font-mono">--{{ .Name }}</td>
          <td class="px-4 py-2">{{ .Type }}</td>
          <td class="px-4 py-2">{{ .Default }}</td>
          <td class="px-4 py-2">{{ .Usage }}</td>
        </tr>
        {{ end }}
      </tbody>
    </table>
  </section>
  {{ end }} {{ if .Example }}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Examples</h2>
    <pre class="bg-gray-100 dark:bg-gray-800 p-4 rounded-lg overflow-x-auto">
            <code>{{ .Example }}</code>
        </pre>
  </section>
  {{ end }}
</article>
{{ end }}
```

#### Benefits of This Approach

**Always Current**:

- No separate docs build step
- Impossible for docs to drift from code
- Changes to commands immediately reflected

**Zero Dependencies**:

- No docs framework (Docusaurus, MkDocs, etc.)
- Single binary contains CLI + docs server
- Works offline

**Developer Friendly**:

- Add docs by adding to Cobra commands
- `Short`, `Long`, `Example` fields become documentation
- No separate Markdown files to maintain

**User Friendly**:

- Clean, responsive UI
- Dark mode support
- Mobile-friendly
- Fast (no JavaScript framework overhead)

#### Future Enhancements (Post-MVP)

Not implementing in MVP, but documented for Phase 2:

- Migrate from Tailwind CDN to embedded CSS (faster load)
- Full-text search using Fuse.js or similar
- Syntax highlighting for example code blocks
- Copy-to-clipboard buttons
- Keyboard shortcuts (/, ?, ESC navigation)
- Version comparison (if multiple CLI versions deployed)

### MCP Server

Exposes CLI functionality as MCP tools for AI agents:

**Tool Generation**:

- Each leaf command becomes an MCP tool
- Flags become tool input schema properties
- `documents list --org X` → `documents_list({ org: "X" })`

**Transports**:
| Transport | Use Case | Configuration |
|-----------|----------|---------------|
| stdio | Claude Desktop | `--mcp-stdio` |
| HTTP/SSE | Web clients, remote | `--mcp-port 3100` |

**Claude Desktop Integration**:

```json
// ~/Library/Application Support/Claude/claude_desktop_config.json
{
  "mcpServers": {
    "emergent": {
      "command": "emergent-cli",
      "args": ["serve", "--mcp-stdio"]
    }
  }
}
```

**Generated MCP Tools**:
| Tool Name | Maps To | Description |
|-----------|---------|-------------|
| `documents_list` | `documents list` | List documents |
| `documents_create` | `documents create` | Upload document |
| `extraction_run` | `extraction run` | Start extraction |
| `extraction_status` | `extraction status` | Check job status |
| `template_packs_list` | `template-packs list` | List packs |
| `template_packs_install` | `template-packs install` | Install pack |
| `chat_send` | `chat send` | Send chat message |

### Error Handling Strategy

**Categories**:

1. **Authentication Errors** (401, 403)

   - Message: "Authentication failed. Run 'emergent-cli config set-credentials'"
   - Check token expiry, attempt refresh, guide user

2. **Invalid Configuration** (missing org/project)

   - Message: "No organization selected. Use --org flag or run 'emergent-cli admin orgs list'"
   - Suggest available options when possible

3. **Network Errors**

   - Retry with exponential backoff (3 attempts)
   - Clear error messages (timeout, connection refused, DNS)

4. **API Errors** (4xx, 5xx)

   - Display structured error from API
   - Include request ID for support tickets

5. **Validation Errors** (invalid input)
   - Show specific field errors
   - Suggest valid values/formats

### Output Formatting

**Formats**: `table` (default), `json`, `yaml`, `csv`

**Examples**:

```bash
# Table (default)
emergent-cli documents list
┌──────────────────────────────────────┬─────────────────────┬──────────┐
│ ID                                   │ Title               │ Status   │
├──────────────────────────────────────┼─────────────────────┼──────────┤
│ 123e4567-e89b-12d3-a456-426614174000 │ Product Spec        │ indexed  │
│ 223e4567-e89b-12d3-a456-426614174001 │ Meeting Notes       │ pending  │
└──────────────────────────────────────┴─────────────────────┴──────────┘

# JSON (for scripting)
emergent-cli documents list --output json
[
  {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "Product Spec",
    "status": "indexed"
  }
]

# CSV (for spreadsheets)
emergent-cli documents list --output csv
id,title,status
123e4567-e89b-12d3-a456-426614174000,Product Spec,indexed
```

### Interactive Mode

**Prompt for missing required values**:

```bash
$ emergent-cli documents create
? Select organization: (Use arrow keys)
❯ Acme Corp (org-123)
  Example Inc (org-456)

? Select project:
❯ Product Docs (proj-789)
  Engineering Wiki (proj-012)

? File path: [enter path or drag-and-drop]
```

**Confirmation prompts for destructive actions**:

```bash
$ emergent-cli documents delete doc-123
⚠ This will permanently delete "Product Spec.pdf"
? Are you sure? (y/N)
```

## Testing Strategy

### Unit Tests

```bash
# Run all unit tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/auth/...
go test ./internal/api/...
go test ./internal/config/...
```

**Test Coverage**:

- Command parsing and validation (Cobra)
- Authentication token management (refresh, expiry)
- Output formatters (table, JSON, YAML, CSV)
- Error handling logic
- Configuration file read/write (Viper)
- Credential storage and permission checks

### Integration Tests

```bash
# Run integration tests (requires mock server)
go test -tags=integration ./...
```

**Test Scope**:

- Mock API responses for all commands
- Configuration file read/write operations
- Token caching and refresh flows
- End-to-end command execution

### E2E Tests (against dev server)

```bash
# Run E2E tests (requires EMERGENT_TEST_* env vars)
EMERGENT_TEST_SERVER=https://api.dev.emergent-company.ai \
EMERGENT_TEST_EMAIL=test@example.com \
EMERGENT_TEST_PASSWORD=testpass \
go test -tags=e2e ./...
```

**Test Scope**:

- Full authentication flow
- CRUD operations for documents
- Chat interaction
- Admin operations

### Test Commands (Nx Integration)

```bash
nx run emergent-cli:test           # Unit tests
nx run emergent-cli:test-cover     # With coverage
nx run emergent-cli:test-e2e       # E2E tests (requires dev server)
```

## Security Considerations

### Credential Protection

- Never log credentials or tokens
- Store credentials with 0600 file permissions (owner read/write only)
- Warn user if credentials file has insecure permissions
- Clear sensitive data from memory after use (explicit overwrite)
- Support environment variables for CI/CD (no file storage needed)

### Token Management

- Cache tokens in credentials file (`token_cache` object)
- Automatic token refresh before expiry (5 minute buffer)
- Revoke tokens on explicit logout (`emergent-cli config logout`)
- Credentials file permissions: 0600 (owner read/write only)

### Input Validation

- Sanitize all user inputs
- Validate UUIDs, URLs, email formats
- Prevent command injection in shell contexts

### Audit Trail

- Log all commands to `~/.emergent/cli-audit.log`
- Include timestamp, command, org/project context
- Exclude sensitive data (passwords, tokens)

## Deployment & Distribution

### Binary Distribution

**Primary**: GitHub Releases with pre-built binaries

```bash
# Option 1: go install (for Go developers)
go install github.com/emergent-company/emergent/tools/emergent-cli/cmd/emergent-cli@latest

# Option 2: Download binary from releases
curl -L https://github.com/emergent-company/emergent/releases/download/v1.0.0/emergent-cli-darwin-arm64 -o emergent-cli
chmod +x emergent-cli
sudo mv emergent-cli /usr/local/bin/

# Option 3: Homebrew (future)
brew install emergent-company/tap/emergent-cli
```

### Cross-Compilation (Makefile)

```makefile
# Build for all platforms
build-all:
	GOOS=darwin GOARCH=arm64 go build -o dist/emergent-cli-darwin-arm64 ./cmd/emergent-cli
	GOOS=darwin GOARCH=amd64 go build -o dist/emergent-cli-darwin-amd64 ./cmd/emergent-cli
	GOOS=linux GOARCH=amd64 go build -o dist/emergent-cli-linux-amd64 ./cmd/emergent-cli
	GOOS=linux GOARCH=arm64 go build -o dist/emergent-cli-linux-arm64 ./cmd/emergent-cli
	GOOS=windows GOARCH=amd64 go build -o dist/emergent-cli-windows-amd64.exe ./cmd/emergent-cli

# Build for current platform
build:
	go build -o emergent-cli ./cmd/emergent-cli

# Install locally
install:
	go install ./cmd/emergent-cli
```

### Advantages over npm Distribution

- Single ~10MB binary, no runtime dependencies
- No Node.js installation required
- Instant startup (<50ms vs ~500ms for Node.js)
- Cross-platform builds from single machine
- Users can `curl` binary directly
- Works in minimal Docker containers (scratch, alpine)
- No dependency conflicts or version issues

### Versioning

- Follow semantic versioning (semver)
- Major version for breaking CLI changes
- Minor version for new commands
- Patch version for bug fixes
- Version embedded at build time: `go build -ldflags "-X main.version=1.0.0"`

### Update Strategy

```bash
emergent-cli version              # Show current version
emergent-cli version --check      # Check for updates
# Self-update not supported initially (use package manager or re-download)
```

## Performance Targets

- **Cold start**: < 50ms (Go binary, no JIT)
- **Initial authentication**: < 1 second
- **Cached token operations**: < 100ms
- **List operations**: < 500ms for 100 items
- **Create document**: < 3 seconds for 10MB file
- **Binary size**: ~10-15MB (single executable)
- **Memory usage**: < 50MB for typical operations

## Compatibility

- **Go version**: 1.21+ (current stable)
- **Platforms**: macOS, Linux, Windows
- **Architectures**: amd64 (x86_64), arm64 (Apple Silicon, ARM servers)
- **Server API**: Compatible with current v1 API (no backend changes required)
- **Shell completion**: bash, zsh, fish, PowerShell (via Cobra)

## Future Enhancements (v2+)

1. **Service Account Support** (Option B authentication)
2. **Plugin System** for custom commands
3. **Interactive Shell Mode** (`emergent-cli shell`)
4. **Batch Operations** from CSV/JSON input
5. **Watch Mode** for continuous sync
6. **Desktop Notifications** for long-running jobs
7. **Autocomplete** for shells (bash, zsh, fish)

## Migration from Workspace CLI

**Clarification**: This CLI is **not replacing** the existing workspace CLI (`tools/workspace-cli`).

**Separation of Concerns**:

- **Workspace CLI**: Local process management (PM2, Docker, logs)
- **New CLI**: Remote server API operations (documents, chat, admin)

**Example**:

```bash
# Start local server (workspace CLI)
npm run workspace:start

# Use remote server API (new CLI)
emergent-cli documents list --server https://api.dev.emergent-company.ai
```

## Open Questions

1. Should we support multiple server profiles (dev, staging, prod)?

   - **Proposal**: Yes, via `emergent-cli config use-profile <name>`

2. Should destructive operations require confirmation by default?

   - **Proposal**: Yes, with `--force` flag to skip

3. Should we support bulk operations (import 100 documents)?

   - **Proposal**: Yes, but v2 feature

4. How to handle long-running operations (extraction jobs)?
   - **Proposal**: Poll status with progress bar, return job ID for background mode

## Success Metrics

**Adoption**:

- 10+ active users within first month
- 50+ CLI invocations per day

**Reliability**:

- < 1% command failure rate (excluding user errors)
- < 5 reported auth issues per month

**Performance**:

- 95% of commands complete within 5 seconds
- No memory leaks over 1000 command executions
