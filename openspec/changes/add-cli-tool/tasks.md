# Tasks: CLI Tool Implementation

> **CRITICAL**: All implementation must follow test-first workflow:
>
> 1. Write failing test → 2. Implement (test passes) → 3. Run full suite → 4. Commit

## Phase 0: Test Infrastructure Setup (1 hour)

**Purpose**: Establish test helpers and utilities that will be used across all subsequent phases. Complete this phase BEFORE starting Phase 1.

### 0.1 Create Test Utilities Package (30m)

**Test-First Workflow**:

1. **Write tests for test utilities** (15m)

   - [ ] Create `internal/testutil/testutil_test.go`
   - [ ] Write `TestCreateTempConfig` - verify temp file creation, content, cleanup
   - [ ] Write `TestSetEnv` - verify env var set, cleanup after test
   - [ ] Write `TestCaptureOutput` - verify stdout/stderr capture, content retrieval
   - [ ] Run: `go test ./internal/testutil` → FAILS (package doesn't exist)

2. **Implement test utilities** (15m)

   - [ ] Create `internal/testutil/testutil.go`
   - [ ] Implement `CreateTempConfig(t *testing.T, content string) string`
     - Create temp file with given content
     - Register cleanup with `t.Cleanup()`
     - Return file path
   - [ ] Implement `SetEnv(t *testing.T, key, value string)`
     - Save original value
     - Set new value with `os.Setenv()`
     - Register cleanup to restore original
   - [ ] Implement `CaptureOutput() *OutputCapture`
     - Create pipes for stdout/stderr
     - Return struct with `Read() (string, string, error)` method
     - Include `Restore()` method
   - [ ] Run: `go test ./internal/testutil` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...` → All tests pass
   - [ ] Run: `go build ./cmd` → Compiles successfully
   - [ ] Git commit: "feat(testutil): add test utilities package"

**Deliverables**:

- Files: `testutil.go`, `testutil_test.go`
- Functions: `CreateTempConfig()`, `SetEnv()`, `CaptureOutput()`
- Tests: 6 unit tests passing (2 per function)
- Build: No compilation errors

**Verification**:

```bash
go test ./internal/testutil -v -cover
# Expected: PASS (6 tests), coverage: ≥80%
```

### 0.2 Create Mock HTTP Server Utilities (20m)

**Test-First Workflow**:

1. **Write mock server tests** (8m)

   - [ ] Create `internal/testutil/mock_server_test.go`
   - [ ] Write `TestNewMockServer` - verify server starts, responds to requests
   - [ ] Write `TestMockServerHandlers` - verify custom handler registration
   - [ ] Write `TestMockServerClose` - verify cleanup
   - [ ] Run: `go test ./internal/testutil` → FAILS (functions don't exist)

2. **Implement mock server** (10m)

   - [ ] Add to `internal/testutil/mock_server.go`
   - [ ] Implement `NewMockServer(handlers map[string]http.HandlerFunc) *httptest.Server`
     - Create test server
     - Register handlers for specified paths
     - Default handler returns 404
   - [ ] Implement `WithJSONResponse(statusCode int, body interface{}) http.HandlerFunc`
     - Helper to return JSON responses
   - [ ] Implement `WithDelayedResponse(delay time.Duration, handler http.HandlerFunc) http.HandlerFunc`
     - Wrapper for simulating network latency
   - [ ] Run: `go test ./internal/testutil` → PASSES

3. **Verify full suite** (2m)
   - [ ] Run: `go test ./...` → All tests pass
   - [ ] Run: `go build ./cmd` → Compiles
   - [ ] Git commit: "feat(testutil): add mock HTTP server utilities"

**Deliverables**:

- Files: `mock_server.go`, `mock_server_test.go`
- Functions: `NewMockServer()`, `WithJSONResponse()`, `WithDelayedResponse()`
- Tests: 3 unit tests passing
- Coverage: ≥70% for mock server package

**Verification**:

```bash
go test ./internal/testutil -v -run TestMockServer
# Expected: PASS (3 tests)
```

### 0.3 Create Config Test Helpers (10m)

**Test-First Workflow**:

1. **Write helper tests** (4m)

   - [ ] Add to `internal/testutil/testutil_test.go`
   - [ ] Write `TestCreateTestConfig` - verify YAML config creation with defaults
   - [ ] Write `TestWithConfigFile` - verify test runs with custom config, cleanup
   - [ ] Run: `go test ./internal/testutil -run TestCreate` → FAILS

2. **Implement config helpers** (5m)

   - [ ] Add to `internal/testutil/testutil.go`
   - [ ] Implement `CreateTestConfig(serverURL, email string) string`
     - Generate YAML config with defaults
     - Use `CreateTempConfig` internally
     - Return file path
   - [ ] Implement `WithConfigFile(t *testing.T, content string) string`
     - Create temp config file
     - Set `EMERGENT_CONFIG` env var
     - Register cleanup
     - Return file path
   - [ ] Run: `go test ./internal/testutil -run TestCreate` → PASSES

3. **Verify and commit** (1m)
   - [ ] Run: `go test ./...` → All pass
   - [ ] Git commit: "feat(testutil): add config test helpers"

**Deliverables**:

- Functions: `CreateTestConfig()`, `WithConfigFile()`
- Tests: 2 additional tests
- Integration: Uses existing utilities

**Verification**:

```bash
go test ./internal/testutil -v
# Expected: PASS (11 tests total), coverage: ≥80%
```

## Phase 1: Config Management (4-5 days)

**Purpose**: Establish configuration file handling, environment variable support, and credential storage. Foundation for all subsequent phases.

---

### 1.1 Initialize Project Structure (30m)

**Test-First Workflow**:

1. **Write structure validation test** (10m)

   - [ ] Create `tools/emergent-cli/init_test.go`
   - [ ] Write `TestProjectStructure` that checks:
     - `go.mod` exists with correct module path
     - Required directories exist: `cmd/`, `internal/`, `internal/testutil/`
     - `.gitignore` includes Go binary patterns
   - [ ] Run: `go test ./...` → FAILS (directories don't exist)

2. **Create project structure** (15m)

   - [ ] Create `tools/emergent-cli/` directory
   - [ ] Initialize: `go mod init github.com/emergent-company/emergent/tools/emergent-cli`
   - [ ] Create directories:
     - `cmd/` (entry point)
     - `internal/config/`
     - `internal/auth/`
     - `internal/api/`
     - `internal/cmd/`
     - `internal/output/`
     - `internal/testutil/` (already exists from Phase 0)
   - [ ] Create `.gitignore`:

     ```
     # Binaries
     emergent-cli
     emergent-cli.exe
     dist/

     # Test files
     *.test
     coverage.out

     # IDE
     .vscode/
     .idea/
     ```

   - [ ] Run: `go test ./...` → PASSES

3. **Verify and commit** (5m)
   - [ ] Run: `go mod tidy`
   - [ ] Run: `go build ./cmd` → (will fail, cmd not created yet - expected)
   - [ ] Git commit: "chore(cli): initialize project structure"

**Deliverables**:

- Files: `go.mod`, `.gitignore`, `init_test.go`
- Directories: Complete internal structure
- Tests: 1 structure validation test

**Verification**:

```bash
go test ./... -v
# Expected: PASS (12 tests), all from testutil + structure test
```

---

### 1.2 Add Core Dependencies (30m)

**Test-First Workflow**:

1. **Write dependency import test** (10m)

   - [ ] Create `tools/emergent-cli/deps_test.go`
   - [ ] Write `TestDependencyImports` that attempts:
     - Import `"github.com/spf13/cobra"`
     - Import `"github.com/spf13/viper"`
     - Import `"github.com/go-resty/resty/v2"`
     - Import `"github.com/olekukonko/tablewriter"`
     - Import `"gopkg.in/yaml.v3"`
   - [ ] Run: `go test ./...` → FAILS (packages not found)

2. **Install dependencies** (15m)

   - [ ] Run: `go get github.com/spf13/cobra@latest`
   - [ ] Run: `go get github.com/spf13/viper@latest`
   - [ ] Run: `go get github.com/go-resty/resty/v2@latest`
   - [ ] Run: `go get github.com/olekukonko/tablewriter@latest`
   - [ ] Run: `go get gopkg.in/yaml.v3@latest`
   - [ ] Run: `go mod tidy`
   - [ ] Run: `go test ./...` → PASSES

3. **Verify and commit** (5m)
   - [ ] Check `go.mod` for all dependencies
   - [ ] Run: `go mod verify`
   - [ ] Git commit: "feat(cli): add core dependencies"

**Deliverables**:

- Files: `deps_test.go`, updated `go.mod`, `go.sum`
- Dependencies: 5 core libraries installed
- Tests: 1 import validation test

**Verification**:

```bash
go test ./... -v
# Expected: PASS (13 tests)
```

---

### 1.3 Create Config Package Structure (1h)

**Test-First Workflow**:

1. **Write config tests** (25m)

   - [ ] Create `internal/config/config_test.go`
   - [ ] Write `TestConfigStruct` - verify struct fields (ServerURL, Email, etc.)
   - [ ] Write `TestConfigLoad_File` - load from YAML file
   - [ ] Write `TestConfigLoad_Defaults` - apply defaults when no file exists
   - [ ] Write `TestConfigSave` - persist to YAML file with 0644 perms
   - [ ] Run: `go test ./internal/config` → FAILS (package doesn't exist)

2. **Implement config package** (30m)

   - [ ] Create `internal/config/config.go`
   - [ ] Define `Config` struct:
     ```go
     type Config struct {
         ServerURL string `mapstructure:"server_url" yaml:"server_url"`
         Email     string `mapstructure:"email" yaml:"email"`
         OrgID     string `mapstructure:"org_id" yaml:"org_id"`
         ProjectID string `mapstructure:"project_id" yaml:"project_id"`
         Debug     bool   `mapstructure:"debug" yaml:"debug"`
     }
     ```
   - [ ] Implement `Load() (*Config, error)` - read from file or return defaults
   - [ ] Implement `Save(cfg *Config, path string) error` - write to YAML with 0644
   - [ ] Implement `defaults() *Config` - return default configuration
   - [ ] Run: `go test ./internal/config` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...` → All tests pass
   - [ ] Git commit: "feat(config): add config package with load/save"

**Deliverables**:

- Files: `config.go`, `config_test.go`
- Functions: `Load()`, `Save()`, `defaults()`
- Tests: 4 unit tests passing
- Coverage: ≥80% for config package

**Verification**:

```bash
go test ./internal/config -v -cover
# Expected: PASS (4 tests), coverage: ≥80%
```

---

### 1.4 Implement Config File Discovery (45m)

**Test-First Workflow**:

1. **Write discovery tests** (20m)

   - [ ] Add to `internal/config/config_test.go`
   - [ ] Write `TestDiscoverPath_FlagProvided` - check `--config` flag first
   - [ ] Write `TestDiscoverPath_EnvVar` - check `EMERGENT_CONFIG` env var second
   - [ ] Write `TestDiscoverPath_Default` - fallback to `~/.emergent/config.yaml`
   - [ ] Write `TestDiscoverPath_Precedence` - verify flag > env > default
   - [ ] Run: `go test ./internal/config` → FAILS (function doesn't exist)

2. **Implement discovery** (20m)

   - [ ] Add to `internal/config/config.go`
   - [ ] Implement `DiscoverPath(flagPath string) string`:
     - Check if `flagPath` non-empty and file exists → return it
     - Check `EMERGENT_CONFIG` env var and file exists → return it
     - Check `~/.emergent/config.yaml` exists → return it
     - Return `~/.emergent/config.yaml` (default path for creation)
   - [ ] Update `Load()` to use `DiscoverPath("")` internally
   - [ ] Run: `go test ./internal/config` → PASSES

3. **Verify and commit** (5m)
   - [ ] Run: `go test ./...` → All pass
   - [ ] Git commit: "feat(config): add config file discovery logic"

**Deliverables**:

- Functions: `DiscoverPath()`
- Tests: 4 additional tests (8 total for config package)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/config -v -run TestDiscover
# Expected: PASS (4 tests)
```

---

### 1.5 Implement Environment Variable Support (30m)

**Test-First Workflow**:

1. **Write env var tests** (12m)

   - [ ] Add to `internal/config/config_test.go`
   - [ ] Write `TestLoadFromEnv_ServerURL` - `EMERGENT_SERVER_URL` overrides file
   - [ ] Write `TestLoadFromEnv_Email` - `EMERGENT_EMAIL` overrides file
   - [ ] Write `TestLoadFromEnv_Precedence` - env > file > defaults
   - [ ] Run: `go test ./internal/config -run TestLoadFromEnv` → FAILS

2. **Implement env var binding** (15m)

   - [ ] Update `Load()` in `internal/config/config.go`:
     - Use Viper to auto-bind env vars with `EMERGENT_` prefix
     - Set up env var replacer: `_` in config keys → `_` in env vars
     - Precedence: env vars override file values, file overrides defaults
   - [ ] Update `Config` struct with `mapstructure` tags
   - [ ] Run: `go test ./internal/config -run TestLoadFromEnv` → PASSES

3. **Verify and commit** (3m)
   - [ ] Run: `go test ./...`
   - [ ] Git commit: "feat(config): add environment variable support"

**Deliverables**:

- Functions: Updated `Load()` with Viper env binding
- Tests: 3 additional tests (11 total)
- Coverage: ≥85%

**Verification**:

```bash
EMERGENT_SERVER_URL=http://test go test ./internal/config -v -run TestLoadFromEnv
# Expected: PASS (3 tests)
```

---

### 1.6 Implement Credentials Storage (1h)

**Test-First Workflow**:

1. **Write credentials tests** (25m)

   - [ ] Create `internal/auth/credentials_test.go`
   - [ ] Write `TestCredentialsLoad` - read from file
   - [ ] Write `TestCredentialsSave` - write with 0600 permissions
   - [ ] Write `TestCredentialsPermissionCheck` - warn if file is 0644 or 0666
   - [ ] Write `TestCredentialsEncryption` - tokens stored securely (if encryption added)
   - [ ] Run: `go test ./internal/auth` → FAILS (package doesn't exist)

2. **Implement credentials package** (30m)

   - [ ] Create `internal/auth/credentials.go`
   - [ ] Define `Credentials` struct:
     ```go
     type Credentials struct {
         AccessToken  string    `json:"access_token"`
         RefreshToken string    `json:"refresh_token"`
         ExpiresAt    time.Time `json:"expires_at"`
     }
     ```
   - [ ] Implement `Load(path string) (*Credentials, error)`:
     - Check file permissions (warn if > 0600)
     - Read JSON from file
     - Parse into Credentials struct
   - [ ] Implement `Save(creds *Credentials, path string) error`:
     - Marshal to JSON
     - Write with 0600 permissions (owner read/write only)
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify and commit** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Git commit: "feat(auth): add credentials storage with secure permissions"

**Deliverables**:

- Files: `credentials.go`, `credentials_test.go`
- Functions: `Load()`, `Save()`
- Tests: 4 unit tests
- Coverage: ≥75%

**Verification**:

```bash
go test ./internal/auth -v -cover
# Expected: PASS (4 tests), coverage: ≥75%
```

---

### 1.7 Create Root Command (45m)

**Test-First Workflow**:

1. **Write root command tests** (20m)

   - [ ] Create `internal/cmd/root_test.go`
   - [ ] Write `TestRootCommand_Flags` - verify global flags registered
   - [ ] Write `TestRootCommand_Execution` - command runs without error
   - [ ] Write `TestRootCommand_ViperBinding` - flags bind to Viper config
   - [ ] Run: `go test ./internal/cmd` → FAILS (package doesn't exist)

2. **Implement root command** (20m)

   - [ ] Create `internal/cmd/root.go`
   - [ ] Define root command using Cobra:
     ```go
     var rootCmd = &cobra.Command{
         Use:   "emergent-cli",
         Short: "CLI tool for Emergent platform",
         Long:  "Command-line interface for interacting with the Emergent knowledge base API",
     }
     ```
   - [ ] Add global flags:
     - `--server` (string): Server URL
     - `--output` (string): Output format (table, json, yaml)
     - `--debug` (bool): Enable debug logging
     - `--no-color` (bool): Disable colored output
   - [ ] Bind flags to Viper for config integration
   - [ ] Implement `Execute() error` function
   - [ ] Run: `go test ./internal/cmd` → PASSES

3. **Verify and commit** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Git commit: "feat(cmd): add root command with global flags"

**Deliverables**:

- Files: `root.go`, `root_test.go`
- Commands: Root command with 4 global flags
- Tests: 3 unit tests
- Coverage: ≥70%

**Verification**:

```bash
go test ./internal/cmd -v
# Expected: PASS (3 tests)
```

---

### 1.8 Create Config Commands (1.5h)

**Test-First Workflow**:

1. **Write config command tests** (40m)

   - [ ] Create `internal/cmd/config_test.go`
   - [ ] Write `TestConfigSetServer` - sets server URL in config
   - [ ] Write `TestConfigSetCredentials` - sets email in config
   - [ ] Write `TestConfigShow` - displays current configuration
   - [ ] Write `TestConfigLogout` - clears credentials file
   - [ ] Run: `go test ./internal/cmd -run TestConfig` → FAILS

2. **Implement config commands** (40m)

   - [ ] Create `internal/cmd/config.go`
   - [ ] Implement `configCmd` (parent command)
   - [ ] Implement `configSetServerCmd`:
     - Load current config
     - Update `ServerURL`
     - Save config
     - Output: "Server URL updated to: {url}"
   - [ ] Implement `configSetCredentialsCmd`:
     - Load current config
     - Update `Email`
     - Save config
     - Output: "Email set to: {email}"
   - [ ] Implement `configShowCmd`:
     - Load config
     - Display all settings in table format
   - [ ] Implement `configLogoutCmd`:
     - Delete credentials file
     - Output: "Logged out successfully"
   - [ ] Register subcommands to `configCmd`
   - [ ] Register `configCmd` to root in `root.go`
   - [ ] Run: `go test ./internal/cmd -run TestConfig` → PASSES

3. **Verify and commit** (10m)
   - [ ] Run: `go test ./...`
   - [ ] Test manually: `go run ./cmd config show`
   - [ ] Git commit: "feat(cmd): add config commands (set-server, set-credentials, show, logout)"

**Deliverables**:

- Files: `config.go`, `config_test.go`
- Commands: `config set-server`, `config set-credentials`, `config show`, `config logout`
- Tests: 4 command tests
- Coverage: ≥75%

**Verification**:

```bash
go test ./internal/cmd -v -run TestConfig
# Expected: PASS (4 tests)

# Manual smoke test
go run ./cmd config show
# Expected: Table output with current config
```

---

### 1.9 Create Main Entry Point (30m)

**Test-First Workflow**:

1. **Write main integration test** (12m)

   - [ ] Create `cmd/main_test.go`
   - [ ] Write `TestMainExecution` - verify main runs without panic
   - [ ] Write `TestMainHelp` - verify `--help` flag works
   - [ ] Run: `go test ./cmd` → FAILS (main.go doesn't exist)

2. **Implement main** (15m)

   - [ ] Create `cmd/main.go`:

     ```go
     package main

     import (
         "github.com/emergent-company/emergent/tools/emergent-cli/internal/cmd"
         "os"
     )

     func main() {
         if err := cmd.Execute(); err != nil {
             os.Exit(1)
         }
     }
     ```

   - [ ] Run: `go test ./cmd` → PASSES
   - [ ] Run: `go build -o emergent-cli ./cmd` → SUCCESS

3. **Verify and commit** (3m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `./emergent-cli --help` → Shows help
   - [ ] Run: `./emergent-cli config show` → Shows config
   - [ ] Git commit: "feat(cmd): add main entry point"

**Deliverables**:

- Files: `cmd/main.go`, `cmd/main_test.go`
- Binary: `emergent-cli` executable
- Tests: 2 integration tests
- Build: Successful compilation

**Verification**:

```bash
go build -o emergent-cli ./cmd
./emergent-cli --help
# Expected: Help text with available commands

./emergent-cli config show
# Expected: Current configuration displayed
```

---

## Phase 1 Summary

**Total Time Estimate**: 4-5 days (assuming 6-8 hour work days)

**Breakdown**:

- 1.1: Project Structure - 30m
- 1.2: Dependencies - 30m
- 1.3: Config Package - 1h
- 1.4: File Discovery - 45m
- 1.5: Environment Variables - 30m
- 1.6: Credentials Storage - 1h
- 1.7: Root Command - 45m
- 1.8: Config Commands - 1.5h
- 1.9: Main Entry Point - 30m

**Total**: ~6.5 hours

**Deliverables at Phase 1 Completion**:

- Working CLI binary (`emergent-cli`)
- Configuration management (file + env vars)
- Credentials storage (secure file permissions)
- Commands: `config set-server`, `config set-credentials`, `config show`, `config logout`
- Test coverage: ~80% overall
- Foundation ready for Phase 2 (Authentication)

**Next Phase Preview**: Phase 2 will implement OAuth Device Flow authentication, token caching, and automatic refresh.

## Phase 2: Configuration Layer

- [ ] 2.1 Implement config package

  - [ ] Create `internal/config/config.go`
  - [ ] Define Config struct with Viper binding
  - [ ] Implement `Load()` function (file + env)
  - [ ] Implement `Save()` function
  - [ ] Handle config file creation with proper permissions (0644)

- [ ] 2.2 Implement credential storage

  - [ ] Create `internal/auth/credentials.go`
  - [ ] Define Credentials struct (email, password, token_cache)
  - [ ] Implement `Load()` with permission check (0600)
  - [ ] Implement `Save()` with secure permissions
  - [ ] Add permission warning on insecure files

- [ ] 2.3 Add environment variable support
  - [ ] `EMERGENT_SERVER_URL`
  - [ ] `EMERGENT_EMAIL`
  - [ ] `EMERGENT_PASSWORD`
  - [ ] `EMERGENT_ORG_ID`
  - [ ] `EMERGENT_PROJECT_ID`

## Phase 2: Authentication (OAuth Device Flow) - 4-6 days

**Goal**: Implement browser-based OAuth Device Flow authentication via Zitadel

**Prerequisites**: Phase 1 complete (config system working)

**Deliverables**: `emergent-cli login`, `emergent-cli logout`, `emergent-cli auth status` commands

### 2.1 OIDC Discovery & Configuration (45 minutes)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Create test file: `internal/auth/discovery_test.go`
   - [ ] Write test function: `TestDiscoverOIDC()`
   - [ ] Test assertions:
     - Valid issuer URL returns discovery document
     - Invalid issuer URL returns error
     - Discovery document contains required endpoints (device_authorization_endpoint, token_endpoint, userinfo_endpoint)
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Create implementation: `internal/auth/discovery.go`
   - [ ] Implement `DiscoverOIDC(issuerURL string) (*OIDCConfig, error)`
   - [ ] Define `OIDCConfig` struct with fields:
     - `Issuer string`
     - `DeviceAuthorizationEndpoint string`
     - `TokenEndpoint string`
     - `UserinfoEndpoint string`
   - [ ] Fetch `.well-known/openid-configuration`
   - [ ] Parse JSON response
   - [ ] Validate required fields are present
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): add OIDC discovery"

**Deliverables**:

- Files: `discovery.go`, `discovery_test.go`
- Functions: `DiscoverOIDC(issuerURL string) (*OIDCConfig, error)`
- Structs: `OIDCConfig`
- Tests: 3 unit tests passing
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/auth -v
# Expected: PASS (3 tests)
```

### 2.2 Device Code Request (1 hour)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Create test file: `internal/auth/device_flow_test.go`
   - [ ] Write test function: `TestRequestDeviceCode()`
   - [ ] Test assertions:
     - Valid request returns device code response
     - Response contains: device_code, user_code, verification_uri, verification_uri_complete, interval, expires_in
     - Invalid client ID returns error
   - [ ] Use mock HTTP server from testutil
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Create implementation: `internal/auth/device_flow.go`
   - [ ] Implement `RequestDeviceCode(config *OIDCConfig, clientID string, scopes []string) (*DeviceCodeResponse, error)`
   - [ ] Define `DeviceCodeResponse` struct:
     - `DeviceCode string`
     - `UserCode string`
     - `VerificationURI string`
     - `VerificationURIComplete string`
     - `Interval int`
     - `ExpiresIn int`
   - [ ] POST to device_authorization_endpoint
   - [ ] Form-encode request body: client_id, scope
   - [ ] Parse JSON response
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement device code request"

**Deliverables**:

- Files: `device_flow.go`, `device_flow_test.go`
- Functions: `RequestDeviceCode(config, clientID, scopes) (*DeviceCodeResponse, error)`
- Structs: `DeviceCodeResponse`
- Tests: 2 unit tests passing
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/auth -v -run TestRequestDeviceCode
# Expected: PASS (2 tests)
```

### 2.3 Browser Launch Integration (45 minutes)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add test to: `internal/auth/device_flow_test.go`
   - [ ] Write test function: `TestOpenBrowser()`
   - [ ] Test assertions:
     - Valid URL opens browser (mock detection)
     - Invalid URL returns error
     - Browser launch failure returns informative error
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Add to: `internal/auth/device_flow.go`
   - [ ] Implement `OpenBrowser(url string) error`
   - [ ] Use `github.com/pkg/browser` package
   - [ ] Detect OS (macOS, Linux, Windows)
   - [ ] Handle launch failures gracefully
   - [ ] Return error with manual instructions if browser fails
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): add browser launch integration"

**Deliverables**:

- Files: `device_flow.go` (updated), `device_flow_test.go` (updated)
- Functions: `OpenBrowser(url string) error`
- Tests: 3 unit tests passing
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/auth -v -run TestOpenBrowser
# Expected: PASS (3 tests)
```

### 2.4 Token Polling Loop (1 hour 30 minutes)

**Test-First Workflow**:

1. **Write failing test** (35m)

   - [ ] Add to: `internal/auth/device_flow_test.go`
   - [ ] Write test function: `TestPollForToken()`
   - [ ] Test scenarios:
     - Pending status → keep polling
     - Slow_down response → increase interval
     - Authorization_denied → return error
     - Expired_token → return timeout error
     - Success → return token response
   - [ ] Use mock HTTP server with state tracking
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] Add to: `internal/auth/device_flow.go`
   - [ ] Implement `PollForToken(config *OIDCConfig, deviceCode string, interval, expiresIn int) (*TokenResponse, error)`
   - [ ] Define `TokenResponse` struct:
     - `AccessToken string`
     - `RefreshToken string`
     - `ExpiresIn int`
     - `TokenType string`
   - [ ] Implement polling loop with interval respect
   - [ ] Handle error codes: authorization_pending, slow_down, access_denied, expired_token
   - [ ] Timeout after expiresIn seconds
   - [ ] POST to token_endpoint
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement token polling loop"

**Deliverables**:

- Files: `device_flow.go` (updated), `device_flow_test.go` (updated)
- Functions: `PollForToken(config, deviceCode, interval, expiresIn) (*TokenResponse, error)`
- Structs: `TokenResponse`
- Tests: 5 unit tests passing (all error scenarios)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/auth -v -run TestPollForToken
# Expected: PASS (5 tests - pending, slow_down, denied, expired, success)
```

### 2.5 Credentials Storage Structure (1 hour)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Create test file: `internal/auth/credentials_test.go`
   - [ ] Write test function: `TestSaveCredentials()`
   - [ ] Test assertions:
     - Credentials saved to correct path (use temp dir)
     - File has 0600 permissions
     - JSON format is valid
     - Required fields present
   - [ ] Write test function: `TestLoadCredentials()`
   - [ ] Test assertions:
     - Valid credentials file loads successfully
     - Missing file returns error
     - Invalid JSON returns error
     - Insecure permissions (0644) logs warning
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Create implementation: `internal/auth/credentials.go`
   - [ ] Define `Credentials` struct:
     - `AccessToken string`
     - `RefreshToken string`
     - `ExpiresAt time.Time`
     - `UserEmail string`
     - `IssuerURL string`
   - [ ] Implement `SaveCredentials(creds *Credentials) error`
     - Create directory with 0700 permissions
     - Write JSON with 0600 permissions
     - Use atomic write (temp file + rename)
   - [ ] Implement `LoadCredentials() (*Credentials, error)`
     - Check file exists
     - Verify file permissions (warn if insecure)
     - Parse JSON
     - Return error if invalid
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement credentials storage"

**Deliverables**:

- Files: `credentials.go`, `credentials_test.go`
- Functions: `SaveCredentials(creds) error`, `LoadCredentials() (*Credentials, error)`
- Structs: `Credentials`
- Tests: 6 unit tests passing
- Coverage: ≥85%
- Security: 0600 file permissions, 0700 directory permissions

**Verification**:

```bash
go test ./internal/auth -v -run "TestSave|TestLoad"
# Expected: PASS (6 tests)
```

### 2.6 Token Expiry Check (45 minutes)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to: `internal/auth/credentials_test.go`
   - [ ] Write test function: `TestIsExpired()`
   - [ ] Test scenarios:
     - Token expired → true
     - Token expires in 10 minutes → true (5-minute buffer)
     - Token expires in 30 minutes → false
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Add to: `internal/auth/credentials.go`
   - [ ] Implement `(c *Credentials) IsExpired() bool`
   - [ ] Check if `time.Now().Add(5 * time.Minute).After(c.ExpiresAt)`
   - [ ] Return true if expired or about to expire (5-minute buffer)
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): add token expiry check with buffer"

**Deliverables**:

- Files: `credentials.go` (updated), `credentials_test.go` (updated)
- Methods: `(c *Credentials) IsExpired() bool`
- Tests: 3 unit tests passing
- Buffer: 5 minutes before actual expiry
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/auth -v -run TestIsExpired
# Expected: PASS (3 tests)
```

### 2.7 Token Refresh Implementation (1 hour 15 minutes)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Add to: `internal/auth/device_flow_test.go`
   - [ ] Write test function: `TestRefreshToken()`
   - [ ] Test scenarios:
     - Valid refresh token → new access token
     - Invalid refresh token → error
     - Network error → error with retry guidance
   - [ ] Use mock HTTP server
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Add to: `internal/auth/device_flow.go`
   - [ ] Implement `RefreshToken(config *OIDCConfig, refreshToken string) (*TokenResponse, error)`
   - [ ] POST to token_endpoint
   - [ ] Form-encode request: grant_type=refresh_token, refresh_token={token}
   - [ ] Parse response
   - [ ] Update credentials atomically:
     - Save to temp file
     - Rename to credentials file (atomic operation)
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement token refresh"

**Deliverables**:

- Files: `device_flow.go` (updated), `device_flow_test.go` (updated)
- Functions: `RefreshToken(config, refreshToken) (*TokenResponse, error)`
- Tests: 3 unit tests passing
- Atomicity: Uses temp file + rename pattern
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/auth -v -run TestRefreshToken
# Expected: PASS (3 tests)
```

### 2.8 User Info Fetching (45 minutes)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to: `internal/auth/device_flow_test.go`
   - [ ] Write test function: `TestGetUserInfo()`
   - [ ] Test assertions:
     - Valid access token returns user info
     - Invalid token returns 401 error
     - Response contains email, name, sub
   - [ ] Use mock HTTP server
   - [ ] Run: `go test ./internal/auth` → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Add to: `internal/auth/device_flow.go`
   - [ ] Implement `GetUserInfo(config *OIDCConfig, accessToken string) (*UserInfo, error)`
   - [ ] Define `UserInfo` struct:
     - `Sub string`
     - `Email string`
     - `Name string`
   - [ ] GET to userinfo_endpoint
   - [ ] Authorization header: Bearer {accessToken}
   - [ ] Parse JSON response
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement user info fetching"

**Deliverables**:

- Files: `device_flow.go` (updated), `device_flow_test.go` (updated)
- Functions: `GetUserInfo(config, accessToken) (*UserInfo, error)`
- Structs: `UserInfo`
- Tests: 2 unit tests passing
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/auth -v -run TestGetUserInfo
# Expected: PASS (2 tests)
```

### 2.9 Login Command Implementation (1 hour)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Create test file: `internal/cmd/auth/login_test.go`
   - [ ] Write test function: `TestLoginCommand()`
   - [ ] Test flow:
     - Command runs without error
     - Output includes verification instructions
     - Browser opens (mock check)
     - Credentials saved after successful auth
   - [ ] Mock all HTTP calls using testutil
   - [ ] Run: `go test ./internal/cmd/auth` → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Create implementation: `internal/cmd/auth/login.go`
   - [ ] Implement `RunLogin(cmd *cobra.Command, args []string) error`
   - [ ] Flow:
     1. Discover OIDC endpoints
     2. Request device code
     3. Display verification instructions
     4. Open browser
     5. Poll for token
     6. Fetch user info
     7. Save credentials
     8. Print success message
   - [ ] Handle all error cases with user-friendly messages
   - [ ] Add spinner for "Waiting for authorization..."
   - [ ] Run: `go test ./internal/cmd/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement login command"

**Deliverables**:

- Files: `login.go`, `login_test.go`
- Functions: `RunLogin(cmd, args) error`
- Tests: 1 integration test passing
- UX: Progress spinner, clear instructions
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/cmd/auth -v -run TestLoginCommand
# Expected: PASS (1 test, full flow)
```

### 2.10 Logout Command Implementation (30 minutes)

**Test-First Workflow**:

1. **Write failing test** (12m)

   - [ ] Add to: `internal/cmd/auth/logout_test.go`
   - [ ] Write test function: `TestLogoutCommand()`
   - [ ] Test assertions:
     - Credentials file deleted
     - Command succeeds even if no credentials exist
     - Confirmation message printed
   - [ ] Run: `go test ./internal/cmd/auth` → FAILS (expected)

2. **Implement minimal** (15m)

   - [ ] Create implementation: `internal/cmd/auth/logout.go`
   - [ ] Implement `RunLogout(cmd *cobra.Command, args []string) error`
   - [ ] Delete credentials file
   - [ ] Print "Successfully logged out" message
   - [ ] Handle case where no credentials exist (not an error)
   - [ ] Run: `go test ./internal/cmd/auth` → PASSES

3. **Verify full suite** (3m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement logout command"

**Deliverables**:

- Files: `logout.go`, `logout_test.go`
- Functions: `RunLogout(cmd, args) error`
- Tests: 2 unit tests passing
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/cmd/auth -v -run TestLogoutCommand
# Expected: PASS (2 tests)
```

### 2.11 Auth Status Command Implementation (45 minutes)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to: `internal/cmd/auth/status_test.go`
   - [ ] Write test function: `TestStatusCommand()`
   - [ ] Test scenarios:
     - Logged in, token valid → show user info
     - Logged in, token expired → show expiry warning
     - Not logged in → show "not authenticated" message
   - [ ] Run: `go test ./internal/cmd/auth` → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Create implementation: `internal/cmd/auth/status.go`
   - [ ] Implement `RunStatus(cmd *cobra.Command, args []string) error`
   - [ ] Load credentials
   - [ ] If no credentials: print "Not authenticated. Run 'emergent-cli login'"
   - [ ] If credentials exist:
     - Check expiry
     - Print user email
     - Print issuer URL
     - Print expiry status (valid / expires soon / expired)
   - [ ] Run: `go test ./internal/cmd/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): implement auth status command"

**Deliverables**:

- Files: `status.go`, `status_test.go`
- Functions: `RunStatus(cmd, args) error`
- Tests: 3 unit tests passing
- Output: User-friendly status messages
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/cmd/auth -v -run TestStatusCommand
# Expected: PASS (3 tests)
```

### 2.12 Command Registration & Wiring (30 minutes)

**Test-First Workflow**:

1. **Write integration test** (12m)

   - [ ] Create test file: `cmd/main_test.go`
   - [ ] Write test function: `TestAuthCommands()`
   - [ ] Test that commands are registered:
     - `emergent-cli auth login` → exists
     - `emergent-cli auth logout` → exists
     - `emergent-cli auth status` → exists
   - [ ] Run: `go test ./cmd` → FAILS (expected)

2. **Implement command registration** (15m)

   - [ ] Update: `internal/cmd/root.go`
   - [ ] Add auth commands to root:
     - Create `authCmd` with subcommands
     - Register `loginCmd`, `logoutCmd`, `statusCmd`
   - [ ] Wire up RunE functions:
     - `loginCmd.RunE = auth.RunLogin`
     - `logoutCmd.RunE = auth.RunLogout`
     - `statusCmd.RunE = auth.RunStatus`
   - [ ] Run: `go test ./cmd` → PASSES

3. **Verify full suite** (3m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Manual test: `./emergent-cli auth --help`
   - [ ] Git commit: "feat(auth): wire up auth commands to root"

**Deliverables**:

- Files: `root.go` (updated), `main_test.go`
- Commands: `auth login`, `auth logout`, `auth status`
- Tests: 1 integration test passing
- Manual verification: Help text shows commands

**Verification**:

```bash
go test ./cmd -v
# Expected: PASS (1 test)

./emergent-cli auth --help
# Expected: Shows login, logout, status subcommands
```

### 2.13 Error Handling & User Messages (1 hour)

**Test-First Workflow**:

1. **Write error scenario tests** (30m)

   - [ ] Add to various test files
   - [ ] Test network errors (timeout, DNS, connection refused):
     - OIDC discovery fails
     - Device code request fails
     - Token polling fails
   - [ ] Test OAuth errors:
     - invalid_client
     - invalid_grant
     - unauthorized_client
   - [ ] Test permission errors:
     - Can't create credentials directory
     - Can't write credentials file
   - [ ] Run: `go test ./internal/auth` → FAILS (not all errors handled)

2. **Implement error handling** (25m)

   - [ ] Add to relevant files in `internal/auth/`
   - [ ] Wrap errors with context using `fmt.Errorf`
   - [ ] Add user-friendly error messages:
     - "Failed to connect to auth server. Check your network connection."
     - "Authentication failed. Please try again or check your permissions."
     - "Unable to save credentials. Check directory permissions."
   - [ ] Include actionable guidance in error messages
   - [ ] Run: `go test ./internal/auth` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./...`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): add comprehensive error handling"

**Deliverables**:

- Files: Multiple files updated with error handling
- Error scenarios: 9 test cases passing
- UX: User-friendly error messages with guidance
- Coverage: Error paths covered

**Verification**:

```bash
go test ./internal/auth -v -run Error
# Expected: PASS (9 error scenario tests)
```

### 2.14 Integration Test for Full Login Flow (1 hour)

**Test-First Workflow**:

1. **Write end-to-end test** (30m)

   - [ ] Create test file: `internal/auth/integration_test.go`
   - [ ] Write test function: `TestFullLoginFlow()`
   - [ ] Test complete flow:
     1. OIDC discovery
     2. Device code request
     3. Token polling (mock success after 2 attempts)
     4. User info fetch
     5. Credentials save
     6. Credentials load
     7. Token expiry check
   - [ ] Use mock HTTP server for all endpoints
   - [ ] Simulate realistic timing (intervals, etc.)
   - [ ] Run: `go test ./internal/auth -tags=integration` → FAILS (expected)

2. **Fix integration issues** (25m)

   - [ ] Debug any integration problems
   - [ ] Ensure all components work together
   - [ ] Fix timing issues, race conditions
   - [ ] Run: `go test ./internal/auth -tags=integration` → PASSES

3. **Verify full suite** (5m)
   - [ ] Run: `go test ./... -tags=integration`
   - [ ] Run: `go build ./cmd`
   - [ ] Git commit: "feat(auth): add end-to-end integration test"

**Deliverables**:

- Files: `integration_test.go`
- Tests: 1 comprehensive integration test passing
- Build tags: `-tags=integration` for CI separation
- Coverage: Full flow exercised

**Verification**:

```bash
go test ./internal/auth -v -tags=integration
# Expected: PASS (1 full flow test)
```

### 2.15 Documentation & Examples (30 minutes)

**Test-First Workflow**:

1. **Create package documentation** (15m)

   - [ ] Add godoc comments to `internal/auth/` package
   - [ ] Document all public functions
   - [ ] Add usage examples in godoc
   - [ ] Document error scenarios

2. **Create examples directory** (10m)

   - [ ] Create `examples/auth/`
   - [ ] Add `examples/auth/login.go` - basic login example
   - [ ] Add `examples/auth/refresh.go` - token refresh example
   - [ ] Ensure examples compile: `go build ./examples/...`

3. **Verify documentation** (5m)
   - [ ] Run: `go doc internal/auth`
   - [ ] Verify all public APIs documented
   - [ ] Git commit: "docs(auth): add package documentation and examples"

**Deliverables**:

- Documentation: Godoc comments on all public APIs
- Examples: 2 working examples
- Build: Examples compile successfully
- Quality: Clear, concise documentation

**Verification**:

```bash
go doc internal/auth | grep -c "^func"
# Expected: ≥8 documented functions

go build ./examples/...
# Expected: Successful build
```

---

**Phase 2 Summary**:

- **Total tasks**: 15
- **Estimated time**: ~12-15 hours (1.5-2 days with testing)
- **Files created**: ~12 (6 implementation + 6 test)
- **Functions**: ~20
- **Commands**: 3 (login, logout, status)
- **Test coverage**: ≥80% on all packages
- **Pattern**: Strict test-first workflow on every task

## Phase 3: API Client Foundation

### 3.1 Base HTTP Client Structure (45m)

**Test-First Workflow**:

1. **Write failing test** (15m)

   - [ ] Create test file: internal/api/client_test.go
   - [ ] Write test function: TestNewClient()
   - [ ] Test assertions: verify Client{BaseURL, HTTPClient, Credentials} fields populated
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Create implementation: internal/api/client.go
   - [ ] Implement type Client struct with BaseURL, HTTPClient, Credentials fields
   - [ ] Implement NewClient(config *Config) (*Client, error)
   - [ ] Set 30s default timeout on http.Client
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add base HTTP client struct"

**Deliverables**:

- Files: client.go, client_test.go
- Functions: NewClient(config *Config) (*Client, error)
- Tests: 2 unit tests passing (NewClient success + config validation)
- Coverage: ≥80%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (2 tests)
```

---

### 3.2 Request Header Construction (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to client_test.go: TestAddHeaders()
   - [ ] Test assertions: Authorization header with Bearer token, X-Org-ID header, X-Project-ID header, User-Agent header
   - [ ] Mock Credentials with test AccessToken
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Add to client.go: func (c *Client) addHeaders(req *http.Request, orgID, projectID string)
   - [ ] Set Authorization: fmt.Sprintf("Bearer %s", c.Credentials.AccessToken)
   - [ ] Set X-Org-ID: orgID
   - [ ] Set X-Project-ID: projectID
   - [ ] Set User-Agent: "emergent-cli/1.0.0"
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add standard request header construction"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: addHeaders(req \*http.Request, orgID, projectID string)
- Tests: 4 unit tests passing (all header types verified)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (6 tests total)
```

---

### 3.3 JSON Response Parsing (45m)

**Test-First Workflow**:

1. **Write failing test** (15m)

   - [ ] Add to client_test.go: TestParseJSONResponse()
   - [ ] Test assertions: successful JSON decode into struct, handles empty body, handles invalid JSON
   - [ ] Use httptest.NewServer with mock responses
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Add to client.go: func parseJSONResponse(resp \*http.Response, target interface{}) error
   - [ ] Check Content-Type header contains "application/json"
   - [ ] Use json.NewDecoder(resp.Body).Decode(&target)
   - [ ] Defer resp.Body.Close() with error check
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add JSON response parsing"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: parseJSONResponse(resp \*http.Response, target interface{}) error
- Tests: 3 unit tests passing (success decode, empty body, invalid JSON)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (9 tests total)
```

---

### 3.4 Custom Error Types (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Create test file: internal/api/errors_test.go
   - [ ] Write test function: TestErrorTypes()
   - [ ] Test assertions: AuthError, ValidationError, NotFoundError, FileTooLargeError all implement error interface
   - [ ] Verify error messages contain expected fields
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Create implementation: internal/api/errors.go
   - [ ] Implement type AuthError struct { Message, Code string }
   - [ ] Implement type ValidationError struct { Message, Code, Details string }
   - [ ] Implement type NotFoundError struct { Message string }
   - [ ] Implement type FileTooLargeError struct { Message string }
   - [ ] Implement Error() methods for each type
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add custom error types"

**Deliverables**:

- Files: errors.go, errors_test.go
- Functions: 4 error types with Error() methods
- Tests: 5 unit tests passing (type assertions + message formatting)
- Coverage: ≥90%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (14 tests total)
```

---

### 3.5 HTTP Status Code Mapping (1h 15m)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Add to errors_test.go: TestHandleErrorResponse()
   - [ ] Test assertions: 401→AuthError, 400→ValidationError, 404→NotFoundError, 413→FileTooLargeError, 500→generic error
   - [ ] Mock HTTP responses with different status codes
   - [ ] Verify error type assertions work
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Add to errors.go: func handleErrorResponse(resp \*http.Response) error
   - [ ] Parse JSON error response: {error: {code, message, details}}
   - [ ] Switch on resp.StatusCode
   - [ ] Map 401→AuthError, 400→ValidationError, 404→NotFoundError, 413→FileTooLargeError
   - [ ] Default case returns generic formatted error
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add HTTP status code error mapping"

**Deliverables**:

- Files: errors.go (updated), errors_test.go (updated)
- Functions: handleErrorResponse(resp \*http.Response) error
- Tests: 6 unit tests passing (all status code mappings + fallback)
- Coverage: ≥90%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (20 tests total)
```

---

### 3.6 Exponential Backoff Implementation (1h 30m)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Create test file: internal/api/retry_test.go
   - [ ] Write test function: TestExponentialBackoff()
   - [ ] Test assertions: verify delay sequence 1s, 2s, 4s
   - [ ] Use time.Since to measure actual delays
   - [ ] Test with mock time if possible, otherwise accept timing variance
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] Create implementation: internal/api/retry.go
   - [ ] Implement func exponentialBackoff(attempt int) time.Duration
   - [ ] Calculate delay: time.Duration(1<<uint(attempt)) \* time.Second
   - [ ] Cap at 4s (max attempt 2)
   - [ ] Add jitter: ±10% random variation
   - [ ] Implement helper: func shouldRetry(statusCode int) bool (5xx + network errors)
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add exponential backoff calculation"

**Deliverables**:

- Files: retry.go, retry_test.go
- Functions: exponentialBackoff(attempt int) time.Duration, shouldRetry(statusCode int) bool
- Tests: 4 unit tests passing (delay sequence, cap enforcement, jitter bounds, retry decision)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v -timeout 10s
# Expected: PASS (24 tests total)
```

---

### 3.7 Retry Loop with Max Attempts (1h 15m)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Add to retry_test.go: TestDoRequestWithRetry()
   - [ ] Test assertions: succeeds on 1st try, retries on 5xx, stops at max 3 attempts, no retry on 4xx
   - [ ] Use httptest.NewServer with configurable failure counts
   - [ ] Track attempt counts in test
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Add to client.go: func (c *Client) doRequestWithRetry(req *http.Request) (\*http.Response, error)
   - [ ] Implement retry loop: for attempt := 0; attempt < 3; attempt++
   - [ ] Call c.HTTPClient.Do(req)
   - [ ] Check shouldRetry(statusCode)
   - [ ] If retry needed: call time.Sleep(exponentialBackoff(attempt))
   - [ ] Return response on success or non-retryable error
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add retry loop with exponential backoff"

**Deliverables**:

- Files: client.go (updated), retry_test.go (updated)
- Functions: doRequestWithRetry(req *http.Request) (*http.Response, error)
- Tests: 4 unit tests passing (immediate success, retry + success, max attempts, no retry 4xx)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v -timeout 15s
# Expected: PASS (28 tests total)
```

---

### 3.8 Token Refresh on 401 (1h 15m)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Add to client_test.go: TestTokenRefreshOn401()
   - [ ] Test assertions: 401 triggers refresh, retries with new token, saves updated credentials, fails after refresh error
   - [ ] Mock auth.RefreshToken() and auth.SaveCredentials()
   - [ ] Use httptest with 401 then 200 response sequence
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Update doRequestWithRetry in client.go
   - [ ] Check if resp.StatusCode == 401
   - [ ] Call auth.RefreshToken(c.Credentials.RefreshToken)
   - [ ] Update c.Credentials = newCreds
   - [ ] Call auth.SaveCredentials(newCreds)
   - [ ] Update Authorization header on req
   - [ ] Retry request once
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add token refresh on 401"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: doRequestWithRetry updated to handle 401 special case
- Tests: 3 unit tests passing (refresh + retry, refresh failure, refresh + success)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (31 tests total)
```

---

### 3.9 Context and Timeout Management (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to client_test.go: TestContextAndTimeout()
   - [ ] Test assertions: respects context cancellation, respects timeout, propagates context through request
   - [ ] Use context.WithTimeout and context.WithCancel
   - [ ] Verify request fails on context cancel/timeout
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Update doRequestWithRetry signature: func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (\*http.Response, error)
   - [ ] Create req = req.Clone(ctx) before each retry
   - [ ] Check ctx.Err() before each retry attempt
   - [ ] Return ctx.Err() if context cancelled/timed out
   - [ ] Update all callers to pass context
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add context and timeout management"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: doRequestWithRetry signature updated with context.Context parameter
- Tests: 3 unit tests passing (context cancel, timeout, propagation)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v -timeout 5s
# Expected: PASS (34 tests total)
```

---

### 3.10 Multipart Form Builder (1h 30m)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Create test file: internal/api/multipart_test.go
   - [ ] Write test function: TestBuildMultipartUpload()
   - [ ] Test assertions: creates multipart writer, adds file part, adds metadata fields, closes writer, returns correct Content-Type
   - [ ] Mock file with bytes.Buffer
   - [ ] Verify generated form can be parsed by multipart.Reader
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] Create implementation: internal/api/multipart.go
   - [ ] Implement func buildMultipartUpload(file io.Reader, filename string, metadata map[string]string) (\*bytes.Buffer, string, error)
   - [ ] Create bytes.Buffer and multipart.Writer
   - [ ] Call writer.CreateFormFile("file", filename)
   - [ ] Call io.Copy(part, file)
   - [ ] For each metadata field call writer.WriteField(key, value)
   - [ ] Call writer.Close()
   - [ ] Return buffer, writer.FormDataContentType(), nil
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add multipart form builder for uploads"

**Deliverables**:

- Files: multipart.go, multipart_test.go
- Functions: buildMultipartUpload(file io.Reader, filename string, metadata map[string]string) (\*bytes.Buffer, string, error)
- Tests: 4 unit tests passing (basic upload, metadata fields, empty file, Content-Type)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (38 tests total)
```

---

### 3.11 Query Parameter Builder (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to client_test.go: TestBuildQueryParams()
   - [ ] Test assertions: encodes string params, handles arrays (multiple values), skips empty/nil values, URL escapes special characters
   - [ ] Test with filters: {status: "active", tags: ["tag1", "tag2"], limit: "10"}
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Add to client.go: func buildQueryParams(filters map[string]interface{}) string
   - [ ] Create url.Values
   - [ ] Iterate filters, handle string → single value
   - [ ] Handle []string → multiple values with same key
   - [ ] Handle int/bool → convert to string
   - [ ] Skip nil or empty string values
   - [ ] Return params.Encode()
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add query parameter builder"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: buildQueryParams(filters map[string]interface{}) string
- Tests: 4 unit tests passing (strings, arrays, types, escaping)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (42 tests total)
```

---

### 3.12 HTTP GET Request Helper (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to client_test.go: TestDoGet()
   - [ ] Test assertions: constructs URL with query params, calls addHeaders, uses doRequestWithRetry, parses JSON response
   - [ ] Use httptest.NewServer with mock response
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Add to client.go: func (c \*Client) DoGet(ctx context.Context, path string, orgID, projectID string, filters map[string]interface{}, target interface{}) error
   - [ ] Build full URL: c.BaseURL + path + "?" + buildQueryParams(filters)
   - [ ] Create req: http.NewRequestWithContext(ctx, "GET", url, nil)
   - [ ] Call c.addHeaders(req, orgID, projectID)
   - [ ] Call resp, err := c.doRequestWithRetry(ctx, req)
   - [ ] Call parseJSONResponse(resp, target)
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add HTTP GET request helper"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: DoGet(ctx context.Context, path string, orgID, projectID string, filters map[string]interface{}, target interface{}) error
- Tests: 3 unit tests passing (basic GET, with filters, error handling)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (45 tests total)
```

---

### 3.13 HTTP POST Request Helper (1h 15m)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Add to client_test.go: TestDoPost()
   - [ ] Test assertions: constructs URL, marshals body to JSON, calls addHeaders, uses doRequestWithRetry, parses JSON response
   - [ ] Test with struct payload
   - [ ] Use httptest.NewServer
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Add to client.go: func (c \*Client) DoPost(ctx context.Context, path string, orgID, projectID string, body interface{}, target interface{}) error
   - [ ] Marshal body: jsonData, err := json.Marshal(body)
   - [ ] Create req: http.NewRequestWithContext(ctx, "POST", c.BaseURL+path, bytes.NewReader(jsonData))
   - [ ] Set Content-Type: req.Header.Set("Content-Type", "application/json")
   - [ ] Call c.addHeaders(req, orgID, projectID)
   - [ ] Call resp, err := c.doRequestWithRetry(ctx, req)
   - [ ] Check if resp.StatusCode == 201 || 200
   - [ ] Call parseJSONResponse(resp, target)
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add HTTP POST request helper"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: DoPost(ctx context.Context, path string, orgID, projectID string, body interface{}, target interface{}) error
- Tests: 3 unit tests passing (basic POST, with body, error handling)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (48 tests total)
```

---

### 3.14 HTTP DELETE Request Helper (1h)

**Test-First Workflow**:

1. **Write failing test** (20m)

   - [ ] Add to client_test.go: TestDoDelete()
   - [ ] Test assertions: constructs URL, calls addHeaders, uses doRequestWithRetry, checks 204 status code
   - [ ] Use httptest.NewServer returning 204
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (30m)

   - [ ] Add to client.go: func (c \*Client) DoDelete(ctx context.Context, path string, orgID, projectID string) error
   - [ ] Create req: http.NewRequestWithContext(ctx, "DELETE", c.BaseURL+path, nil)
   - [ ] Call c.addHeaders(req, orgID, projectID)
   - [ ] Call resp, err := c.doRequestWithRetry(ctx, req)
   - [ ] Check resp.StatusCode == 204 || 200
   - [ ] Return handleErrorResponse(resp) if not success
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add HTTP DELETE request helper"

**Deliverables**:

- Files: client.go (updated), client_test.go (updated)
- Functions: DoDelete(ctx context.Context, path string, orgID, projectID string) error
- Tests: 2 unit tests passing (successful delete, error handling)
- Coverage: ≥85%

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (50 tests total)
```

---

### 3.15 Mock HTTP Server Test Pattern (1h 15m)

**Test-First Workflow**:

1. **Write failing test** (25m)

   - [ ] Create test file: internal/api/mock_server_test.go
   - [ ] Write test function: TestMockServerPattern()
   - [ ] Test assertions: createMockServer() returns httptest.Server, setupMockCredentials() returns valid Credentials, createTestClient() integrates both
   - [ ] Test complete request flow with all components
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (40m)

   - [ ] Implement func createMockServer(handler http.HandlerFunc) \*httptest.Server
   - [ ] Implement func setupMockCredentials() \*auth.Credentials
   - [ ] Implement func createTestClient(serverURL string, creds *auth.Credentials) *Client
   - [ ] Add helper: func newTestContext() (context.Context, context.CancelFunc) with 5s timeout
   - [ ] Document usage pattern in comments
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add mock HTTP server test pattern"

**Deliverables**:

- Files: mock_server_test.go
- Functions: createMockServer, setupMockCredentials, createTestClient, newTestContext
- Tests: 3 unit tests passing (server creation, credentials setup, client creation)
- Coverage: Test helpers (not counted in coverage but critical for test quality)

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (53 tests total)
```

---

### 3.16 Error Scenario Test Suite (1h 30m)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Create test file: internal/api/error_scenarios_test.go
   - [ ] Write test functions: TestNetworkTimeout, TestInvalidJSON, TestMalformedURL, TestNilClient, TestEmptyResponse
   - [ ] Test assertions: each scenario produces expected error type and message
   - [ ] Use httptest with deliberate failure modes
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] Review error handling in client.go for all identified scenarios
   - [ ] Add validation: NewClient checks for nil config
   - [ ] Add validation: request methods check for empty required parameters
   - [ ] Ensure network timeout returns descriptive error
   - [ ] Ensure invalid JSON parsing returns descriptive error
   - [ ] Ensure all error paths tested
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add comprehensive error scenario tests"

**Deliverables**:

- Files: error_scenarios_test.go, client.go (validation updates)
- Functions: Enhanced validation in existing functions
- Tests: 8 unit tests passing (all error scenarios covered)
- Coverage: ≥90%

**Verification**:

```bash
go test ./internal/api -v -timeout 10s
# Expected: PASS (61 tests total)
```

---

### 3.17 Retry Logic Test Suite (1h 30m)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Create test file: internal/api/retry_scenarios_test.go
   - [ ] Write test functions: Test5xxRetrySuccess, Test5xxMaxAttemptsExhausted, Test4xxNoRetry, TestNetworkErrorRetry, TestTimeoutNoRetry
   - [ ] Test assertions: verify exact retry attempts, verify final error type, measure total elapsed time
   - [ ] Use httptest with configurable failure sequences
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] Review doRequestWithRetry logic for all scenarios
   - [ ] Ensure network errors trigger retry
   - [ ] Ensure context timeout prevents retry
   - [ ] Ensure 4xx errors skip retry
   - [ ] Ensure 5xx errors retry up to max attempts
   - [ ] Add logging for retry attempts (debug level)
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add comprehensive retry logic tests"

**Deliverables**:

- Files: retry_scenarios_test.go, client.go (retry logic verified)
- Functions: Verified correctness of doRequestWithRetry, exponentialBackoff, shouldRetry
- Tests: 7 unit tests passing (all retry scenarios + timing validation)
- Coverage: ≥90%

**Verification**:

```bash
go test ./internal/api -v -timeout 15s
# Expected: PASS (68 tests total)
```

---

### 3.18 Integration Test for Full Request Flow (1h 30m)

**Test-First Workflow**:

1. **Write failing test** (30m)

   - [ ] Create test file: internal/api/integration_test.go
   - [ ] Write test function: TestCompleteRequestFlow()
   - [ ] Test assertions: complete flow from NewClient → DoGet/DoPost/DoDelete → response parsing
   - [ ] Mock complete server responses including headers
   - [ ] Verify all components work together
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (50m)

   - [ ] No new implementation needed if previous tasks complete
   - [ ] Fix any integration issues discovered
   - [ ] Ensure all components wire together correctly
   - [ ] Verify error propagation through full stack
   - [ ] Verify context cancellation works end-to-end
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Git commit: "feat(api): add integration tests for complete request flow"

**Deliverables**:

- Files: integration_test.go
- Functions: No new functions, validates existing integration
- Tests: 4 unit tests passing (GET flow, POST flow, DELETE flow, error flow)
- Coverage: ≥85% (integration tests exercise all components)

**Verification**:

```bash
go test ./internal/api -v -timeout 20s
# Expected: PASS (72 tests total)
```

---

### 3.19 Documentation and Examples (45m)

**Test-First Workflow**:

1. **Write failing test** (15m)

   - [ ] Create test file: internal/api/doc_test.go
   - [ ] Write example functions: ExampleClient_DoGet, ExampleClient_DoPost, ExampleClient_DoDelete
   - [ ] Use testable examples with // Output: comments
   - [ ] Run: go test ./internal/api → FAILS (expected)

2. **Implement minimal** (20m)

   - [ ] Add package-level documentation to client.go
   - [ ] Add GoDoc comments to all exported functions
   - [ ] Add usage examples in comments
   - [ ] Create README.md in internal/api/ with:
     - Quick start guide
     - Request patterns (GET, POST, DELETE)
     - Error handling guide
     - Retry behavior explanation
     - Context and timeout usage
   - [ ] Run: go test ./internal/api → PASSES

3. **Verify full suite** (10m)
   - [ ] Run: go test ./...
   - [ ] Run: go build ./cmd/emergent-cli
   - [ ] Run: go doc -all ./internal/api
   - [ ] Git commit: "docs(api): add comprehensive documentation and examples"

**Deliverables**:

- Files: doc_test.go, README.md, updated comments in all .go files
- Functions: 3 testable examples
- Tests: 3 example tests passing (validated by go test)
- Coverage: Documentation completeness (all exported symbols documented)

**Verification**:

```bash
go test ./internal/api -v
# Expected: PASS (75 tests total, 3 examples)
go doc -all ./internal/api | wc -l
# Expected: >200 lines of documentation
```

---

**Phase 3 Summary**:

- **Total tasks**: 19
- **Estimated time**: ~18-20 hours (2.5-3 days with testing)
- **Files created**: ~12 (9 implementation + 3 test)
- **Functions**: ~25 (client operations + helpers)
- **Test coverage**: ≥85% on all packages
- **Pattern**: Strict test-first workflow on every task

**Dependency Order**: Tasks 3.1-3.5 (foundation) → 3.6-3.9 (retry/resilience) → 3.10-3.14 (request helpers) → 3.15-3.19 (testing/documentation)

## Phase 5: Output Formatters

**Goals**: Implement flexible output formatting system supporting JSON (pretty/compact), Table (manual, no tablewriter), and YAML (simple stdlib generator). Zero external dependencies except standard library extensions. Format selection via `--format` flag with auto-detection (TTY → table, pipe → JSON).

**Total Estimated Time**: ~7.75 hours (range: 6-8 hours)

**Technical Decisions**:

- **No `tablewriter` library**: Manual column calculation + box-drawing characters (ASCII fallback)
- **No `gopkg.in/yaml.v3`**: Simple stdlib YAML generator with documented limitations (flat/simple structures only)
- **ANSI Colors**: Direct escape codes, respect `NO_COLOR` env, TTY detection
- **Terminal Width**: `golang.org/x/term` (allowed as stdlib extension)
- **Format Priority**: `--format` flag → auto-detection (isatty check) → default (table for TTY, JSON for pipe)

**File Structure**:

```
internal/output/
├── colors.go          # ANSI escape codes, NO_COLOR support
├── colors_test.go
├── terminal.go        # Width detection, text truncation
├── terminal_test.go
├── json.go            # JSON formatter (pretty + compact)
├── json_test.go
├── table.go           # Table formatter (manual, no tablewriter)
├── table_test.go
├── yaml.go            # Simple YAML generator (stdlib only)
├── yaml_test.go
├── formatter.go       # Format selection & auto-detection
└── formatter_test.go
```

---

### 5.1 Base Infrastructure & Color Support

**Estimated Time**: 1 hour

**Goal**: Establish foundation for terminal color support with NO_COLOR env respect and TTY detection.

#### Test-First Workflow

1. **Write tests** in `internal/output/colors_test.go`:

   ```bash
   go test ./internal/output -run TestColorEnabled -v
   go test ./internal/output -run TestColorize -v
   go test ./internal/output -run TestStripColors -v
   ```

2. **Implement** in `internal/output/colors.go`:

   - `ColorEnabled() bool` - checks TTY + NO_COLOR env
   - `Colorize(text, color string) string` - applies ANSI codes if enabled
   - `StripColors(text string) string` - removes ANSI escape codes
   - ANSI escape code constants (Red, Green, Yellow, Blue, Bold, Reset)

3. **Verify**:
   ```bash
   go test ./internal/output -run Color -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **TTY Detection**: `ColorEnabled()` returns true when stdout is TTY, false otherwise
2. **NO_COLOR Env**: `ColorEnabled()` returns false when `NO_COLOR=1` regardless of TTY
3. **Colorize Behavior**: `Colorize("test", Red)` wraps with ANSI codes when enabled, returns plain text otherwise
4. **Strip Colors**: `StripColors("\x1b[31mRed\x1b[0m")` returns "Red"

#### Deliverables

- [ ] File: `internal/output/colors.go` (~60 LOC)
  - [ ] Function: `ColorEnabled() bool`
  - [ ] Function: `Colorize(text, color string) string`
  - [ ] Function: `StripColors(text string) string`
  - [ ] Constants: Red, Green, Yellow, Blue, Bold, Reset (ANSI codes)
- [ ] File: `internal/output/colors_test.go` (~100 LOC)
  - [ ] Test: `TestColorEnabled` (TTY + NO_COLOR scenarios)
  - [ ] Test: `TestColorize` (enabled/disabled)
  - [ ] Test: `TestStripColors` (removes ANSI codes)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run Color` passes (≥80% coverage)

---

### 5.2 Terminal Width Detection

**Estimated Time**: 45 minutes

**Goal**: Detect terminal width for table column calculations and text truncation.

#### Test-First Workflow

1. **Write tests** in `internal/output/terminal_test.go`:

   ```bash
   go test ./internal/output -run TestDetectTerminalWidth -v
   go test ./internal/output -run TestTruncateText -v
   ```

2. **Implement** in `internal/output/terminal.go`:

   - `DetectTerminalWidth() int` - uses `golang.org/x/term`, fallback 80
   - `TruncateText(text string, maxWidth int) string` - UTF-8 aware, adds "..."

3. **Verify**:
   ```bash
   go test ./internal/output -run Terminal -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Width Detection**: `DetectTerminalWidth()` returns positive integer (default 80 if not TTY)
2. **Short Text**: `TruncateText("hello", 10)` returns "hello" (no truncation)
3. **Long Text**: `TruncateText("very long text here", 10)` returns "very lo..." (UTF-8 safe)
4. **UTF-8 Handling**: `TruncateText("日本語テキスト", 5)` correctly counts characters

#### Deliverables

- [ ] File: `internal/output/terminal.go` (~40 LOC)
  - [ ] Function: `DetectTerminalWidth() int` (uses `golang.org/x/term`)
  - [ ] Function: `TruncateText(text string, maxWidth int) string`
- [ ] File: `internal/output/terminal_test.go` (~60 LOC)
  - [ ] Test: `TestDetectTerminalWidth` (default 80 + real detection)
  - [ ] Test: `TestTruncateText` (short/long/UTF-8)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run Terminal` passes (≥80% coverage)

---

### 5.3 JSON Formatter

**Estimated Time**: 1 hour

**Goal**: Implement JSON output with pretty-print and compact modes.

#### Test-First Workflow

1. **Write tests** in `internal/output/json_test.go`:

   ```bash
   go test ./internal/output -run TestFormatJSON -v
   ```

2. **Implement** in `internal/output/json.go`:

   - `FormatJSON(data interface{}, compact bool) (string, error)` - uses `encoding/json`
   - Pretty mode: `json.MarshalIndent(data, "", "  ")`
   - Compact mode: `json.Marshal(data)`

3. **Verify**:
   ```bash
   go test ./internal/output -run JSON -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Pretty Mode**: `FormatJSON(map, false)` returns indented JSON (2 spaces)
2. **Compact Mode**: `FormatJSON(map, true)` returns single-line JSON
3. **Error Handling**: `FormatJSON(unsupportedType, false)` returns error with context
4. **UTF-8 Content**: Correctly handles Unicode characters

#### Deliverables

- [ ] File: `internal/output/json.go` (~30 LOC)
  - [ ] Function: `FormatJSON(data interface{}, compact bool) (string, error)`
- [ ] File: `internal/output/json_test.go` (~80 LOC)
  - [ ] Test: `TestFormatJSON_Pretty` (indented output)
  - [ ] Test: `TestFormatJSON_Compact` (single line)
  - [ ] Test: `TestFormatJSON_Error` (invalid data)
  - [ ] Test: `TestFormatJSON_UTF8` (Unicode handling)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run JSON` passes (≥80% coverage)

---

### 5.4 Table Column Calculation

**Estimated Time**: 1 hour 15 minutes

**Goal**: Manual column width calculation and text alignment (no tablewriter dependency).

#### Test-First Workflow

1. **Write tests** in `internal/output/table_test.go`:

   ```bash
   go test ./internal/output -run TestCalculateColumnWidths -v
   go test ./internal/output -run TestAlignText -v
   ```

2. **Implement** in `internal/output/table.go`:

   - `CalculateColumnWidths(headers []string, rows [][]string, maxWidth int) []int`
   - `AlignText(text string, width int, align string) string` (left/right/center)
   - Uses `unicode/utf8` for character counting

3. **Verify**:
   ```bash
   go test ./internal/output -run Table -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Balanced Widths**: `CalculateColumnWidths(["Name", "Status"], [["Alice", "Active"]], 80)` distributes space proportionally
2. **Min Width**: Each column gets at least header width + 2 padding
3. **Max Width**: Respects terminal width, truncates if needed
4. **Align Left**: `AlignText("test", 10, "left")` returns "test "
5. **Align Right**: `AlignText("test", 10, "right")` returns " test"
6. **UTF-8 Counting**: Correctly counts multi-byte characters

#### Deliverables

- [ ] File: `internal/output/table.go` (~100 LOC, partial implementation)
  - [ ] Function: `CalculateColumnWidths(headers []string, rows [][]string, maxWidth int) []int`
  - [ ] Function: `AlignText(text string, width int, align string) string`
  - [ ] Helper: `visualWidth(text string) int` (UTF-8 aware)
- [ ] File: `internal/output/table_test.go` (~120 LOC, partial tests)
  - [ ] Test: `TestCalculateColumnWidths` (balanced/min/max)
  - [ ] Test: `TestAlignText` (left/right/center)
  - [ ] Test: `TestVisualWidth` (UTF-8 counting)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run Table` passes (≥80% coverage)

---

### 5.5 Table Rendering

**Estimated Time**: 1 hour 30 minutes

**Goal**: Complete table rendering with box-drawing characters and ASCII fallback.

#### Test-First Workflow

1. **Write tests** in `internal/output/table_test.go` (continued):

   ```bash
   go test ./internal/output -run TestRenderTable -v
   ```

2. **Implement** in `internal/output/table.go` (continued):

   - `RenderTable(headers []string, rows [][]string, options TableOptions) (string, error)`
   - Box-drawing: `─ │ ┌ ┐ └ ┘ ├ ┤` (Unicode mode)
   - ASCII fallback: `- | + + + + + +` (when `NO_UNICODE=1`)
   - TableOptions struct: MaxWidth, NoHeader, NoColors, UseASCII

3. **Verify**:
   ```bash
   go test ./internal/output -run RenderTable -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Box Drawing**: `RenderTable(headers, rows, {})` produces Unicode table with `─ │ ┌`
2. **ASCII Mode**: `RenderTable(headers, rows, {UseASCII: true})` uses `- | +`
3. **Color Headers**: Headers are bold/colored when ColorEnabled() is true
4. **No Header**: `RenderTable(headers, rows, {NoHeader: true})` skips header row
5. **Empty Rows**: `RenderTable(headers, [], {})` renders header + empty message

#### Deliverables

- [ ] File: `internal/output/table.go` (complete, ~200 LOC total)
  - [ ] Function: `RenderTable(headers []string, rows [][]string, options TableOptions) (string, error)`
  - [ ] Struct: `TableOptions` (MaxWidth, NoHeader, NoColors, UseASCII)
  - [ ] Helper: `buildRow(cells []string, widths []int, sep string) string`
  - [ ] Helper: `buildSeparator(widths []int, left, mid, right, horiz string) string`
- [ ] File: `internal/output/table_test.go` (complete, ~250 LOC total)
  - [ ] Test: `TestRenderTable_BoxDrawing` (Unicode characters)
  - [ ] Test: `TestRenderTable_ASCII` (fallback mode)
  - [ ] Test: `TestRenderTable_Colors` (header formatting)
  - [ ] Test: `TestRenderTable_NoHeader` (skip headers)
  - [ ] Test: `TestRenderTable_EmptyRows` (empty message)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run Table` passes (≥80% coverage)

---

### 5.6 Simple YAML Generator

**Estimated Time**: 1 hour 15 minutes

**Goal**: Stdlib-only YAML generator with documented limitations (no `gopkg.in/yaml.v3`).

#### Test-First Workflow

1. **Write tests** in `internal/output/yaml_test.go`:

   ```bash
   go test ./internal/output -run TestFormatYAML -v
   ```

2. **Implement** in `internal/output/yaml.go`:

   - `FormatYAML(data interface{}) (string, error)` - reflection-based
   - Supports: string, int, bool, map[string]interface{}, []interface{} (1-2 levels deep)
   - Limitations: No complex nesting (>2 levels), no custom types, no tags
   - Returns error with recommendation: "For complex YAML, use `yq` or `gopkg.in/yaml.v3`"

3. **Verify**:
   ```bash
   go test ./internal/output -run YAML -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Flat Map**: `FormatYAML(map{"name": "test", "count": 42})` produces `name: test\ncount: 42`
2. **Nested Map (1 level)**: `FormatYAML(map{"user": map{"name": "Alice"}})` produces `user:\n  name: Alice`
3. **Array**: `FormatYAML(map{"items": []string{"a", "b"}})` produces `items:\n  - a\n  - b`
4. **Complex Rejection**: `FormatYAML(deeplyNestedMap)` returns error: "YAML too complex (>2 levels deep)"
5. **Recommend yq**: Error message suggests `emergent-cli documents list --format=json | yq`

#### Deliverables

- [ ] File: `internal/output/yaml.go` (~120 LOC)
  - [ ] Function: `FormatYAML(data interface{}) (string, error)`
  - [ ] Helper: `formatValue(v interface{}, indent int) (string, error)` (recursive, max depth 2)
  - [ ] Helper: `isSimpleType(v interface{}) bool` (string/int/bool check)
  - [ ] Limitation doc comment: Explains 1-2 level depth, recommends yq for complex cases
- [ ] File: `internal/output/yaml_test.go` (~100 LOC)
  - [ ] Test: `TestFormatYAML_Flat` (simple key-value)
  - [ ] Test: `TestFormatYAML_Nested` (1 level)
  - [ ] Test: `TestFormatYAML_Array` (list formatting)
  - [ ] Test: `TestFormatYAML_ComplexError` (depth limit)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run YAML` passes (≥80% coverage)
- [ ] Doc: Limitation comment in code + README section explaining when to use yq

---

### 5.7 Format Selection Integration

**Estimated Time**: 1 hour

**Goal**: Integrate all formatters with `--format` flag and auto-detection.

#### Test-First Workflow

1. **Write tests** in `internal/output/formatter_test.go`:

   ```bash
   go test ./internal/output -run TestFormatOutput -v
   go test ./internal/output -run TestDetectFormat -v
   ```

2. **Implement** in `internal/output/formatter.go`:

   - `FormatOutput(data interface{}, format string, options OutputOptions) (string, error)`
   - `DetectFormat(explicitFormat string) string` - checks `--format` flag, then TTY (table vs JSON)
   - OutputOptions: Compact (JSON), NoHeader (table), NoColors, UseASCII
   - Switch cases: "json", "table", "yaml" → calls respective formatters

3. **Verify**:
   ```bash
   go test ./internal/output -run Format -v
   go build ./internal/output
   ```

#### Test Scenarios

1. **Explicit JSON**: `FormatOutput(data, "json", {})` calls JSON formatter
2. **Explicit Table**: `FormatOutput(data, "table", {})` calls table formatter
3. **Explicit YAML**: `FormatOutput(data, "yaml", {})` calls YAML formatter
4. **Auto TTY**: `DetectFormat("")` returns "table" when stdout is TTY
5. **Auto Pipe**: `DetectFormat("")` returns "json" when stdout is pipe
6. **Invalid Format**: `FormatOutput(data, "xml", {})` returns error: "unsupported format: xml (supported: json, table, yaml)"

#### Deliverables

- [ ] File: `internal/output/formatter.go` (~80 LOC)
  - [ ] Function: `FormatOutput(data interface{}, format string, options OutputOptions) (string, error)`
  - [ ] Function: `DetectFormat(explicitFormat string) string`
  - [ ] Struct: `OutputOptions` (Compact, NoHeader, NoColors, UseASCII)
  - [ ] Constants: ValidFormats (json, table, yaml)
- [ ] File: `internal/output/formatter_test.go` (~150 LOC)
  - [ ] Test: `TestFormatOutput_JSON` (calls JSON formatter)
  - [ ] Test: `TestFormatOutput_Table` (calls table formatter)
  - [ ] Test: `TestFormatOutput_YAML` (calls YAML formatter)
  - [ ] Test: `TestDetectFormat_TTY` (auto-detects table)
  - [ ] Test: `TestDetectFormat_Pipe` (auto-detects JSON)
  - [ ] Test: `TestFormatOutput_InvalidFormat` (error message)
- [ ] Build: `go build ./internal/output` succeeds
- [ ] Tests: `go test ./internal/output -run Format` passes (≥80% coverage)

---

### Phase 5 Verification

After completing all tasks, verify the complete output system:

```bash
# Build entire package
go build ./internal/output

# Run all tests with coverage
go test ./internal/output -coverprofile=coverage.out
go tool cover -html=coverage.out

# Verify coverage ≥80%
go test ./internal/output -cover | grep -E "coverage: [8-9][0-9]|coverage: 100"

# Integration test (manual)
# Create small test program in cmd/test-output/main.go:
# - Test JSON pretty/compact
# - Test table with colors/ASCII
# - Test YAML simple/complex
# - Test format auto-detection
```

**Success Criteria**:

- [ ] All 7 tasks completed (5.1 through 5.7)
- [ ] All tests passing (`go test ./internal/output`)
- [ ] Coverage ≥80% per file
- [ ] Build succeeds (`go build ./internal/output`)
- [ ] Manual integration test shows correct formatting
- [ ] No external dependencies (except `golang.org/x/term`)
- [ ] Documentation complete (function comments + README)

**Dependency Order**: 5.1 (colors) → 5.2 (terminal) → 5.3 (JSON) + 5.4-5.5 (table) + 5.6 (YAML) → 5.7 (integration)

## Phase 6: Commands (Cobra)

**Goals**: Implement all CLI commands using Cobra framework with proper flag handling, Viper config integration, and output formatting. Commands integrate Phase 3 (HTTP client), Phase 4 (config/auth), and Phase 5 (output formatters). Zero business logic in command layer—delegate to API client and services.

**Total Estimated Time**: ~13.5 hours (range: 12-15 hours)

**Technical Decisions**:

- **Cobra Only**: Use `github.com/spf13/cobra` and `github.com/spf13/viper` (allowed dependencies)
- **No Business Logic in Commands**: Commands orchestrate (validate flags → call API → format output)
- **Global Flags**: `--server`, `--output`, `--debug`, `--no-color` available on all commands
- **Output Integration**: Use Phase 5 formatters (JSON/Table/YAML) for all list/get operations
- **Error Handling**: Use `cobra.CheckErr()` for consistent error display
- **Config Integration**: Commands read from Phase 4 config system, respect precedence (flags > env > file)

**File Structure**:

```
internal/cmd/
├── root.go              # Root command + global flags
├── root_test.go
├── config.go            # Config management (set-server, logout, etc.)
├── config_test.go
├── documents.go         # Documents CRUD operations
├── documents_test.go
├── chat.go              # Chat send + history
├── chat_test.go
├── extraction.go        # Extraction job operations
├── extraction_test.go
├── admin.go             # Admin list operations (orgs, projects, users)
├── admin_test.go
├── server.go            # Server health + info
├── server_test.go
├── completion.go        # Shell completion generation
├── completion_test.go
├── template_packs.go    # Template pack operations
├── template_packs_test.go
└── serve.go             # Combined docs + MCP server
    serve_test.go
```

**Integration Points**:

- **Phase 3 (HTTP Client)**: All commands use `api.Client` to make requests
- **Phase 4 (Config/Auth)**: Commands load config, validate credentials before API calls
- **Phase 5 (Output)**: List/get commands use `formatter.Format(data, format)` for output

**Dependency Injection Pattern**:

```go
// Commands accept dependencies for testability
type CommandDeps struct {
    APIClient    *api.Client
    ConfigLoader *config.Loader
    Formatter    *output.Formatter
}

// Tests inject mocks
deps := &CommandDeps{
    APIClient: &mockAPIClient{},
    ConfigLoader: &mockConfigLoader{},
    Formatter: output.NewFormatter(),
}
```

---

### 6.1 Root Command & Global Flags

**Estimated Time**: 1.5 hours

**Goal**: Establish root command with global flags (`--server`, `--output`, `--debug`, `--no-color`) and Viper binding for config hierarchy.

#### Test-First Workflow

1. **Write tests** in `internal/cmd/root_test.go`:

   ```bash
   go test ./internal/cmd -run TestRootCommand -v
   go test ./internal/cmd -run TestGlobalFlags -v
   go test ./internal/cmd -run TestVersionFlag -v
   ```

2. **Implement** in `internal/cmd/root.go`:

   - `NewRootCommand(deps *CommandDeps) *cobra.Command` - creates root with global flags
   - Global flags: `--server`, `--output`, `--debug`, `--no-color` bound to Viper
   - Version flag: `--version` prints version info from build tags
   - Persistent pre-run: validates global config, initializes logger

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Root -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Global Flags Binding**: `rootCmd.ParseFlags(["--server", "https://api.example.com"])` → Viper has correct value
2. **Config Precedence**: Flags override env vars, env vars override config file
3. **Version Output**: `rootCmd.SetArgs(["--version"])` → prints version, build time, commit hash
4. **Debug Logging**: `--debug` flag enables verbose logging in output
5. **Help Text**: `rootCmd.Execute()` with no args shows usage and subcommand list

#### Deliverables

- [ ] File: `internal/cmd/root.go` (~150 LOC)
  - [ ] Function: `NewRootCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Function: `initGlobalFlags(cmd *cobra.Command)`
  - [ ] Function: `validateGlobalConfig(cmd *cobra.Command) error` (persistent pre-run)
  - [ ] Variable: `Version`, `BuildTime`, `CommitHash` (set via ldflags)
- [ ] File: `internal/cmd/root_test.go` (~120 LOC)
  - [ ] Test: `TestRootCommand_Help` (usage display)
  - [ ] Test: `TestGlobalFlags_Viper` (binding verification)
  - [ ] Test: `TestVersionFlag` (version output)
  - [ ] Test: `TestDebugFlag` (logger config)
  - [ ] Test: `TestConfigPrecedence` (flag > env > file)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Root` passes (≥80% coverage)

---

### 6.2 Config Commands

**Estimated Time**: 1.5 hours

**Goal**: Implement config management commands (set-server, set-credentials, set-defaults, show, logout).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/config_test.go`:

   ```bash
   go test ./internal/cmd -run TestConfigSetServer -v
   go test ./internal/cmd -run TestConfigSetCredentials -v
   go test ./internal/cmd -run TestConfigShow -v
   go test ./internal/cmd -run TestConfigLogout -v
   ```

2. **Implement** in `internal/cmd/config.go`:

   - `newConfigCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `setServerCmd()` - `--url` flag, validates URL, writes to config file
   - `setCredentialsCmd()` - `--email` flag, prompts for password (hidden), stores encrypted
   - `setDefaultsCmd()` - `--org`, `--project` flags, validates existence via API
   - `showCmd()` - displays current config (masks sensitive values), uses formatter
   - `logoutCmd()` - clears credentials from config file, confirms action

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Config -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Set Server**: `config set-server --url https://api.example.com` → writes to config file
2. **Set Credentials**: `config set-credentials --email user@example.com` → prompts password, stores encrypted
3. **Set Defaults**: `config set-defaults --org myorg --project myproj` → validates via API, writes config
4. **Show Config**: `config show --format=json` → outputs current config as JSON with masked password
5. **Logout**: `config logout` → clears credentials, confirms with "Logged out successfully"

#### Deliverables

- [ ] File: `internal/cmd/config.go` (~200 LOC)
  - [ ] Function: `newConfigCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `setServerCmd()` (validate URL, write config)
  - [ ] Subcommand: `setCredentialsCmd()` (prompt password, encrypt, store)
  - [ ] Subcommand: `setDefaultsCmd()` (validate org/project, write config)
  - [ ] Subcommand: `showCmd()` (format config output, mask sensitive)
  - [ ] Subcommand: `logoutCmd()` (clear credentials, confirm)
- [ ] File: `internal/cmd/config_test.go` (~180 LOC)
  - [ ] Test: `TestConfigSetServer` (URL validation + write)
  - [ ] Test: `TestConfigSetCredentials` (mock prompt, encryption)
  - [ ] Test: `TestConfigSetDefaults` (API validation)
  - [ ] Test: `TestConfigShow` (formatting + masking)
  - [ ] Test: `TestConfigLogout` (credential clearing)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Config` passes (≥80% coverage)

---

### 6.3 Documents Commands

**Estimated Time**: 2 hours

**Goal**: Implement documents CRUD operations (list, get, create, delete, search).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/documents_test.go`:

   ```bash
   go test ./internal/cmd -run TestDocumentsList -v
   go test ./internal/cmd -run TestDocumentsGet -v
   go test ./internal/cmd -run TestDocumentsCreate -v
   go test ./internal/cmd -run TestDocumentsDelete -v
   go test ./internal/cmd -run TestDocumentsSearch -v
   ```

2. **Implement** in `internal/cmd/documents.go`:

   - `newDocumentsCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `listCmd()` - `--org`, `--project` flags (optional), calls API, formats as table/JSON
   - `getCmd()` - requires `<ID>` arg, calls API, formats output
   - `createCmd()` - `--file` flag (required), reads file, uploads via API
   - `deleteCmd()` - requires `<ID>` arg, `--force` flag (skip confirm), deletes via API
   - `searchCmd()` - requires `<QUERY>` arg, calls search API, formats results

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Documents -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **List Documents**: `documents list --format=table` → calls API, renders table with ID, Name, Status columns
2. **Get Document**: `documents get doc-123 --format=json` → fetches document, outputs JSON
3. **Create Document**: `documents create --file ./test.pdf` → uploads file, prints "Created document {ID}"
4. **Delete Document**: `documents delete doc-123 --force` → skips confirmation, deletes, prints success
5. **Search Documents**: `documents search "machine learning"` → searches, displays results in table
6. **Error Handling**: `documents get invalid-id` → prints "Document not found" error

#### Deliverables

- [ ] File: `internal/cmd/documents.go` (~250 LOC)
  - [ ] Function: `newDocumentsCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `listCmd()` (API call + formatting)
  - [ ] Subcommand: `getCmd()` (ID validation + fetch)
  - [ ] Subcommand: `createCmd()` (file upload)
  - [ ] Subcommand: `deleteCmd()` (confirmation + deletion)
  - [ ] Subcommand: `searchCmd()` (query + results formatting)
- [ ] File: `internal/cmd/documents_test.go` (~220 LOC)
  - [ ] Test: `TestDocumentsList` (mock API, verify table output)
  - [ ] Test: `TestDocumentsGet` (ID arg parsing, JSON output)
  - [ ] Test: `TestDocumentsCreate` (file reading, upload)
  - [ ] Test: `TestDocumentsDelete` (force flag, confirmation)
  - [ ] Test: `TestDocumentsSearch` (query parsing, results)
  - [ ] Test: `TestDocumentsErrors` (404, invalid file)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Documents` passes (≥80% coverage)

---

### 6.4 Chat Commands

**Estimated Time**: 1 hour

**Goal**: Implement chat operations (send message, view history).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/chat_test.go`:

   ```bash
   go test ./internal/cmd -run TestChatSend -v
   go test ./internal/cmd -run TestChatHistory -v
   ```

2. **Implement** in `internal/cmd/chat.go`:

   - `newChatCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `sendCmd()` - requires `<MESSAGE>` arg, calls chat API, prints response
   - `historyCmd()` - `--limit` flag (default 10), fetches conversation history, formats as table

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Chat -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Send Message**: `chat send "What is the purpose of this KB?"` → calls API, prints AI response
2. **History with Limit**: `chat history --limit=5` → fetches last 5 messages, displays in table
3. **History Default**: `chat history` → uses default limit (10), formats output
4. **Streaming Response**: `chat send "long query"` → displays response incrementally (if API supports SSE)
5. **Error Handling**: `chat send ""` → validates non-empty message, prints error

#### Deliverables

- [ ] File: `internal/cmd/chat.go` (~150 LOC)
  - [ ] Function: `newChatCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `sendCmd()` (message validation + API call + response display)
  - [ ] Subcommand: `historyCmd()` (limit flag + fetch + formatting)
- [ ] File: `internal/cmd/chat_test.go` (~120 LOC)
  - [ ] Test: `TestChatSend` (message arg, API call, response)
  - [ ] Test: `TestChatHistory` (limit flag, formatting)
  - [ ] Test: `TestChatHistoryDefault` (default limit)
  - [ ] Test: `TestChatSendEmpty` (validation error)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Chat` passes (≥80% coverage)

---

### 6.5 Extraction Commands

**Estimated Time**: 1.5 hours

**Goal**: Implement extraction job operations (run, status, list-jobs).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/extraction_test.go`:

   ```bash
   go test ./internal/cmd -run TestExtractionRun -v
   go test ./internal/cmd -run TestExtractionStatus -v
   go test ./internal/cmd -run TestExtractionListJobs -v
   ```

2. **Implement** in `internal/cmd/extraction.go`:

   - `newExtractionCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `runCmd()` - requires `<DOCUMENT_ID>` arg, triggers extraction job, prints job ID
   - `statusCmd()` - requires `<JOB_ID>` arg, fetches job status, displays progress
   - `listJobsCmd()` - `--status` flag (pending/running/completed/failed), lists jobs in table

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Extraction -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Run Extraction**: `extraction run doc-123` → triggers job, prints "Job started: job-456"
2. **Check Status**: `extraction status job-456` → fetches status, displays progress (e.g., "Running: 45/100 items")
3. **List All Jobs**: `extraction list-jobs` → lists all jobs in table (ID, Document, Status, Progress)
4. **Filter by Status**: `extraction list-jobs --status=completed` → lists only completed jobs
5. **Error Handling**: `extraction run invalid-doc` → prints "Document not found" error

#### Deliverables

- [ ] File: `internal/cmd/extraction.go` (~180 LOC)
  - [ ] Function: `newExtractionCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `runCmd()` (document ID validation + job trigger)
  - [ ] Subcommand: `statusCmd()` (job ID validation + status fetch + progress display)
  - [ ] Subcommand: `listJobsCmd()` (status filter + table formatting)
- [ ] File: `internal/cmd/extraction_test.go` (~150 LOC)
  - [ ] Test: `TestExtractionRun` (document ID, job creation)
  - [ ] Test: `TestExtractionStatus` (job ID, progress display)
  - [ ] Test: `TestExtractionListJobs` (all jobs, table output)
  - [ ] Test: `TestExtractionListJobsFiltered` (status filter)
  - [ ] Test: `TestExtractionRunInvalidDoc` (error handling)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Extraction` passes (≥80% coverage)

---

### 6.6 Admin Commands

**Estimated Time**: 1.5 hours

**Goal**: Implement admin list operations (orgs, projects, users).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/admin_test.go`:

   ```bash
   go test ./internal/cmd -run TestAdminOrgsList -v
   go test ./internal/cmd -run TestAdminProjectsList -v
   go test ./internal/cmd -run TestAdminUsersList -v
   ```

2. **Implement** in `internal/cmd/admin.go`:

   - `newAdminCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `orgsListCmd()` - lists all orgs, formats as table (ID, Name, Created)
   - `projectsListCmd()` - requires `--org` flag, lists projects in org, formats as table
   - `usersListCmd()` - lists all users, formats as table (ID, Email, Role, Created)

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Admin -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **List Orgs**: `admin orgs list --format=table` → calls API, displays orgs in table
2. **List Projects**: `admin projects list --org myorg` → fetches projects for org, displays in table
3. **List Users**: `admin users list --format=json` → fetches users, outputs JSON array
4. **Projects Missing Org**: `admin projects list` → prints error "Flag --org is required"
5. **Empty Results**: `admin orgs list` → displays "No organizations found"

#### Deliverables

- [ ] File: `internal/cmd/admin.go` (~180 LOC)
  - [ ] Function: `newAdminCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `orgsListCmd()` (fetch orgs + table formatting)
  - [ ] Subcommand: `projectsListCmd()` (org flag required + fetch + format)
  - [ ] Subcommand: `usersListCmd()` (fetch users + table formatting)
- [ ] File: `internal/cmd/admin_test.go` (~150 LOC)
  - [ ] Test: `TestAdminOrgsList` (API call, table output)
  - [ ] Test: `TestAdminProjectsList` (org flag, filtering)
  - [ ] Test: `TestAdminUsersList` (JSON output)
  - [ ] Test: `TestAdminProjectsNoOrg` (required flag error)
  - [ ] Test: `TestAdminOrgsListEmpty` (empty message)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Admin` passes (≥80% coverage)

---

### 6.7 Server Commands

**Estimated Time**: 45 minutes

**Goal**: Implement server status operations (health, info).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/server_test.go`:

   ```bash
   go test ./internal/cmd -run TestServerHealth -v
   go test ./internal/cmd -run TestServerInfo -v
   ```

2. **Implement** in `internal/cmd/server.go`:

   - `newServerCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `healthCmd()` - calls `/health` endpoint, prints "Healthy" or error
   - `infoCmd()` - calls `/info` endpoint, displays server version, uptime, features

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Server -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Health Check**: `server health` → calls API, prints "Server is healthy ✓"
2. **Server Info**: `server info --format=json` → fetches server metadata, outputs JSON
3. **Health Failure**: `server health` (server down) → prints "Server is unhealthy: connection refused"
4. **Info Table**: `server info` → displays server info in table (Version, Uptime, Features)

#### Deliverables

- [ ] File: `internal/cmd/server.go` (~100 LOC)
  - [ ] Function: `newServerCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `healthCmd()` (health check + status display)
  - [ ] Subcommand: `infoCmd()` (info fetch + formatting)
- [ ] File: `internal/cmd/server_test.go` (~80 LOC)
  - [ ] Test: `TestServerHealth` (success case)
  - [ ] Test: `TestServerHealthFailure` (error handling)
  - [ ] Test: `TestServerInfo` (JSON output)
  - [ ] Test: `TestServerInfoTable` (table formatting)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Server` passes (≥80% coverage)

---

### 6.8 Completion Command

**Estimated Time**: 1 hour

**Goal**: Generate shell completion scripts for bash, zsh, fish, powershell.

#### Test-First Workflow

1. **Write tests** in `internal/cmd/completion_test.go`:

   ```bash
   go test ./internal/cmd -run TestCompletionBash -v
   go test ./internal/cmd -run TestCompletionZsh -v
   go test ./internal/cmd -run TestCompletionFish -v
   ```

2. **Implement** in `internal/cmd/completion.go`:

   - `newCompletionCommand(rootCmd *cobra.Command) *cobra.Command` - parent command
   - `bashCmd()` - generates bash completion script via `rootCmd.GenBashCompletion()`
   - `zshCmd()` - generates zsh completion script via `rootCmd.GenZshCompletion()`
   - `fishCmd()` - generates fish completion script via `rootCmd.GenFishCompletion()`
   - `powershellCmd()` - generates powershell completion script via `rootCmd.GenPowerShellCompletion()`
   - Each subcommand includes installation instructions in help text

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Completion -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Bash Completion**: `completion bash` → outputs bash completion script
2. **Zsh Completion**: `completion zsh` → outputs zsh completion script
3. **Fish Completion**: `completion fish` → outputs fish completion script
4. **Powershell Completion**: `completion powershell` → outputs powershell script
5. **Help Text**: `completion --help` → displays installation instructions for each shell

#### Deliverables

- [ ] File: `internal/cmd/completion.go` (~150 LOC)
  - [ ] Function: `newCompletionCommand(rootCmd *cobra.Command) *cobra.Command`
  - [ ] Subcommand: `bashCmd()` (generate bash script + install instructions)
  - [ ] Subcommand: `zshCmd()` (generate zsh script + install instructions)
  - [ ] Subcommand: `fishCmd()` (generate fish script + install instructions)
  - [ ] Subcommand: `powershellCmd()` (generate powershell script + install instructions)
- [ ] File: `internal/cmd/completion_test.go` (~120 LOC)
  - [ ] Test: `TestCompletionBash` (script generation)
  - [ ] Test: `TestCompletionZsh` (script generation)
  - [ ] Test: `TestCompletionFish` (script generation)
  - [ ] Test: `TestCompletionPowershell` (script generation)
  - [ ] Test: `TestCompletionHelp` (instructions display)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Completion` passes (≥80% coverage)

---

### 6.9 Template-Packs Commands

**Estimated Time**: 2.5 hours

**Goal**: Implement template pack operations (list, get, validate, create, installed, install, uninstall, compiled-types).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/template_packs_test.go`:

   ```bash
   go test ./internal/cmd -run TestTemplatePacksList -v
   go test ./internal/cmd -run TestTemplatePacksGet -v
   go test ./internal/cmd -run TestTemplatePacksValidate -v
   go test ./internal/cmd -run TestTemplatePacksCreate -v
   go test ./internal/cmd -run TestTemplatePacksInstall -v
   ```

2. **Implement** in `internal/cmd/template_packs.go`:

   - `newTemplatePacksCommand(deps *CommandDeps) *cobra.Command` - parent command
   - `listCmd()` - lists available packs, formats as table (ID, Name, Version, Types)
   - `getCmd()` - requires `<ID>` arg, fetches pack details, outputs JSON/YAML
   - `validateCmd()` - `--file` flag, reads JSON, validates schema, prints errors
   - `createCmd()` - `--file` flag, reads JSON, creates pack via API
   - `installedCmd()` - lists installed packs for current project
   - `installCmd()` - requires `<ID>` arg, installs pack to project
   - `uninstallCmd()` - requires `<ID>` arg, uninstalls pack from project
   - `compiledTypesCmd()` - fetches merged types for current project, outputs JSON

3. **Verify**:
   ```bash
   go test ./internal/cmd -run TemplatePacks -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **List Packs**: `template-packs list` → displays available packs in table
2. **Get Pack**: `template-packs get pack-123 --format=yaml` → fetches pack, outputs YAML
3. **Validate Local**: `template-packs validate --file ./pack.json` → validates, prints "Valid ✓" or errors
4. **Create Pack**: `template-packs create --file ./pack.json` → uploads, prints "Created pack-456"
5. **List Installed**: `template-packs installed` → shows packs installed in current project
6. **Install Pack**: `template-packs install pack-123` → installs, prints "Installed pack-123 to project"
7. **Uninstall Pack**: `template-packs uninstall pack-123` → removes, confirms
8. **Compiled Types**: `template-packs compiled-types --format=json` → outputs merged schema

#### Deliverables

- [ ] File: `internal/cmd/template_packs.go` (~350 LOC)
  - [ ] Function: `newTemplatePacksCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Subcommand: `listCmd()` (fetch + table formatting)
  - [ ] Subcommand: `getCmd()` (ID validation + fetch + output)
  - [ ] Subcommand: `validateCmd()` (file read + schema validation)
  - [ ] Subcommand: `createCmd()` (file upload)
  - [ ] Subcommand: `installedCmd()` (project context + list)
  - [ ] Subcommand: `installCmd()` (pack ID + installation)
  - [ ] Subcommand: `uninstallCmd()` (pack ID + removal)
  - [ ] Subcommand: `compiledTypesCmd()` (fetch merged types + format)
- [ ] File: `internal/cmd/template_packs_test.go` (~300 LOC)
  - [ ] Test: `TestTemplatePacksList` (API call, table)
  - [ ] Test: `TestTemplatePacksGet` (ID, YAML output)
  - [ ] Test: `TestTemplatePacksValidate` (valid/invalid files)
  - [ ] Test: `TestTemplatePacksCreate` (upload)
  - [ ] Test: `TestTemplatePacksInstalled` (project filtering)
  - [ ] Test: `TestTemplatePacksInstall` (installation)
  - [ ] Test: `TestTemplatePacksUninstall` (removal)
  - [ ] Test: `TestTemplatePacksCompiledTypes` (JSON output)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run TemplatePacks` passes (≥80% coverage)

---

### 6.10 Serve Command

**Estimated Time**: 1.5 hours

**Goal**: Implement combined server mode (documentation server + MCP over stdio/HTTP).

#### Test-First Workflow

1. **Write tests** in `internal/cmd/serve_test.go`:

   ```bash
   go test ./internal/cmd -run TestServeDocs -v
   go test ./internal/cmd -run TestServeMCPStdio -v
   go test ./internal/cmd -run TestServeMCPHTTP -v
   go test ./internal/cmd -run TestServeCombined -v
   ```

2. **Implement** in `internal/cmd/serve.go`:

   - `newServeCommand(deps *CommandDeps) *cobra.Command` - unified serve command
   - Flags: `--docs-port` (default 8080), `--mcp-stdio`, `--mcp-port` (default 3000)
   - Modes: docs-only, MCP-stdio, MCP-HTTP, combined (docs + MCP-HTTP)
   - Signal handling: graceful shutdown on SIGINT/SIGTERM
   - Health endpoints: `/health` for docs server, MCP health check

3. **Verify**:
   ```bash
   go test ./internal/cmd -run Serve -v
   go build ./internal/cmd
   ```

#### Test Scenarios

1. **Docs Only**: `serve --docs-port=9000` → starts docs server on port 9000
2. **MCP Stdio**: `serve --mcp-stdio` → starts MCP server over stdio (no HTTP)
3. **MCP HTTP**: `serve --mcp-port=3001` → starts MCP server on HTTP port 3001
4. **Combined Mode**: `serve --docs-port=8080 --mcp-port=3000` → runs both servers
5. **Graceful Shutdown**: SIGINT during serve → servers shut down cleanly, logs "Shutting down..."

#### Deliverables

- [ ] File: `internal/cmd/serve.go` (~200 LOC)
  - [ ] Function: `newServeCommand(deps *CommandDeps) *cobra.Command`
  - [ ] Function: `runDocsServer(port int) error` (HTTP server for docs)
  - [ ] Function: `runMCPStdio() error` (stdio MCP server)
  - [ ] Function: `runMCPHTTP(port int) error` (HTTP MCP server)
  - [ ] Function: `handleShutdown(servers ...io.Closer)` (graceful shutdown)
- [ ] File: `internal/cmd/serve_test.go` (~180 LOC)
  - [ ] Test: `TestServeDocs` (docs server start)
  - [ ] Test: `TestServeMCPStdio` (stdio server)
  - [ ] Test: `TestServeMCPHTTP` (HTTP server)
  - [ ] Test: `TestServeCombined` (both servers)
  - [ ] Test: `TestServeShutdown` (signal handling)
- [ ] Build: `go build ./internal/cmd` succeeds
- [ ] Tests: `go test ./internal/cmd -run Serve` passes (≥80% coverage)

---

## Phase 6 Summary

**Total Implementation Time**: ~13.5 hours

**Files Created**: 20 files (10 implementation + 10 test files)

**Total Lines of Code**: ~3,200 LOC (~1,700 implementation + ~1,500 tests)

**Coverage Target**: ≥80% for each command group

**Integration Points Verified**:

- Phase 3 HTTP Client: All API calls use `api.Client`
- Phase 4 Config/Auth: All commands load config, validate credentials
- Phase 5 Output: All list/get commands support `--format` flag (json/table/yaml)

**Key Testing Patterns**:

- Dependency injection via `CommandDeps` struct
- Mock API client for isolated tests
- Cobra command execution via `cmd.SetArgs()` and `cmd.Execute()`
- Output capture via `bytes.Buffer` for verification
- Flag parsing tests for all commands

**Command Hierarchy**:

```
emergent-cli
├── config (set-server, set-credentials, set-defaults, show, logout)
├── documents (list, get, create, delete, search)
├── chat (send, history)
├── extraction (run, status, list-jobs)
├── admin (orgs list, projects list, users list)
├── server (health, info)
├── completion (bash, zsh, fish, powershell)
├── template-packs (list, get, validate, create, installed, install, uninstall, compiled-types)
└── serve (docs + MCP modes)
```

**Next Steps After Phase 6**:

- Phase 7: Template Pack API (specialized API client)
- Phase 8: MCP Server Implementation (stdio + HTTP transports)
- Phase 9: Documentation Server (static file serving)
- Phase 10: Integration Testing (end-to-end command flows)

## Phase 7: Template Pack API

**Total Time**: ~3.5 hours

**Goal**: Implement specialized API client for template pack operations and local validation logic.

**Dependencies**:

- Phase 3 HTTP Client: Uses `api.Client` for HTTP requests
- Phase 4 Config: Loads server URL and authentication
- Phase 5 Output: Validation errors formatted as structured output

---

### 7.1 Template-Packs API Client

**Estimated Time**: 2 hours

**Goal**: Create specialized API client for template pack operations (list, get, create, install, uninstall, compiled types).

#### Test-First Workflow

1. **Write tests** in `internal/api/template_packs_test.go`:

   ```bash
   go test ./internal/api -run TestTemplatePacks -v
   go test ./internal/api -run TestTemplatePacksList -v
   go test ./internal/api -run TestTemplatePacksGet -v
   go test ./internal/api -run TestTemplatePacksCreate -v
   go test ./internal/api -run TestTemplatePacksInstall -v
   ```

2. **Implement** in `internal/api/template_packs.go`:

   - `type TemplatePacksAPI struct { client *Client }` - wraps base HTTP client
   - `NewTemplatePacksAPI(client *Client) *TemplatePacksAPI` - constructor
   - `List(ctx context.Context) ([]TemplatePack, error)` - GET /template-packs
   - `Get(ctx context.Context, id string) (*TemplatePack, error)` - GET /template-packs/:id
   - `Create(ctx context.Context, pack *TemplatePack) (*TemplatePack, error)` - POST /template-packs
   - `GetInstalled(ctx context.Context, projectID string) ([]TemplatePack, error)` - GET /template-packs/projects/:id/installed
   - `Install(ctx context.Context, projectID, packID string) error` - POST /template-packs/projects/:id/assign
   - `Uninstall(ctx context.Context, projectID, assignmentID string) error` - DELETE /template-packs/projects/:id/assignments/:id
   - `GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypes, error)` - GET /template-packs/projects/:id/compiled-types

3. **Verify**:
   ```bash
   go test ./internal/api -run TemplatePacks -v
   go build ./internal/api
   ```

#### Test Scenarios

1. **List Packs**: `List()` → returns array of template packs with ID, name, version
2. **Get Pack**: `Get("pack-123")` → returns single pack with full schema details
3. **Get Not Found**: `Get("invalid-id")` → returns ErrNotFound error
4. **Create Pack**: `Create(validPack)` → returns created pack with ID assigned
5. **Create Invalid**: `Create(invalidPack)` → returns validation error from API
6. **List Installed**: `GetInstalled("proj-456")` → returns packs installed for project
7. **Install Pack**: `Install("proj-456", "pack-123")` → returns nil on success
8. **Install Already Installed**: `Install("proj-456", "pack-123")` → returns conflict error
9. **Uninstall Pack**: `Uninstall("proj-456", "assign-789")` → returns nil on success
10. **Get Compiled Types**: `GetCompiledTypes("proj-456")` → returns merged type schemas

#### Deliverables

- [ ] File: `internal/api/template_packs.go` (~250 LOC)
  - [ ] Struct: `TemplatePacksAPI` (wraps \*Client)
  - [ ] Function: `NewTemplatePacksAPI(client *Client) *TemplatePacksAPI`
  - [ ] Method: `List(ctx context.Context) ([]TemplatePack, error)`
  - [ ] Method: `Get(ctx context.Context, id string) (*TemplatePack, error)`
  - [ ] Method: `Create(ctx context.Context, pack *TemplatePack) (*TemplatePack, error)`
  - [ ] Method: `GetInstalled(ctx context.Context, projectID string) ([]TemplatePack, error)`
  - [ ] Method: `Install(ctx context.Context, projectID, packID string) error`
  - [ ] Method: `Uninstall(ctx context.Context, projectID, assignmentID string) error`
  - [ ] Method: `GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypes, error)`
- [ ] File: `internal/api/template_packs_test.go` (~300 LOC)
  - [ ] Test: `TestTemplatePacksList` (success + empty)
  - [ ] Test: `TestTemplatePacksGet` (success + not found)
  - [ ] Test: `TestTemplatePacksCreate` (success + validation error)
  - [ ] Test: `TestTemplatePacksInstalled` (success + empty)
  - [ ] Test: `TestTemplatePacksInstall` (success + conflict)
  - [ ] Test: `TestTemplatePacksUninstall` (success + not found)
  - [ ] Test: `TestTemplatePacksCompiledTypes` (success + merge logic)
- [ ] File: `internal/api/types.go` (add template pack types) (~100 LOC)
  - [ ] Struct: `TemplatePack` (ID, Name, Version, ObjectTypeSchemas, RelationshipTypeSchemas)
  - [ ] Struct: `CompiledTypes` (ObjectTypes, RelationshipTypes merged from all installed packs)
- [ ] Build: `go build ./internal/api` succeeds
- [ ] Tests: `go test ./internal/api -run TemplatePacks` passes (≥80% coverage)

---

### 7.2 Validation Logic

**Estimated Time**: 1.5 hours

**Goal**: Implement client-side validation for template pack schemas before API submission.

#### Test-First Workflow

1. **Write tests** in `internal/validation/template_pack_test.go`:

   ```bash
   go test ./internal/validation -run TestValidateTemplatePack -v
   go test ./internal/validation -run TestValidateObjectTypeSchema -v
   go test ./internal/validation -run TestValidateRelationshipTypeSchema -v
   ```

2. **Implement** in `internal/validation/template_pack.go`:

   - `type ValidationError struct { Path string, Message string }` - structured error
   - `ValidateTemplatePack(pack *TemplatePack) []ValidationError` - main validator
   - `validateObjectTypeSchemas(schemas map[string]ObjectTypeSchema) []ValidationError` - validate object types
   - `validateRelationshipTypeSchemas(schemas map[string]RelationshipTypeSchema) []ValidationError` - validate relationship types
   - `validateVersion(version string) error` - ensure non-empty version string
   - Use stdlib `encoding/json` for schema structure validation (no external validator needed)

3. **Verify**:
   ```bash
   go test ./internal/validation -run TemplatePack -v
   go build ./internal/validation
   ```

#### Test Scenarios

1. **Valid Pack**: `ValidateTemplatePack(validPack)` → returns empty error slice
2. **Missing Name**: `ValidateTemplatePack(noName)` → returns error with path "name"
3. **Empty Version**: `ValidateTemplatePack(emptyVersion)` → returns error with path "version"
4. **Missing ObjectTypeSchemas**: `ValidateTemplatePack(noSchemas)` → returns error with path "object_type_schemas"
5. **Invalid Object Schema**: Missing `type` field → error with path "object_type_schemas.Person.type"
6. **Invalid Object Schema**: Missing `properties` → error with path "object_type_schemas.Person.properties"
7. **Invalid Relationship Schema**: Empty `fromTypes` → error with path "relationship_type_schemas.WORKS_AT.fromTypes"
8. **Invalid Relationship Schema**: Empty `toTypes` → error with path "relationship_type_schemas.WORKS_AT.toTypes"
9. **Multiple Errors**: Pack with 3 validation issues → returns all 3 errors with paths
10. **Structured Output**: Validation errors formatted as JSON → parseable error list

#### Deliverables

- [ ] File: `internal/validation/template_pack.go` (~150 LOC)
  - [ ] Struct: `ValidationError` (Path, Message fields)
  - [ ] Function: `ValidateTemplatePack(pack *TemplatePack) []ValidationError`
  - [ ] Function: `validateObjectTypeSchemas(schemas map[string]ObjectTypeSchema) []ValidationError`
  - [ ] Function: `validateRelationshipTypeSchemas(schemas map[string]RelationshipTypeSchema) []ValidationError`
  - [ ] Function: `validateVersion(version string) error`
- [ ] File: `internal/validation/template_pack_test.go` (~200 LOC)
  - [ ] Test: `TestValidateTemplatePackValid` (all fields correct)
  - [ ] Test: `TestValidateTemplatePackMissingName` (required field check)
  - [ ] Test: `TestValidateTemplatePackMissingVersion` (required field check)
  - [ ] Test: `TestValidateObjectTypeSchemaMissingType` (schema structure)
  - [ ] Test: `TestValidateObjectTypeSchemaMissingProperties` (schema structure)
  - [ ] Test: `TestValidateRelationshipSchemaEmptyFromTypes` (array non-empty check)
  - [ ] Test: `TestValidateRelationshipSchemaEmptyToTypes` (array non-empty check)
  - [ ] Test: `TestValidateMultipleErrors` (returns all errors)
- [ ] Build: `go build ./internal/validation` succeeds
- [ ] Tests: `go test ./internal/validation -run TemplatePack` passes (≥80% coverage)

---

## Phase 7 Summary

**Total Implementation Time**: ~3.5 hours

**Files Created**: 4 files (2 implementation + 2 test files)

**Total Lines of Code**: ~1,000 LOC (~500 implementation + ~500 tests)

**Coverage Target**: ≥80% for both modules

**Integration Points**:

- Phase 3 HTTP Client: `TemplatePacksAPI` wraps `api.Client` for authenticated requests
- Phase 4 Config: API client uses server URL and credentials from config
- Phase 5 Output: Validation errors formatted as structured output (JSON paths)
- Phase 6 Commands: `template-packs` commands use this API + validation

**Key Design Decisions**:

1. **No External Validator**: Use stdlib `encoding/json` for schema validation instead of `github.com/go-playground/validator/v10` to keep dependencies minimal
2. **Structured Errors**: `ValidationError` provides JSON path (e.g., `object_type_schemas.Person.type`) for precise error location
3. **Client-Side Validation**: Validate before API call to provide faster feedback and reduce server load
4. **Context-Aware API**: All API methods accept `context.Context` for cancellation and timeout control

**Validation Rules**:

```
Required Fields:
- name: non-empty string
- version: non-empty string
- object_type_schemas: non-empty map

Object Type Schema:
- type: must be "object"
- properties: must exist (can be empty map)

Relationship Type Schema:
- fromTypes: non-empty array of type names
- toTypes: non-empty array of type names
```

**API Endpoint Mapping**:

| Method             | Endpoint                                            | Returns         |
| ------------------ | --------------------------------------------------- | --------------- |
| List()             | GET /template-packs                                 | []TemplatePack  |
| Get()              | GET /template-packs/:id                             | \*TemplatePack  |
| Create()           | POST /template-packs                                | \*TemplatePack  |
| GetInstalled()     | GET /template-packs/projects/:id/installed          | []TemplatePack  |
| Install()          | POST /template-packs/projects/:id/assign            | error           |
| Uninstall()        | DELETE /template-packs/projects/:id/assignments/:id | error           |
| GetCompiledTypes() | GET /template-packs/projects/:id/compiled-types     | \*CompiledTypes |

**Next Steps After Phase 7**:

- Phase 8: Documentation Server (static file serving + command docs generation)
- Phase 9: MCP Server Implementation (stdio + HTTP transports)
- Phase 10: Integration Testing (end-to-end command flows)

## Phase 8: Documentation Server

**Goal**: Build an embedded HTTP documentation server that generates interactive HTML docs from Cobra commands, supports dark mode, and works in the compiled binary.

**Estimated Time**: 8 hours (2h + 1.5h + 3h + 1h + 0.5h)

---

### Task 8.1: Core Generator Implementation

**Goal**: Extract structured documentation metadata from Cobra command tree

**Estimated Time**: 2 hours

**Test-First Workflow**:

1. **Write tests first** (`cmd/docs/generator_test.go`):

   ```go
   func TestGenerateCommandDocs_TraversesTree(t *testing.T) {
       root := &cobra.Command{Use: "root", Short: "Root command"}
       sub := &cobra.Command{Use: "sub", Short: "Subcommand"}
       root.AddCommand(sub)

       docs := GenerateCommandDocs(root)

       assert.Len(t, docs, 2) // root + sub
       assert.Equal(t, "root", docs[0].Name)
       assert.Len(t, docs[0].Subcommands, 1)
   }
   ```

2. **Implement** (`cmd/docs/generator.go`):

   ```go
   type CommandDoc struct {
       Name        string
       Usage       string
       Short       string
       Long        string
       Flags       []FlagDoc
       Examples    []string
       Subcommands []string
       Parent      string
   }

   func GenerateCommandDocs(root *cobra.Command) []CommandDoc { ... }
   ```

3. **Verify**:
   ```bash
   go test ./cmd/docs/... -v
   go build ./cmd/docs
   ```

**Test Scenarios** (≥5 required):

1. **Tree traversal**: Root with 2 subcommands → generates 3 docs
2. **Flag extraction**: Command with --string, --bool, --int → extracts all 3 with types
3. **Example parsing**: Multi-line example → splits correctly, preserves formatting
4. **Subcommand recursion**: Nested 3 levels → traverses full depth, builds parent-child links
5. **Nil handling**: Command with no flags/examples → returns empty slices, no panics

**Deliverables**:

- [ ] File: `cmd/docs/generator.go` (~200 LOC)
  - [ ] Type: `CommandDoc` struct (8 fields)
  - [ ] Type: `FlagDoc` struct (6 fields)
  - [ ] Function: `GenerateCommandDocs(root *cobra.Command) []CommandDoc`
  - [ ] Function: `extractFlags(cmd *cobra.Command) []FlagDoc`
  - [ ] Function: `walkCommandTree(cmd *cobra.Command, parent string, docs *[]CommandDoc)`
- [ ] File: `cmd/docs/generator_test.go` (~250 LOC)
  - [ ] Test: `TestGenerateCommandDocs_TraversesTree`
  - [ ] Test: `TestExtractFlags_AllTypes`
  - [ ] Test: `TestWalkCommandTree_Nested`
  - [ ] Test: `TestCommandDoc_NilHandling`
  - [ ] Test: `TestFlagDoc_DefaultValues`
- [ ] Build: `go build ./cmd/docs` succeeds
- [ ] Tests: `go test ./cmd/docs` passes (≥80% coverage)

**Integration Dependencies**:

- Phase 6: Uses Cobra command tree structure
- Exports: `CommandDoc` type used by Task 8.2

**Key Implementation Details**:

- Use `cmd.Commands()` for subcommand iteration
- Use `cmd.Flags().VisitAll()` for flag extraction
- Store parent-child relationships using command names
- Parse `cmd.Example` string into slice (split on newline, trim)
- Handle hidden commands (skip if `cmd.Hidden == true`)

---

### Task 8.2: HTTP Server & Routing

**Goal**: Serve documentation via HTTP with clean routes and graceful shutdown

**Estimated Time**: 1.5 hours

**Test-First Workflow**:

1. **Write tests first** (`cmd/docs/server_test.go`):

   ```go
   func TestStartDocsServer_RootEndpoint(t *testing.T) {
       root := &cobra.Command{Use: "test"}
       ctx, cancel := context.WithCancel(context.Background())
       defer cancel()

       go StartDocsServer(ctx, 8999, root)
       time.Sleep(100 * time.Millisecond) // Wait for server start

       resp, err := http.Get("http://localhost:8999/")
       require.NoError(t, err)
       assert.Equal(t, 200, resp.StatusCode)
   }
   ```

2. **Implement** (`cmd/docs/server.go`):

   ```go
   func StartDocsServer(ctx context.Context, port int, rootCmd *cobra.Command) error {
       docs := GenerateCommandDocs(rootCmd)
       mux := http.NewServeMux()

       mux.HandleFunc("/", handleIndex(docs))
       mux.HandleFunc("/cmd/", handleCommand(docs))
       mux.HandleFunc("/schema.json", handleSchema(docs))

       srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

       // Graceful shutdown
       go func() {
           <-ctx.Done()
           srv.Shutdown(context.Background())
       }()

       return srv.ListenAndServe()
   }
   ```

3. **Verify**:
   ```bash
   go test ./cmd/docs/server_test.go -v
   curl http://localhost:8080/
   curl http://localhost:8080/schema.json
   ```

**Test Scenarios** (≥5 required):

1. **Root endpoint** (/) → 200 OK, returns HTML with command list
2. **Command detail** (/cmd/docs) → 200 OK, returns command-specific HTML
3. **404 handling** (/cmd/nonexistent) → 404 Not Found with helpful message
4. **JSON export** (/schema.json) → 200 OK, valid JSON array of CommandDoc
5. **Graceful shutdown** → Context cancellation closes server cleanly within 5s

**Deliverables**:

- [ ] File: `cmd/docs/server.go` (~150 LOC)
  - [ ] Function: `StartDocsServer(ctx context.Context, port int, root *cobra.Command) error`
  - [ ] Function: `handleIndex(docs []CommandDoc) http.HandlerFunc`
  - [ ] Function: `handleCommand(docs []CommandDoc) http.HandlerFunc`
  - [ ] Function: `handleSchema(docs []CommandDoc) http.HandlerFunc`
- [ ] File: `cmd/docs/server_test.go` (~200 LOC)
  - [ ] Test: `TestStartDocsServer_RootEndpoint`
  - [ ] Test: `TestHandleCommand_ValidName`
  - [ ] Test: `TestHandleCommand_InvalidName`
  - [ ] Test: `TestHandleSchema_JSON`
  - [ ] Test: `TestGracefulShutdown`
- [ ] Integration: Add `--docs-port` flag to existing `serve` command
- [ ] Build: `go build ./cmd` succeeds
- [ ] Tests: `go test ./cmd/docs` passes (≥80% coverage)

**Integration with serve command** (`cmd/serve.go`):

```go
var docsPort int

func init() {
    serveCmd.Flags().IntVar(&docsPort, "docs-port", 8080, "Port for documentation server")
}

func runServe(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    // Start docs server in goroutine
    if docsPort > 0 {
        go docs.StartDocsServer(ctx, docsPort, cmd.Root())
    }

    // Continue with MCP server or other serve logic...
}
```

**Key Implementation Details**:

- Use `http.ServeMux` for routing (stdlib, no external deps)
- Extract command name from URL path: `/cmd/docs` → "docs"
- Search docs slice for matching command name
- Return 404 with JSON: `{"error": "Command not found", "available": [...]}`
- Use `json.Marshal()` for /schema.json endpoint
- Log startup: `fmt.Printf("Documentation server: http://localhost:%d\n", port)`

---

### Task 8.3: HTML Templates & Styling

**Goal**: Create responsive HTML templates with Tailwind CSS and 3-way dark mode

**Estimated Time**: 3 hours

**Implementation Guidance** (Not test-first - visual implementation):

**Directory Structure**:

```
cmd/docs/templates/
├── layout.html
├── index.html
├── command.html
└── partials/
    ├── sidebar.html
    ├── header.html
    └── command-card.html
```

**Step 1: Create layout.html** (~150 LOC)

**Purpose**: Master template with common structure (head, sidebar, dark mode, scripts)

**Implementation**:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>{{.Title}} - Emergent CLI Documentation</title>
    <script src="https://cdn.tailwindcss.com?plugins=typography"></script>
    <style>
      /* Dark mode CSS variables */
      :root[data-theme='light'] {
        --bg: #ffffff;
        --text: #000000;
      }
      :root[data-theme='dark'] {
        --bg: #1a1a1a;
        --text: #ffffff;
      }
    </style>
  </head>
  <body class="bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100">
    <!-- Mobile Header -->
    {{template "header" .}}

    <div class="flex min-h-screen">
      <!-- Sidebar (desktop) -->
      <aside class="hidden md:block w-64 bg-white dark:bg-gray-800 border-r">
        {{template "sidebar" .}}
      </aside>

      <!-- Main Content -->
      <main class="flex-1 p-8">{{template "content" .}}</main>
    </div>

    <!-- Dark Mode Script -->
    <script>
      function setTheme(mode) {
        if (mode === 'system') {
          const prefersDark = window.matchMedia(
            '(prefers-color-scheme: dark)'
          ).matches;
          document.documentElement.dataset.theme = prefersDark
            ? 'dark'
            : 'light';
        } else {
          document.documentElement.dataset.theme = mode;
        }
        localStorage.setItem('theme', mode);
      }

      // Load saved theme or default to system
      const saved = localStorage.getItem('theme') || 'system';
      setTheme(saved);

      // Listen for system preference changes
      window
        .matchMedia('(prefers-color-scheme: dark)')
        .addEventListener('change', (e) => {
          if (localStorage.getItem('theme') === 'system') {
            document.documentElement.dataset.theme = e.matches
              ? 'dark'
              : 'light';
          }
        });
    </script>
  </body>
</html>
```

**Key Features**:

- Tailwind CDN (v3.4+) with typography plugin
- Mobile-first responsive (sidebar hidden on mobile, shows hamburger menu)
- Three theme slots: system (default), dark, light
- localStorage persistence for theme selection
- Template slots: `{{template "header"}}`, `{{template "sidebar"}}`, `{{template "content"}}`

---

**Step 2: Create index.html** (~100 LOC)

**Purpose**: Homepage with command grid and search placeholder

**Implementation**:

```html
{{define "content"}}
<div class="max-w-7xl mx-auto">
  <h1 class="text-4xl font-bold mb-2">Emergent CLI Documentation</h1>
  <p class="text-gray-600 dark:text-gray-400 mb-8">
    Command-line interface for Emergent platform
  </p>

  <!-- Search (placeholder, not functional in MVP) -->
  <input
    type="text"
    placeholder="Search commands..."
    class="w-full p-3 border rounded-lg mb-8 bg-white dark:bg-gray-800"
    disabled
  />

  <!-- Command Grid -->
  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
    {{range .Commands}} {{template "command-card" .}} {{end}}
  </div>
</div>
{{end}}
```

**Key Features**:

- Responsive grid: 1 col (mobile), 2 cols (tablet), 3 cols (desktop)
- Search input (disabled, visual placeholder for future)
- Iterates over `{{.Commands}}` slice
- Uses `command-card` partial for each item

---

**Step 3: Create command.html** (~150 LOC)

**Purpose**: Command detail page with usage, flags, examples, subcommands

**Implementation**:

```html
{{define "content"}}
<div class="max-w-4xl mx-auto">
  <!-- Back Link -->
  <a
    href="/"
    class="text-blue-600 dark:text-blue-400 hover:underline mb-4 inline-block"
  >
    ← Back to Commands
  </a>

  <!-- Command Header -->
  <h1 class="text-4xl font-bold mb-2">{{.Name}}</h1>
  <p class="text-xl text-gray-600 dark:text-gray-400 mb-8">{{.Short}}</p>

  <!-- Usage -->
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Usage</h2>
    <pre class="bg-gray-100 dark:bg-gray-800 p-4 rounded-lg overflow-x-auto">
<code>{{.Usage}}</code></pre>
  </section>

  <!-- Description -->
  {{if .Long}}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Description</h2>
    <div class="prose dark:prose-invert">{{.Long}}</div>
  </section>
  {{end}}

  <!-- Flags -->
  {{if .Flags}}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Flags</h2>
    <div class="overflow-x-auto">
      <table class="w-full border">
        <thead class="bg-gray-100 dark:bg-gray-800">
          <tr>
            <th class="p-3 text-left">Flag</th>
            <th class="p-3 text-left">Type</th>
            <th class="p-3 text-left">Default</th>
            <th class="p-3 text-left">Description</th>
          </tr>
        </thead>
        <tbody>
          {{range .Flags}}
          <tr class="border-t">
            <td class="p-3 font-mono">--{{.Name}}</td>
            <td class="p-3">{{.Type}}</td>
            <td class="p-3">{{.Default}}</td>
            <td class="p-3">{{.Usage}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
  {{end}}

  <!-- Examples -->
  {{if .Examples}}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Examples</h2>
    {{range .Examples}}
    <pre
      class="bg-gray-100 dark:bg-gray-800 p-4 rounded-lg mb-4 overflow-x-auto"
    >
<code>{{.}}</code></pre>
    {{end}}
  </section>
  {{end}}

  <!-- Subcommands -->
  {{if .Subcommands}}
  <section class="mb-8">
    <h2 class="text-2xl font-semibold mb-4">Subcommands</h2>
    <ul class="list-disc list-inside space-y-2">
      {{range .Subcommands}}
      <li>
        <a
          href="/cmd/{{.}}"
          class="text-blue-600 dark:text-blue-400 hover:underline"
          >{{.}}</a
        >
      </li>
      {{end}}
    </ul>
  </section>
  {{end}}
</div>
{{end}}
```

**Key Features**:

- Back navigation to homepage
- Conditional rendering ({{if .Long}}, {{if .Flags}}, etc.)
- Responsive table for flags (horizontal scroll on mobile)
- Code blocks with syntax-friendly styling
- Linked subcommands

---

**Step 4: Create partials/sidebar.html** (~100 LOC)

**Purpose**: Navigation sidebar with logo, command tree, dark mode toggle

**Implementation**:

```html
{{define "sidebar"}}
<div class="h-screen flex flex-col">
  <!-- Logo/Branding -->
  <div class="p-6 border-b">
    <h2 class="text-2xl font-bold">Emergent CLI</h2>
    <p class="text-sm text-gray-600 dark:text-gray-400">v{{.Version}}</p>
  </div>

  <!-- Command Tree -->
  <nav class="flex-1 overflow-y-auto p-4">
    <ul class="space-y-2">
      {{range .Commands}}
      <li>
        <a
          href="/cmd/{{.Name}}"
          class="block p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
        >
          {{.Name}}
        </a>
        {{if .Subcommands}}
        <ul class="ml-4 mt-1 space-y-1">
          {{range .Subcommands}}
          <li>
            <a
              href="/cmd/{{.}}"
              class="block p-1 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100"
            >
              {{.}}
            </a>
          </li>
          {{end}}
        </ul>
        {{end}}
      </li>
      {{end}}
    </ul>
  </nav>

  <!-- Dark Mode Toggle -->
  <div class="p-4 border-t">
    <div class="flex gap-2">
      <button
        onclick="setTheme('light')"
        class="flex-1 p-2 rounded border"
        data-theme-btn="light"
      >
        ☀️
      </button>
      <button
        onclick="setTheme('system')"
        class="flex-1 p-2 rounded border"
        data-theme-btn="system"
      >
        💻
      </button>
      <button
        onclick="setTheme('dark')"
        class="flex-1 p-2 rounded border"
        data-theme-btn="dark"
      >
        🌙
      </button>
    </div>
  </div>
</div>
{{end}}
```

**Key Features**:

- Scrollable command list (fixed height, overflow-y-auto)
- Nested subcommand links (indented with ml-4)
- 3-button dark mode toggle (light/system/dark)
- Version display from template data

---

**Step 5: Create partials/header.html** (~50 LOC)

**Purpose**: Mobile-only header with hamburger menu

**Implementation**:

```html
{{define "header"}}
<header
  class="md:hidden bg-white dark:bg-gray-800 border-b p-4 flex items-center justify-between"
>
  <h1 class="text-xl font-bold">Emergent CLI</h1>

  <button
    onclick="toggleSidebar()"
    class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
    aria-label="Toggle menu"
  >
    <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="2"
        d="M4 6h16M4 12h16M4 18h16"
      />
    </svg>
  </button>
</header>

<script>
  function toggleSidebar() {
    const sidebar = document.querySelector('aside');
    sidebar.classList.toggle('hidden');
  }
</script>
{{end}}
```

**Key Features**:

- Visible only on mobile (md:hidden)
- Hamburger icon (SVG, 3 horizontal lines)
- Toggle function shows/hides sidebar
- Accessibility: aria-label

---

**Step 6: Create partials/command-card.html** (~50 LOC)

**Purpose**: Reusable card component for command grid

**Implementation**:

```html
{{define "command-card"}}
<a
  href="/cmd/{{.Name}}"
  class="block p-6 bg-white dark:bg-gray-800 rounded-lg border hover:border-blue-500 transition"
>
  <div class="flex items-start gap-3">
    <!-- Icon Placeholder -->
    <div class="text-3xl">📄</div>

    <!-- Content -->
    <div class="flex-1">
      <h3 class="text-lg font-semibold mb-1">{{.Name}}</h3>
      <p class="text-sm text-gray-600 dark:text-gray-400 line-clamp-2">
        {{.Short}}
      </p>
    </div>
  </div>
</a>
{{end}}
```

**Key Features**:

- Entire card is clickable link
- Icon placeholder (📄 emoji, easily replaced with SVG)
- Text truncation (line-clamp-2, max 2 lines)
- Hover effect (border color change)

---

**Step 7: Go Embed Directives** (`cmd/docs/server.go`)

**Add at top of file**:

```go
package docs

import (
    _ "embed"
    "embed"
    "html/template"
)

//go:embed templates/*
var templateFS embed.FS

func loadTemplates() (*template.Template, error) {
    return template.ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
}
```

**Verification**:

```bash
# Build binary
go build -o emergent-cli ./cmd

# Test embedded templates work
./emergent-cli serve --docs-port 8080
# Binary should serve docs without needing templates/ directory present
```

**Deliverables**:

- [ ] File: `cmd/docs/templates/layout.html` (~150 LOC)
- [ ] File: `cmd/docs/templates/index.html` (~100 LOC)
- [ ] File: `cmd/docs/templates/command.html` (~150 LOC)
- [ ] File: `cmd/docs/templates/partials/sidebar.html` (~100 LOC)
- [ ] File: `cmd/docs/templates/partials/header.html` (~50 LOC)
- [ ] File: `cmd/docs/templates/partials/command-card.html` (~50 LOC)
- [ ] Update: `cmd/docs/server.go` (add embed directives, use `template.ParseFS`)
- [ ] Manual Test: Verify responsive layout at 375px, 768px, 1024px widths
- [ ] Manual Test: Verify dark mode toggle persists across page loads
- [ ] Manual Test: Verify binary works without templates/ directory

**Key Tailwind Classes Used**:

- Layout: `flex`, `grid`, `hidden`, `md:block`, `lg:grid-cols-3`
- Spacing: `p-*`, `m-*`, `gap-*`, `space-y-*`
- Colors: `bg-gray-*`, `text-gray-*`, `dark:bg-*`, `dark:text-*`
- Typography: `text-*xl`, `font-bold`, `prose`
- Interactive: `hover:*`, `border`, `rounded-*`

**Dark Mode CSS Variables**:

```css
:root[data-theme='light'] {
  --bg-primary: #ffffff;
  --text-primary: #000000;
}
:root[data-theme='dark'] {
  --bg-primary: #1a1a1a;
  --text-primary: #ffffff;
}
```

---

### Task 8.4: Template Rendering Tests

**Goal**: Verify templates render correctly with various data inputs

**Estimated Time**: 1 hour

**Test-First Workflow**:

1. **Write tests** (`cmd/docs/templates_test.go`):

   ```go
   func TestRenderLayout_WithData(t *testing.T) {
       tmpl, err := loadTemplates()
       require.NoError(t, err)

       data := map[string]interface{}{
           "Title": "Test",
           "Commands": []CommandDoc{{Name: "test", Short: "A test"}},
       }

       var buf bytes.Buffer
       err = tmpl.ExecuteTemplate(&buf, "layout.html", data)
       require.NoError(t, err)

       output := buf.String()
       assert.Contains(t, output, "Test - Emergent CLI")
       assert.Contains(t, output, "test")
   }
   ```

2. **Verify templates work**:

   ```bash
   go test ./cmd/docs/templates_test.go -v
   ```

3. **Fix issues** (missing data keys, nil pointer dereferences, template syntax errors)

**Test Scenarios** (≥5 required):

1. **Layout renders**: With title, commands list → HTML output contains expected structure
2. **Index with 10+ commands**: Grid renders all command cards, responsive classes present
3. **Command with 5+ flags**: Flags table renders, all columns present (name, type, default, usage)
4. **Partials render**: Sidebar, header, card → All partials execute without errors
5. **Nil data handling**: Command with no flags/examples → Template gracefully handles empty slices

**Deliverables**:

- [ ] File: `cmd/docs/templates_test.go` (~150 LOC)
  - [ ] Test: `TestRenderLayout_WithData`
  - [ ] Test: `TestRenderIndex_WithCommands`
  - [ ] Test: `TestRenderCommand_WithFlags`
  - [ ] Test: `TestRenderPartials_NoErrors`
  - [ ] Test: `TestTemplates_NilDataHandling`
- [ ] Tests: All pass, no panics
- [ ] Coverage: ≥80% for template execution paths

**Common Template Errors to Test**:

- Missing keys: `{{.MissingKey}}` → Should not panic if key doesn't exist in data
- Nil slices: `{{range .Nil}}` → Should render nothing, not panic
- Type mismatches: Passing string where struct expected → Should error gracefully
- Missing partials: `{{template "nonexistent"}}` → Should error clearly

---

### Task 8.5: Documentation & Polish

**Goal**: Document the documentation server architecture and usage

**Estimated Time**: 0.5 hours

**Documentation Deliverables**:

1. **README.md** (`cmd/docs/README.md`, ~100 lines):

   ````markdown
   # Documentation Server

   Embedded HTTP server that generates interactive HTML documentation from Cobra commands.

   ## Usage

   Start the server:

   ```bash
   emergent-cli serve --docs-port 8080
   ```
   ````

   Open browser: http://localhost:8080

   ## Features

   - Responsive design (mobile, tablet, desktop)
   - 3-way dark mode (system, light, dark)
   - Auto-generated from Cobra command tree
   - Embedded templates (works in compiled binary)
   - Zero external dependencies at runtime

   ## Template Structure

   ```
   templates/
   ├── layout.html       # Master template
   ├── index.html        # Homepage
   ├── command.html      # Command detail page
   └── partials/
       ├── sidebar.html  # Navigation
       ├── header.html   # Mobile header
       └── command-card.html  # Grid card
   ```

   ## Customization

   ### Replace Tailwind CDN with local build:

   1. Download Tailwind CSS
   2. Replace `<script src="...cdn...">` in layout.html
   3. Add custom CSS in layout.html `<style>` block

   ### Modify templates:

   1. Edit templates in `cmd/docs/templates/`
   2. Rebuild: `go build ./cmd`
   3. Templates are embedded via `//go:embed`

   ## Dark Mode

   - Automatically detects system preference (`prefers-color-scheme`)
   - Toggle: 3 buttons (☀️ light, 💻 system, 🌙 dark)
   - Persists choice in localStorage
   - CSS variables for easy theming

   ## Architecture

   ```
   Cobra Command Tree
         ↓
   GenerateCommandDocs() (extract metadata)
         ↓
   HTTP Server (routes: /, /cmd/{name}, /schema.json)
         ↓
   HTML Templates (Tailwind CSS + dark mode)
         ↓
   Browser (responsive, interactive)
   ```

   ## API Endpoints

   - `GET /` → Homepage with command grid
   - `GET /cmd/{name}` → Command detail page
   - `GET /schema.json` → JSON export of all commands

   ```

   ```

2. **GoDoc comments** (add to `cmd/docs/generator.go`, `server.go`):

   ```go
   // Package docs provides an embedded HTTP documentation server
   // that generates interactive HTML docs from Cobra commands.
   //
   // The server auto-extracts metadata (flags, examples, subcommands)
   // and renders responsive HTML with dark mode support.
   //
   // Usage:
   //   ctx := context.Background()
   //   docs.StartDocsServer(ctx, 8080, rootCmd)
   //
   // Templates are embedded in the binary via go:embed.
   package docs

   // GenerateCommandDocs walks the Cobra command tree and extracts
   // structured documentation metadata for each command.
   //
   // It recursively traverses subcommands, extracts flags with types
   // and defaults, parses examples, and builds parent-child relationships.
   //
   // Returns a slice of CommandDoc structs, one per command in the tree.
   func GenerateCommandDocs(root *cobra.Command) []CommandDoc { ... }
   ```

3. **Architecture Diagram** (ASCII art in README):
   ```
   ┌─────────────────────┐
   │  Cobra Command Tree │
   │  (rootCmd + subs)   │
   └──────────┬──────────┘
              │
              ▼
   ┌──────────────────────┐
   │  GenerateCommandDocs │
   │  (metadata extraction)│
   └──────────┬───────────┘
              │
              ▼
   ┌──────────────────────┐
   │   HTTP Server        │
   │   /:  index.html     │
   │   /cmd/:  command    │
   │   /schema.json       │
   └──────────┬───────────┘
              │
              ▼
   ┌──────────────────────┐
   │  HTML Templates      │
   │  (Tailwind + dark)   │
   └──────────┬───────────┘
              │
              ▼
   ┌──────────────────────┐
   │   Browser Render     │
   └──────────────────────┘
   ```

**Deliverables**:

- [ ] File: `cmd/docs/README.md` (~100 lines)
  - [ ] Section: Usage examples
  - [ ] Section: Template structure explanation
  - [ ] Section: Customization guide
  - [ ] Section: Dark mode behavior
  - [ ] Section: Architecture diagram (ASCII art)
- [ ] GoDoc: All public types/functions in `generator.go` have doc comments
- [ ] GoDoc: All public types/functions in `server.go` have doc comments
- [ ] GoDoc: Package-level doc comment explaining purpose and usage

**Polish Items**:

- Verify all error messages are user-friendly (no raw stack traces in browser)
- Add `Access-Control-Allow-Origin: *` header to `/schema.json` for tooling
- Log server startup: `Documentation server running at http://localhost:8080`
- Add `/health` endpoint returning `{"status": "ok"}` for monitoring

---

**Phase 8 Summary**:

**Total Estimated Time**: 8 hours

- Task 8.1 (Generator): 2h
- Task 8.2 (Server): 1.5h
- Task 8.3 (Templates): 3h
- Task 8.4 (Tests): 1h
- Task 8.5 (Docs): 0.5h

**Total Deliverables**:

- 10 Go files (~1,000 LOC)
- 6 HTML template files (~600 LOC)
- 5 test files (~800 LOC)
- 1 comprehensive README (~100 lines)

**Dependencies**:

- **Requires**: Phase 6 (Cobra commands must exist to document)
- **Provides**: Interactive documentation server accessible via browser

**Verification**:

```bash
# Build
go build -o emergent-cli ./cmd

# Start server
./emergent-cli serve --docs-port 8080

# Open browser
open http://localhost:8080

# Verify:
# - Homepage shows all commands in grid
# - Clicking command shows detail page with flags
# - Dark mode toggle works (persists across reloads)
# - Mobile responsive (test at 375px width)
# - Binary works without templates/ directory present
```

## Phase 9: MCP Proxy Server

**Goal**: Expose CLI commands as MCP tools for Claude Desktop integration

**Estimated Time**: 6.5 hours

**Overview**: Implement Model Context Protocol (MCP) server that wraps CLI commands as callable tools. Supports both stdio transport (Claude Desktop) and HTTP/SSE transport (web clients). Automatically generates MCP tool schemas from Cobra command metadata (flags, descriptions, examples).

---

### Task 9.1: MCP Protocol Core (2h)

**Goal**: Implement core MCP server with protocol handling, tool registration, and request routing.

**Estimated Time**: 2 hours

**Test-First Workflow**:

1. **Write tests first** (`internal/mcp/server_test.go`)
2. **Implement** (`internal/mcp/server.go`)
3. **Verify**:
   ```bash
   go test ./internal/mcp -v -run TestMCPServer
   go build ./internal/mcp
   ```

**Test Scenarios** (5 required):

1. **Initialize server**: Create `MCPServer` with root command, verify tool count matches command count
2. **Register tool**: Add tool with schema, verify appears in tool list
3. **Handle call request**: Valid JSON-RPC `tools/call` request → execute tool → return result
4. **Handle list request**: `tools/list` request → return all registered tools with schemas
5. **Invalid request**: Malformed JSON-RPC → return error response with proper error code

**Implementation Guidance**:

```go
// Types for MCP protocol
type MCPServer struct {
    tools     map[string]*Tool  // tool name → tool definition
    rootCmd   *cobra.Command    // Cobra command tree
    mu        sync.RWMutex      // protect tools map
}

type Tool struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    InputSchema map[string]any `json:"inputSchema"`  // JSON Schema
    Handler     func(args map[string]any) (any, error)
}

type JSONRPCRequest struct {
    JSONRPC string         `json:"jsonrpc"`  // must be "2.0"
    ID      any            `json:"id"`       // request ID
    Method  string         `json:"method"`   // e.g., "tools/call"
    Params  map[string]any `json:"params"`
}

type JSONRPCResponse struct {
    JSONRPC string `json:"jsonrpc"`
    ID      any    `json:"id"`
    Result  any    `json:"result,omitempty"`
    Error   *Error `json:"error,omitempty"`
}

type Error struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

**Key Functions**:

```go
// NewMCPServer creates server from Cobra root command
func NewMCPServer(rootCmd *cobra.Command) *MCPServer

// RegisterTool adds a tool (generated from Cobra command)
func (s *MCPServer) RegisterTool(tool *Tool) error

// HandleRequest processes JSON-RPC request
func (s *MCPServer) HandleRequest(req *JSONRPCRequest) *JSONRPCResponse

// ListTools returns all available tools
func (s *MCPServer) ListTools() []Tool

// CallTool executes a tool by name with arguments
func (s *MCPServer) CallTool(name string, args map[string]any) (any, error)
```

**Protocol Methods**:

- `initialize` - Handshake, exchange capabilities
- `tools/list` - Return available tools
- `tools/call` - Execute a tool
- `ping` - Health check

**Deliverables**:

- [ ] File: `internal/mcp/server.go` (~250 LOC)
  - [ ] Type: `MCPServer` struct
  - [ ] Type: `Tool` struct
  - [ ] Type: `JSONRPCRequest` struct
  - [ ] Type: `JSONRPCResponse` struct
  - [ ] Function: `NewMCPServer(rootCmd *cobra.Command) *MCPServer`
  - [ ] Method: `RegisterTool(tool *Tool) error`
  - [ ] Method: `HandleRequest(req *JSONRPCRequest) *JSONRPCResponse`
  - [ ] Method: `ListTools() []Tool`
  - [ ] Method: `CallTool(name, args) (any, error)`
- [ ] File: `internal/mcp/server_test.go` (~300 LOC)
  - [ ] Test: `TestNewMCPServer` - Initialization
  - [ ] Test: `TestRegisterTool` - Tool registration
  - [ ] Test: `TestHandleCallRequest` - Tool execution
  - [ ] Test: `TestHandleListRequest` - Tool listing
  - [ ] Test: `TestInvalidRequest` - Error handling
- [ ] Build: `go build ./internal/mcp` succeeds
- [ ] Tests: `go test ./internal/mcp -v` passes (≥80% coverage)

**Verification**:

```bash
# Run tests
go test ./internal/mcp -v -run TestMCPServer

# Build check
go build ./internal/mcp

# Coverage
go test ./internal/mcp -cover
```

---

### Task 9.2: Tool Schema Generation (1.5h)

**Goal**: Convert Cobra commands to MCP tool schemas with proper input validation.

**Estimated Time**: 1.5 hours

**Test-First Workflow**:

1. **Write tests first** (`internal/mcp/tools_test.go`)
2. **Implement** (`internal/mcp/tools.go`)
3. **Verify**:
   ```bash
   go test ./internal/mcp -v -run TestToolGeneration
   go build ./internal/mcp
   ```

**Test Scenarios** (5 required):

1. **Simple command**: Command with no flags → tool with empty input schema
2. **String flag**: Command with `--api-key string` → tool with string property (required)
3. **Bool flag**: Command with `--verbose bool` → tool with boolean property (optional)
4. **Int flag**: Command with `--count int` → tool with integer property with min/max
5. **Nested command**: Subcommand `config get` → tool named `config-get` with parent context

**Implementation Guidance**:

```go
// GenerateToolsFromCommand walks Cobra tree and generates MCP tools
func GenerateToolsFromCommand(rootCmd *cobra.Command) ([]*Tool, error) {
    var tools []*Tool

    // Walk command tree
    walkCommand(rootCmd, "", func(cmd *cobra.Command, path string) {
        // Skip root and non-runnable commands
        if cmd.Run == nil && cmd.RunE == nil {
            return
        }

        tool := &Tool{
            Name:        generateToolName(path, cmd.Name()),
            Description: cmd.Short,
            InputSchema: generateSchema(cmd),
            Handler:     createHandler(cmd),
        }
        tools = append(tools, tool)
    })

    return tools, nil
}

// generateSchema creates JSON Schema from command flags
func generateSchema(cmd *cobra.Command) map[string]any {
    schema := map[string]any{
        "type": "object",
        "properties": make(map[string]any),
        "required": []string{},
    }

    // Process flags
    cmd.Flags().VisitAll(func(flag *pflag.Flag) {
        prop := flagToProperty(flag)
        schema["properties"].(map[string]any)[flag.Name] = prop

        // Mark required if no default
        if flag.DefValue == "" && !isOptionalType(flag.Value.Type()) {
            required := schema["required"].([]string)
            schema["required"] = append(required, flag.Name)
        }
    })

    return schema
}

// flagToProperty converts Cobra flag to JSON Schema property
func flagToProperty(flag *pflag.Flag) map[string]any {
    prop := map[string]any{
        "description": flag.Usage,
    }

    // Map Go types to JSON Schema types
    switch flag.Value.Type() {
    case "string":
        prop["type"] = "string"
    case "bool":
        prop["type"] = "boolean"
    case "int", "int64":
        prop["type"] = "integer"
    case "float64":
        prop["type"] = "number"
    case "stringSlice", "stringArray":
        prop["type"] = "array"
        prop["items"] = map[string]any{"type": "string"}
    }

    return prop
}

// createHandler wraps Cobra command execution
func createHandler(cmd *cobra.Command) func(map[string]any) (any, error) {
    return func(args map[string]any) (any, error) {
        // Set flags from args
        for key, val := range args {
            if err := cmd.Flags().Set(key, fmt.Sprint(val)); err != nil {
                return nil, err
            }
        }

        // Capture output
        var buf bytes.Buffer
        cmd.SetOut(&buf)
        cmd.SetErr(&buf)

        // Execute
        if err := cmd.Execute(); err != nil {
            return nil, err
        }

        return map[string]any{
            "output": buf.String(),
            "status": "success",
        }, nil
    }
}
```

**Type Mappings**:

| Cobra Type     | JSON Schema Type | Notes                    |
| -------------- | ---------------- | ------------------------ |
| `string`       | `string`         | -                        |
| `bool`         | `boolean`        | -                        |
| `int`, `int64` | `integer`        | Add `minimum`, `maximum` |
| `float64`      | `number`         | -                        |
| `stringSlice`  | `array`          | Items: `string`          |
| `duration`     | `string`         | Format: `duration`       |
| Custom enum    | `string`         | Add `enum` array         |

**Deliverables**:

- [ ] File: `internal/mcp/tools.go` (~200 LOC)
  - [ ] Function: `GenerateToolsFromCommand(rootCmd) ([]*Tool, error)`
  - [ ] Function: `generateSchema(cmd) map[string]any`
  - [ ] Function: `flagToProperty(flag) map[string]any`
  - [ ] Function: `createHandler(cmd) func(args) (any, error)`
  - [ ] Function: `generateToolName(path, name) string`
  - [ ] Helper: `walkCommand(cmd, path, fn)` - Recursive traversal
- [ ] File: `internal/mcp/tools_test.go` (~250 LOC)
  - [ ] Test: `TestGenerateSimpleCommand` - No flags
  - [ ] Test: `TestGenerateStringFlag` - String property
  - [ ] Test: `TestGenerateBoolFlag` - Boolean property
  - [ ] Test: `TestGenerateIntFlag` - Integer with validation
  - [ ] Test: `TestNestedCommand` - Subcommand naming
- [ ] Build: `go build ./internal/mcp` succeeds
- [ ] Tests: `go test ./internal/mcp -v` passes (≥80% coverage)

**Verification**:

```bash
# Run tests
go test ./internal/mcp -v -run TestToolGeneration

# Build check
go build ./internal/mcp

# Coverage
go test ./internal/mcp -cover
```

---

### Task 9.3: Transport Implementations (2.5h)

**Goal**: Implement stdio and HTTP/SSE transports for MCP protocol.

**Estimated Time**: 2.5 hours (1h stdio + 1.5h HTTP/SSE)

**Test-First Workflow**:

1. **Write tests first** (`internal/mcp/stdio_test.go`, `internal/mcp/http_test.go`)
2. **Implement** (`internal/mcp/stdio.go`, `internal/mcp/http.go`)
3. **Verify**:
   ```bash
   go test ./internal/mcp -v -run TestStdio
   go test ./internal/mcp -v -run TestHTTP
   go build ./internal/mcp
   ```

**Test Scenarios** (5 per transport):

**Stdio Transport**:

1. **Read request**: JSON-RPC on stdin → parse → verify
2. **Write response**: Response object → write JSON to stdout
3. **Handle multiple requests**: Read loop until EOF
4. **Invalid JSON**: Malformed stdin → error response
5. **Signal handling**: SIGINT → graceful shutdown

**HTTP/SSE Transport**:

1. **POST /mcp**: Valid JSON-RPC → 200 OK with result
2. **GET /mcp/stream**: SSE connection → send events
3. **Keep-alive**: Idle connection → periodic heartbeat events
4. **Invalid request**: Bad JSON → 400 Bad Request
5. **Concurrent requests**: Multiple clients → isolated execution

**Implementation Guidance**:

**Stdio Transport** (`stdio.go`):

```go
// StdioServer wraps MCPServer for stdin/stdout transport
type StdioServer struct {
    server *MCPServer
    reader *bufio.Reader
    writer *bufio.Writer
}

// NewStdioServer creates stdio transport
func NewStdioServer(server *MCPServer) *StdioServer {
    return &StdioServer{
        server: server,
        reader: bufio.NewReader(os.Stdin),
        writer: bufio.NewWriter(os.Stdout),
    }
}

// Run starts stdio loop (blocks until EOF or signal)
func (s *StdioServer) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Read JSON-RPC request (one per line)
        line, err := s.reader.ReadString('\n')
        if err == io.EOF {
            return nil  // Clean shutdown
        }
        if err != nil {
            return err
        }

        // Parse request
        var req JSONRPCRequest
        if err := json.Unmarshal([]byte(line), &req); err != nil {
            s.writeError(nil, -32700, "Parse error")
            continue
        }

        // Handle request
        resp := s.server.HandleRequest(&req)

        // Write response
        if err := s.writeResponse(resp); err != nil {
            return err
        }
    }
}

// writeResponse writes JSON-RPC response to stdout
func (s *StdioServer) writeResponse(resp *JSONRPCResponse) error {
    data, err := json.Marshal(resp)
    if err != nil {
        return err
    }

    if _, err := s.writer.Write(data); err != nil {
        return err
    }
    if err := s.writer.WriteByte('\n'); err != nil {
        return err
    }
    return s.writer.Flush()
}
```

**HTTP/SSE Transport** (`http.go`):

```go
// HTTPServer provides HTTP/SSE transport for MCP
type HTTPServer struct {
    server *MCPServer
    mux    *http.ServeMux
}

// NewHTTPServer creates HTTP transport
func NewHTTPServer(server *MCPServer) *HTTPServer {
    hs := &HTTPServer{
        server: server,
        mux:    http.NewServeMux(),
    }

    hs.mux.HandleFunc("/mcp", hs.handleMCP)
    hs.mux.HandleFunc("/mcp/stream", hs.handleStream)

    return hs
}

// handleMCP processes JSON-RPC over HTTP POST
func (hs *HTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Parse request
    var req JSONRPCRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }

    // Handle request
    resp := hs.server.HandleRequest(&req)

    // Write response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

// handleStream provides SSE endpoint
func (hs *HTTPServer) handleStream(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    // Send heartbeat every 30s
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            fmt.Fprintf(w, "event: ping\ndata: {\"timestamp\": \"%s\"}\n\n", time.Now().Format(time.RFC3339))
            flusher.Flush()
        }
    }
}

// ListenAndServe starts HTTP server
func (hs *HTTPServer) ListenAndServe(addr string) error {
    return http.ListenAndServe(addr, hs.mux)
}
```

**Deliverables**:

- [ ] File: `internal/mcp/stdio.go` (~150 LOC)
  - [ ] Type: `StdioServer` struct
  - [ ] Function: `NewStdioServer(server) *StdioServer`
  - [ ] Method: `Run(ctx) error` - Main loop
  - [ ] Method: `writeResponse(resp) error`
  - [ ] Helper: `handleSignals(ctx)` - SIGINT handling
- [ ] File: `internal/mcp/stdio_test.go` (~200 LOC)
  - [ ] Test: `TestStdioReadRequest` - Parse stdin
  - [ ] Test: `TestStdioWriteResponse` - Write stdout
  - [ ] Test: `TestStdioMultipleRequests` - Loop handling
  - [ ] Test: `TestStdioInvalidJSON` - Error handling
  - [ ] Test: `TestStdioShutdown` - Graceful exit
- [ ] File: `internal/mcp/http.go` (~180 LOC)
  - [ ] Type: `HTTPServer` struct
  - [ ] Function: `NewHTTPServer(server) *HTTPServer`
  - [ ] Method: `handleMCP(w, r)` - POST endpoint
  - [ ] Method: `handleStream(w, r)` - SSE endpoint
  - [ ] Method: `ListenAndServe(addr) error`
- [ ] File: `internal/mcp/http_test.go` (~250 LOC)
  - [ ] Test: `TestHTTPPost` - JSON-RPC POST
  - [ ] Test: `TestHTTPStream` - SSE connection
  - [ ] Test: `TestHTTPKeepAlive` - Heartbeat
  - [ ] Test: `TestHTTPInvalidRequest` - 400 response
  - [ ] Test: `TestHTTPConcurrent` - Multiple clients
- [ ] Build: `go build ./internal/mcp` succeeds
- [ ] Tests: `go test ./internal/mcp -v` passes (≥80% coverage)

**Verification**:

```bash
# Test stdio transport
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | go run ./cmd/mcp stdio

# Test HTTP transport
go run ./cmd/mcp http --port 3000 &
curl -X POST http://localhost:3000/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

# Test SSE
curl -N http://localhost:3000/mcp/stream

# Coverage
go test ./internal/mcp -cover
```

---

### Task 9.4: Integration & Documentation (0.5h)

**Goal**: Add MCP command to CLI, create Claude Desktop config examples, document usage.

**Estimated Time**: 30 minutes

**Implementation Guidance**:

**Add MCP Command** (`cmd/mcp.go`):

```go
package cmd

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "github.com/spf13/cobra"
    "emergent-cli/internal/mcp"
)

var mcpCmd = &cobra.Command{
    Use:   "mcp [stdio|http]",
    Short: "Run MCP proxy server",
    Long: `Start Model Context Protocol server that exposes CLI commands as callable tools.

Supports two transports:
  stdio - stdin/stdout for Claude Desktop integration
  http  - HTTP/SSE for web clients`,
}

var stdioCmd = &cobra.Command{
    Use:   "stdio",
    Short: "Run MCP server with stdio transport",
    RunE: func(cmd *cobra.Command, args []string) error {
        server := mcp.NewMCPServer(rootCmd)
        tools, _ := mcp.GenerateToolsFromCommand(rootCmd)
        for _, tool := range tools {
            server.RegisterTool(tool)
        }

        stdio := mcp.NewStdioServer(server)

        ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
        defer cancel()

        return stdio.Run(ctx)
    },
}

var httpCmd = &cobra.Command{
    Use:   "http",
    Short: "Run MCP server with HTTP/SSE transport",
    RunE: func(cmd *cobra.Command, args []string) error {
        port, _ := cmd.Flags().GetInt("port")

        server := mcp.NewMCPServer(rootCmd)
        tools, _ := mcp.GenerateToolsFromCommand(rootCmd)
        for _, tool := range tools {
            server.RegisterTool(tool)
        }

        http := mcp.NewHTTPServer(server)
        return http.ListenAndServe(fmt.Sprintf(":%d", port))
    },
}

func init() {
    httpCmd.Flags().IntP("port", "p", 3000, "HTTP server port")
    mcpCmd.AddCommand(stdioCmd, httpCmd)
    rootCmd.AddCommand(mcpCmd)
}
```

**Claude Desktop Configuration**:

Create example config file showing how to register the MCP server.

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
**Linux**: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "emergent-cli": {
      "command": "/path/to/emergent-cli",
      "args": ["mcp", "stdio"]
    }
  }
}
```

**Documentation** (`internal/mcp/README.md`):

Create comprehensive README covering:

1. **What is MCP?** - Brief explanation of Model Context Protocol
2. **Usage** - How to run stdio and HTTP servers
3. **Claude Desktop Setup** - Step-by-step config instructions
4. **Tool Schema** - Explanation of auto-generated schemas
5. **Examples** - Sample tool calls from Claude
6. **Development** - Testing and extending the server

**Deliverables**:

- [ ] File: `cmd/mcp.go` (~80 LOC)
  - [ ] Command: `mcp` (parent command)
  - [ ] Subcommand: `mcp stdio` (stdio transport)
  - [ ] Subcommand: `mcp http` (HTTP transport with --port flag)
  - [ ] Integration: Wire up MCPServer + tool generation
- [ ] File: `internal/mcp/README.md` (~150 lines)
  - [ ] Section: What is MCP?
  - [ ] Section: Usage (stdio and HTTP examples)
  - [ ] Section: Claude Desktop setup (with config file example)
  - [ ] Section: Tool schema generation
  - [ ] Section: Examples (sample interactions)
  - [ ] Section: Troubleshooting
- [ ] File: `examples/claude_desktop_config.json` (~15 lines)
  - [ ] Example: macOS config file path
  - [ ] Example: Windows config file path
  - [ ] Example: Linux config file path
  - [ ] Example: Configuration with absolute path to binary

**Verification**:

```bash
# Build
go build -o emergent-cli ./cmd

# Test stdio transport
./emergent-cli mcp stdio

# Test HTTP transport
./emergent-cli mcp http --port 3000

# Verify Claude Desktop integration (manual)
# 1. Copy binary to permanent location
# 2. Update claude_desktop_config.json with absolute path
# 3. Restart Claude Desktop
# 4. Type "/" in chat to see available tools
# 5. Try calling a tool (e.g., "emergent-cli-login")
```

---

**Phase 9 Summary**:

**Total Estimated Time**: 6.5 hours

- Task 9.1 (Protocol Core): 2h
- Task 9.2 (Tool Generation): 1.5h
- Task 9.3 (Transports): 2.5h
- Task 9.4 (Integration): 0.5h

**Total Deliverables**:

- 7 Go files (~1,200 LOC)
- 5 test files (~1,000 LOC)
- 1 comprehensive README (~150 lines)
- 1 example config file (~15 lines)

**Dependencies**:

- **Requires**: Phase 6 (Cobra commands must exist to convert to tools)
- **Provides**: MCP server for Claude Desktop integration

**Verification**:

```bash
# Build
go build -o emergent-cli ./cmd

# Unit tests
go test ./internal/mcp -v -cover

# Integration test: stdio
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | ./emergent-cli mcp stdio

# Integration test: HTTP
./emergent-cli mcp http --port 3000 &
curl -X POST http://localhost:3000/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

# Verify:
# - All tools from CLI appear in tools/list response
# - Each tool has valid JSON Schema for inputs
# - Calling a tool executes the underlying Cobra command
# - Stdio transport works with Claude Desktop
# - HTTP transport serves multiple concurrent requests
```

**Key Design Decisions**:

1. **Protocol Compliance**: Strict JSON-RPC 2.0 implementation for compatibility
2. **Schema Generation**: Automatic conversion of Cobra flags to JSON Schema properties
3. **Transport Separation**: Clean interfaces allow adding new transports (WebSocket, gRPC)
4. **Error Handling**: MCP-compliant error codes and messages
5. **Concurrency**: Thread-safe tool registration and execution

## Phase 10: Interactive Prompts

**Total Phase Duration**: ~5.5 hours

### Task 10.1: Prompt Library Core (2h)

**Goal**: Create reusable prompt library with input validation, selection menus, and confirmation dialogs.

**Estimated Time**: 2 hours

**Test-First Workflow**:

1. Write tests defining expected prompt behaviors (5 scenarios)
2. Run tests (expect failures)
3. Implement prompt functions to satisfy tests
4. Verify all tests pass with ≥80% coverage

**Test Scenarios** (5 required):

1. **Text Input with Validation**:

   - Given: Prompt for text input with min length 3
   - When: User enters "ab"
   - Then: Shows error "must be at least 3 characters", re-prompts
   - When: User enters "abc"
   - Then: Returns "abc", no error

2. **Single Selection Menu**:

   - Given: Menu with 3 options ["Dev", "Staging", "Prod"]
   - When: User presses arrow keys and Enter on "Staging"
   - Then: Returns "Staging"

3. **Multi-Selection Menu**:

   - Given: Checkbox menu with 5 options
   - When: User selects options 1, 3, 5 with Space, confirms with Enter
   - Then: Returns ["Option1", "Option3", "Option5"]

4. **Confirmation Prompt**:

   - Given: Yes/No confirmation "Delete all data?"
   - When: User presses "n"
   - Then: Returns false
   - When: User presses "y"
   - Then: Returns true

5. **Validation Error Handling**:
   - Given: Email input with regex validation
   - When: User enters "invalid-email"
   - Then: Shows "invalid email format", re-prompts
   - When: User enters "user@example.com"
   - Then: Returns email, no error

**Implementation Guidance**:

**Library Selection**:

Two popular Go libraries for terminal UI prompts:

| Library                            | Pros                                      | Cons                  | Recommendation                        |
| ---------------------------------- | ----------------------------------------- | --------------------- | ------------------------------------- |
| `github.com/manifoldco/promptui`   | Simple API, good docs, active maintenance | Limited customization | ✅ **Recommended** for most use cases |
| `github.com/AlecAivazis/survey/v2` | Rich features, themes, multi-select       | Heavier dependency    | Use if need advanced styling          |

**Decision**: Use `promptui` for simplicity and smaller footprint.

**Core Types** (`internal/prompts/prompt.go`):

```go
package prompts

import (
	"fmt"
	"regexp"
	"github.com/manifoldco/promptui"
)

// InputPrompt represents a text input prompt
type InputPrompt struct {
	Label      string
	Default    string
	Validate   func(string) error
	Mask       rune  // For password inputs (0 = no mask)
}

// SelectPrompt represents a single-selection menu
type SelectPrompt struct {
	Label     string
	Items     []string
	Default   int  // Index of default selection
	Size      int  // Visible items (0 = show all)
}

// MultiSelectPrompt represents a multi-selection checkbox menu
type MultiSelectPrompt struct {
	Label     string
	Items     []string
	Defaults  []int  // Indices of pre-selected items
}

// ConfirmPrompt represents a yes/no confirmation
type ConfirmPrompt struct {
	Label   string
	Default bool
}

// ValidationError wraps validation failure message
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Common validators
func ValidateNotEmpty(input string) error {
	if input == "" {
		return &ValidationError{Message: "value cannot be empty"}
	}
	return nil
}

func ValidateMinLength(min int) func(string) error {
	return func(input string) error {
		if len(input) < min {
			return &ValidationError{Message: fmt.Sprintf("must be at least %d characters", min)}
		}
		return nil
	}
}

func ValidateEmail(input string) error {
	regex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !regex.MatchString(input) {
		return &ValidationError{Message: "invalid email format"}
	}
	return nil
}

func ValidateURL(input string) error {
	regex := regexp.MustCompile(`^https?://[^\s]+$`)
	if !regex.MatchString(input) {
		return &ValidationError{Message: "invalid URL format (must start with http:// or https://)"}
	}
	return nil
}

// NewInput creates and executes a text input prompt
func NewInput(opts InputPrompt) (string, error) {
	prompt := promptui.Prompt{
		Label:     opts.Label,
		Default:   opts.Default,
		Validate:  opts.Validate,
		Mask:      opts.Mask,
	}

	return prompt.Run()
}

// NewSelect creates and executes a single-selection menu
func NewSelect(opts SelectPrompt) (int, string, error) {
	size := opts.Size
	if size == 0 {
		size = len(opts.Items)  // Show all items by default
	}

	prompt := promptui.Select{
		Label:     opts.Label,
		Items:     opts.Items,
		CursorPos: opts.Default,
		Size:      size,
	}

	return prompt.Run()
}

// NewConfirm creates and executes a yes/no confirmation
func NewConfirm(opts ConfirmPrompt) (bool, error) {
	defaultValue := "n"
	if opts.Default {
		defaultValue = "y"
	}

	prompt := promptui.Prompt{
		Label:     opts.Label,
		IsConfirm: true,
		Default:   defaultValue,
	}

	result, err := prompt.Run()
	if err != nil {
		// promptui returns error on "n" in confirm mode
		if err == promptui.ErrAbort {
			return false, nil
		}
		return false, err
	}

	return result == "y", nil
}

// NewMultiSelect creates and executes a multi-selection checkbox menu
func NewMultiSelect(opts MultiSelectPrompt) ([]string, error) {
	// Note: promptui doesn't support native multi-select
	// Use survey library or implement custom loop
	// This is a placeholder showing the interface

	// Implementation using survey:
	// import "github.com/AlecAivazis/survey/v2"

	var selected []string
	prompt := &survey.MultiSelect{
		Message: opts.Label,
		Options: opts.Items,
		Default: makeDefaults(opts.Items, opts.Defaults),
	}

	if err := survey.AskOne(prompt, &selected); err != nil {
		return nil, err
	}

	return selected, nil
}

func makeDefaults(items []string, indices []int) []string {
	defaults := make([]string, len(indices))
	for i, idx := range indices {
		if idx < len(items) {
			defaults[i] = items[idx]
		}
	}
	return defaults
}
```

**Deliverables**:

- [ ] File: `internal/prompts/prompt.go` (~250 LOC)
  - [ ] Type: `InputPrompt` (label, default, validate, mask)
  - [ ] Type: `SelectPrompt` (label, items, default, size)
  - [ ] Type: `MultiSelectPrompt` (label, items, defaults)
  - [ ] Type: `ConfirmPrompt` (label, default bool)
  - [ ] Type: `ValidationError` (message string)
  - [ ] Function: `NewInput(InputPrompt) (string, error)`
  - [ ] Function: `NewSelect(SelectPrompt) (int, string, error)`
  - [ ] Function: `NewConfirm(ConfirmPrompt) (bool, error)`
  - [ ] Function: `NewMultiSelect(MultiSelectPrompt) ([]string, error)`
  - [ ] Validators: `ValidateNotEmpty`, `ValidateMinLength`, `ValidateEmail`, `ValidateURL`
- [ ] File: `internal/prompts/prompt_test.go` (~300 LOC)
  - [ ] Test: `TestInputWithValidation` (Scenario 1)
  - [ ] Test: `TestSelectMenu` (Scenario 2)
  - [ ] Test: `TestMultiSelect` (Scenario 3)
  - [ ] Test: `TestConfirmPrompt` (Scenario 4)
  - [ ] Test: `TestValidationErrors` (Scenario 5)
  - [ ] Mock: `mockTerminal` (simulate stdin/stdout for testing)
- [ ] Go module: Add `github.com/manifoldco/promptui` and `github.com/AlecAivazis/survey/v2` to go.mod
- [ ] Build: `go build ./internal/prompts` succeeds
- [ ] Tests: `go test ./internal/prompts -v` passes (≥80% coverage)

**Verification**:

```bash
# Unit tests
go test ./internal/prompts -v -cover

# Interactive manual test
go run examples/prompt_demo.go
# Should show:
# 1. Text input prompt (try invalid then valid)
# 2. Selection menu (arrow keys + Enter)
# 3. Multi-select menu (Space to select, Enter to confirm)
# 4. Confirmation (y/n)

# Coverage check
go test ./internal/prompts -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

### Task 10.2: Common Prompt Flows (1.5h)

**Goal**: Implement reusable prompt flows for login, configuration, and resource selection.

**Estimated Time**: 1.5 hours

**Test-First Workflow**:

1. Write tests for each flow scenario (5 scenarios)
2. Run tests (expect failures)
3. Implement flow functions
4. Verify all tests pass with ≥80% coverage

**Test Scenarios** (5 required):

1. **Complete Login Flow**:

   - Given: No credentials provided
   - When: User enters URL "https://api.example.com", selects Chrome browser
   - Then: Returns `LoginConfig{URL: "https://api.example.com", Browser: "chrome"}`

2. **Partial Config Wizard**:

   - Given: Existing config with URL set
   - When: User runs wizard, URL field shows current value, user updates org
   - Then: Returns config with new org, URL unchanged

3. **Project Selection**:

   - Given: API returns 5 projects
   - When: User selects "Project Alpha"
   - Then: Returns project ID for "Project Alpha"

4. **Cancel Handling**:

   - Given: User in middle of config wizard
   - When: User presses Ctrl+C
   - Then: Returns `ErrCancelled`, no partial writes

5. **Smart Defaults**:
   - Given: Single org exists
   - When: SelectOrg() called
   - Then: Auto-selects org without prompting

**Implementation Guidance**:

**Login Flow** (`internal/prompts/flows.go`):

```go
package prompts

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrCancelled = errors.New("operation cancelled by user")
	ErrNoOptions = errors.New("no options available")
)

// LoginConfig contains login flow inputs
type LoginConfig struct {
	URL     string
	Browser string
}

// ConfigWizardInput contains current config for wizard
type ConfigWizardInput struct {
	CurrentURL     string
	CurrentOrg     string
	CurrentProject string
}

// ConfigWizardOutput contains wizard results
type ConfigWizardOutput struct {
	URL     string
	Org     string
	Project string
}

// LoginFlow guides user through login configuration
func LoginFlow(ctx context.Context) (*LoginConfig, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ErrCancelled
	default:
	}

	// Prompt for API URL
	url, err := NewInput(InputPrompt{
		Label:    "API Base URL",
		Default:  "https://api.emergent-company.ai",
		Validate: ValidateURL,
	})
	if err != nil {
		return nil, fmt.Errorf("url input failed: %w", err)
	}

	// Prompt for browser
	_, browser, err := NewSelect(SelectPrompt{
		Label: "Browser for authentication",
		Items: []string{"chrome", "firefox", "safari", "edge"},
		Default: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("browser selection failed: %w", err)
	}

	return &LoginConfig{
		URL:     url,
		Browser: browser,
	}, nil
}

// ConfigWizard guides user through configuration setup
func ConfigWizard(ctx context.Context, input ConfigWizardInput) (*ConfigWizardOutput, error) {
	output := &ConfigWizardOutput{
		URL:     input.CurrentURL,
		Org:     input.CurrentOrg,
		Project: input.CurrentProject,
	}

	// Check for cancellation before each step
	checkCancel := func() error {
		select {
		case <-ctx.Done():
			return ErrCancelled
		default:
			return nil
		}
	}

	// URL (show current as default)
	if err := checkCancel(); err != nil {
		return nil, err
	}

	url, err := NewInput(InputPrompt{
		Label:    "API Base URL",
		Default:  input.CurrentURL,
		Validate: ValidateURL,
	})
	if err != nil {
		return nil, fmt.Errorf("url input failed: %w", err)
	}
	output.URL = url

	// Org (show current as default)
	if err := checkCancel(); err != nil {
		return nil, err
	}

	org, err := NewInput(InputPrompt{
		Label:    "Organization ID",
		Default:  input.CurrentOrg,
		Validate: ValidateNotEmpty,
	})
	if err != nil {
		return nil, fmt.Errorf("org input failed: %w", err)
	}
	output.Org = org

	// Project (optional)
	if err := checkCancel(); err != nil {
		return nil, err
	}

	project, err := NewInput(InputPrompt{
		Label:   "Project ID (optional)",
		Default: input.CurrentProject,
	})
	if err != nil {
		return nil, fmt.Errorf("project input failed: %w", err)
	}
	output.Project = project

	return output, nil
}

// SelectOrg prompts user to select an organization
// Returns empty string if no selection made
// Auto-selects if only one option available
func SelectOrg(ctx context.Context, orgs []OrgOption) (string, error) {
	if len(orgs) == 0 {
		return "", ErrNoOptions
	}

	// Smart default: auto-select if only one
	if len(orgs) == 1 {
		fmt.Printf("Auto-selecting organization: %s\n", orgs[0].Name)
		return orgs[0].ID, nil
	}

	// Check cancellation
	select {
	case <-ctx.Done():
		return "", ErrCancelled
	default:
	}

	// Build menu items (format: "Name (ID)")
	items := make([]string, len(orgs))
	for i, org := range orgs {
		items[i] = fmt.Sprintf("%s (%s)", org.Name, org.ID)
	}

	idx, _, err := NewSelect(SelectPrompt{
		Label: "Select Organization",
		Items: items,
	})
	if err != nil {
		return "", fmt.Errorf("org selection failed: %w", err)
	}

	return orgs[idx].ID, nil
}

// SelectProject prompts user to select a project
// Returns empty string if no selection made
// Auto-selects if only one option available
func SelectProject(ctx context.Context, projects []ProjectOption) (string, error) {
	if len(projects) == 0 {
		return "", ErrNoOptions
	}

	// Smart default: auto-select if only one
	if len(projects) == 1 {
		fmt.Printf("Auto-selecting project: %s\n", projects[0].Name)
		return projects[0].ID, nil
	}

	// Check cancellation
	select {
	case <-ctx.Done():
		return "", ErrCancelled
	default:
	}

	// Build menu items
	items := make([]string, len(projects))
	for i, proj := range projects {
		items[i] = fmt.Sprintf("%s (%s)", proj.Name, proj.ID)
	}

	idx, _, err := NewSelect(SelectPrompt{
		Label: "Select Project",
		Items: items,
	})
	if err != nil {
		return "", fmt.Errorf("project selection failed: %w", err)
	}

	return projects[idx].ID, nil
}

// SelectTemplate prompts user to select a template
// Returns empty string if no selection made
func SelectTemplate(ctx context.Context, templates []TemplateOption) (string, error) {
	if len(templates) == 0 {
		return "", ErrNoOptions
	}

	// Check cancellation
	select {
	case <-ctx.Done():
		return "", ErrCancelled
	default:
	}

	// Build menu items with descriptions
	items := make([]string, len(templates))
	for i, tmpl := range templates {
		items[i] = fmt.Sprintf("%s - %s", tmpl.Name, tmpl.Description)
	}

	idx, _, err := NewSelect(SelectPrompt{
		Label: "Select Template",
		Items: items,
		Size:  10,  // Show max 10 at a time
	})
	if err != nil {
		return "", fmt.Errorf("template selection failed: %w", err)
	}

	return templates[idx].ID, nil
}

// OrgOption represents an organization for selection
type OrgOption struct {
	ID   string
	Name string
}

// ProjectOption represents a project for selection
type ProjectOption struct {
	ID   string
	Name string
}

// TemplateOption represents a template for selection
type TemplateOption struct {
	ID          string
	Name        string
	Description string
}
```

**Deliverables**:

- [ ] File: `internal/prompts/flows.go` (~200 LOC)
  - [ ] Type: `LoginConfig` (URL, Browser)
  - [ ] Type: `ConfigWizardInput` (current values)
  - [ ] Type: `ConfigWizardOutput` (new values)
  - [ ] Type: `OrgOption`, `ProjectOption`, `TemplateOption`
  - [ ] Error: `ErrCancelled`, `ErrNoOptions`
  - [ ] Function: `LoginFlow(ctx) (*LoginConfig, error)`
  - [ ] Function: `ConfigWizard(ctx, input) (*ConfigWizardOutput, error)`
  - [ ] Function: `SelectOrg(ctx, []OrgOption) (string, error)`
  - [ ] Function: `SelectProject(ctx, []ProjectOption) (string, error)`
  - [ ] Function: `SelectTemplate(ctx, []TemplateOption) (string, error)`
- [ ] File: `internal/prompts/flows_test.go` (~250 LOC)
  - [ ] Test: `TestLoginFlowComplete` (Scenario 1)
  - [ ] Test: `TestConfigWizardPartial` (Scenario 2)
  - [ ] Test: `TestSelectProject` (Scenario 3)
  - [ ] Test: `TestFlowCancellation` (Scenario 4)
  - [ ] Test: `TestSmartDefaultSingleOption` (Scenario 5)
  - [ ] Mock: Context cancellation simulation
- [ ] Build: `go build ./internal/prompts` succeeds
- [ ] Tests: `go test ./internal/prompts -v` passes (≥80% coverage)

**Verification**:

```bash
# Unit tests
go test ./internal/prompts -v -cover

# Integration test: login flow
go run cmd/main.go login
# (manually walk through prompts)

# Integration test: config wizard
go run cmd/main.go config wizard
# (manually verify defaults are pre-filled)

# Coverage
go test ./internal/prompts -coverprofile=coverage.out
go tool cover -func=coverage.out | grep flows.go
```

---

### Task 10.3: Command Integration (1h)

**Goal**: Integrate prompts into existing commands to make flags optional, prompting when missing.

**Estimated Time**: 1 hour

**Test Scenarios** (3 required):

1. **Flag Provided (No Prompt)**:

   - Given: `emergent-cli config set --project proj-123`
   - When: Command executes
   - Then: Uses `proj-123`, no prompt shown

2. **Flag Missing (Prompt)**:

   - Given: `emergent-cli config set` (no --project flag)
   - When: Command executes
   - Then: Shows project selection prompt, uses selected value

3. **Prompt Cancelled**:
   - Given: `emergent-cli config set` (no --project flag)
   - When: User presses Ctrl+C during prompt
   - Then: Command exits with error "operation cancelled by user"

**Implementation Guidance**:

**Pattern for Optional Flags**:

```go
// Before: Required flag
projectCmd.Flags().StringP("project", "p", "", "Project ID")
projectCmd.MarkFlagRequired("project")  // ← Remove this

// After: Optional flag with prompt fallback
projectCmd.Flags().StringP("project", "p", "", "Project ID (will prompt if not provided)")

func runProjectCommand(cmd *cobra.Command, args []string) error {
	projectID, _ := cmd.Flags().GetString("project")

	// If not provided via flag, prompt for it
	if projectID == "" {
		if !isInteractiveTerminal() {
			return errors.New("--project required in non-interactive mode")
		}

		// Fetch available projects from API
		projects, err := fetchProjects()
		if err != nil {
			return fmt.Errorf("failed to fetch projects: %w", err)
		}

		// Prompt user to select
		projectID, err = prompts.SelectProject(cmd.Context(), projects)
		if err != nil {
			return fmt.Errorf("project selection failed: %w", err)
		}
	}

	// Continue with selected projectID
	return doProjectOperation(projectID)
}
```

**TTY Detection**:

```go
// internal/prompts/tty.go

package prompts

import (
	"os"
	"golang.org/x/term"
)

// IsInteractive returns true if running in an interactive terminal
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// RequireInteractive returns an error if not in interactive mode
func RequireInteractive() error {
	if !IsInteractive() {
		return errors.New("interactive mode required (not running in TTY)")
	}
	return nil
}
```

**Commands to Update**:

1. **`cmd/login.go`**: Make `--url` optional, prompt if missing
2. **`cmd/config.go`**: Add `config wizard` subcommand, make flags optional
3. **`cmd/generate.go`**: Make `--project` and `--template` optional

**Example Integration** (`cmd/login.go`):

```go
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with the Emergent API",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		browser, _ := cmd.Flags().GetString("browser")

		// If flags not provided, use interactive flow
		if url == "" || browser == "" {
			if !prompts.IsInteractive() {
				return errors.New("--url and --browser required in non-interactive mode")
			}

			config, err := prompts.LoginFlow(cmd.Context())
			if err != nil {
				return fmt.Errorf("login flow failed: %w", err)
			}

			url = config.URL
			browser = config.Browser
		}

		// Continue with authentication
		return performLogin(url, browser)
	},
}

func init() {
	loginCmd.Flags().StringP("url", "u", "", "API base URL (will prompt if not provided)")
	loginCmd.Flags().StringP("browser", "b", "", "Browser to use for auth (will prompt if not provided)")
	rootCmd.AddCommand(loginCmd)
}
```

**Deliverables**:

- [ ] File: `internal/prompts/tty.go` (~30 LOC)
  - [ ] Function: `IsInteractive() bool`
  - [ ] Function: `RequireInteractive() error`
- [ ] File: `cmd/login.go` (update ~20 LOC)
  - [ ] Remove: `MarkFlagRequired("url")`
  - [ ] Add: Interactive fallback with `LoginFlow()`
  - [ ] Add: TTY check before prompting
- [ ] File: `cmd/config.go` (update ~30 LOC)
  - [ ] Add: `config wizard` subcommand using `ConfigWizard()`
  - [ ] Update: Make org/project flags optional in other subcommands
  - [ ] Add: Prompt fallback when flags missing
- [ ] File: `cmd/generate.go` (update ~25 LOC)
  - [ ] Update: Make `--project` optional with `SelectProject()` fallback
  - [ ] Update: Make `--template` optional with `SelectTemplate()` fallback
  - [ ] Add: TTY check before prompting
- [ ] Tests: Add unit tests for TTY detection
- [ ] Build: `go build -o emergent-cli ./cmd` succeeds
- [ ] Manual test: Verify prompts appear when flags omitted

**Verification**:

```bash
# Build
go build -o emergent-cli ./cmd

# Test: Flag provided (no prompt)
./emergent-cli login --url https://api.example.com --browser chrome
# Should login without prompting

# Test: Flag missing (prompt)
./emergent-cli login
# Should show URL prompt, then browser selection

# Test: Non-interactive detection
echo "" | ./emergent-cli login
# Should error: "interactive mode required"

# Test: Config wizard
./emergent-cli config wizard
# Should walk through URL, org, project prompts

# Test: Generate with prompts
./emergent-cli generate
# Should prompt for project, then template
```

---

**Phase 10 Summary**:

**Total Estimated Time**: 5.5 hours

- Task 10.1 (Prompt Library): 2h
- Task 10.2 (Common Flows): 1.5h
- Task 10.3 (Command Integration): 1h
- Buffer for testing: 1h

**Total Deliverables**:

- 4 Go files (~500 LOC)
- 3 test files (~550 LOC)
- 3 command updates (~75 LOC)

**Dependencies**:

- **Requires**: Phase 6 (Cobra commands to integrate with)
- **Provides**: Interactive user experience, optional flags with smart fallbacks

**Verification**:

```bash
# Build
go build -o emergent-cli ./cmd

# Unit tests
go test ./internal/prompts -v -cover

# Integration tests (manual)
./emergent-cli login                  # Should prompt for URL and browser
./emergent-cli login --url <url>      # Should only prompt for browser
./emergent-cli config wizard          # Should walk through full setup
./emergent-cli generate               # Should prompt for project and template
./emergent-cli generate --project <id> # Should only prompt for template

# Non-interactive mode tests
echo "" | ./emergent-cli login        # Should error (TTY required)
./emergent-cli login --url <url> --browser chrome  # Should succeed without TTY

# Verify:
# - Prompts appear when flags missing in TTY
# - Flags bypass prompts when provided
# - Non-TTY mode errors helpfully
# - Ctrl+C cancels gracefully
# - Single-option menus auto-select
```

**Key Design Decisions**:

1. **Library Choice**: `promptui` for simplicity, `survey` for multi-select
2. **Smart Defaults**: Auto-select when only one option (orgs, projects)
3. **TTY Detection**: Check terminal before prompting, error in non-interactive
4. **Cancellation**: Respect Ctrl+C, return `ErrCancelled` gracefully
5. **Validation**: Reusable validators (email, URL, min length, not empty)
6. **Progressive Enhancement**: Flags work as before, prompts enhance UX

## Phase 11: Error Handling

**Goal**: Build comprehensive error type system with user-friendly formatting, actionable suggestions, and automatic retry mechanisms for transient failures.

**Total Phase Duration**: ~4.5 hours

**Why This Matters**:

- Generic "Internal server error" messages frustrate users and increase support burden
- Proper error handling distinguishes between recoverable (network timeout) and fatal (invalid credentials) failures
- Context-aware error messages guide users to fix issues themselves (e.g., "Run 'emergent login' again")
- Debug mode provides technical details for troubleshooting without overwhelming users

**Integration Points**:

- Phase 3 (HTTP Client): Return typed errors instead of generic `error`
- Phase 4 (Auth): Use `AuthError` for login/token problems
- Phase 6 (Commands): All commands use `FormatError()` before printing
- Phase 10 (Prompts): Handle `ErrCancelled` gracefully when user presses Ctrl+C

### Task 11.1: Error Type System (2h)

**Goal**: Create error type hierarchy with context preservation, wrapping support, and JSON serialization for API responses.

**Estimated Time**: 2 hours

**Test-First Workflow**:

1. Write tests defining error type behaviors (5 scenarios)
2. Run tests (expect failures)
3. Implement error types with wrapping/unwrapping
4. Verify all tests pass with ≥80% coverage

**Test Scenarios** (5 required):

1. **Error Wrapping with Context Preservation**:

   - Given: NetworkError wrapping connection refused error
   - When: Call `Error()` method
   - Then: Returns "Network error: connection refused: dial tcp 127.0.0.1:3002: connect: connection refused"
   - And: Preserves full chain with `%+v` format

2. **Type Checking with errors.Is()**:

   - Given: AuthError wrapped in generic error
   - When: Call `errors.Is(err, &AuthError{})`
   - Then: Returns true
   - When: Call `errors.Is(err, &NetworkError{})`
   - Then: Returns false

3. **Type Assertion with errors.As()**:

   - Given: ConfigError with path "/home/user/.emergent/config.json"
   - When: Unwrap with `errors.As(err, &configErr)`
   - Then: Returns true and configErr.Path == "/home/user/.emergent/config.json"

4. **Error Chain Unwrapping**:

   - Given: AuthError → APIError → NetworkError chain
   - When: Call `Unwrap()` repeatedly
   - Then: Returns each error in chain until nil

5. **JSON Serialization for API Responses**:
   - Given: APIError with statusCode 401, message "Token expired"
   - When: Marshal to JSON
   - Then: Returns `{"code":"auth_error","message":"Token expired","statusCode":401}`

**Implementation Guidance**:

**Core Interface** (`internal/errors/errors.go`):

```go
package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// CLIError represents any CLI-specific error with context
type CLIError interface {
	error
	Code() string            // Unique error code (e.g., "auth_error", "network_timeout")
	Context() map[string]any // Additional context (URL, status code, etc.)
	Unwrap() error           // For error wrapping (Go 1.13+)
	Is(target error) bool    // For errors.Is() support
}

// BaseError provides common error functionality
type BaseError struct {
	code    string
	message string
	context map[string]any
	wrapped error
}

func (e *BaseError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("%s: %v", e.message, e.wrapped)
	}
	return e.message
}

func (e *BaseError) Code() string            { return e.code }
func (e *BaseError) Context() map[string]any { return e.context }
func (e *BaseError) Unwrap() error           { return e.wrapped }

func (e *BaseError) Is(target error) bool {
	t, ok := target.(*BaseError)
	if !ok {
		return false
	}
	return e.code == t.code
}

// MarshalJSON for API error responses
func (e *BaseError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"code":    e.code,
		"message": e.message,
		"context": e.context,
	})
}

// AuthError represents authentication/authorization failures
type AuthError struct {
	BaseError
	TokenExpired    bool
	InvalidToken    bool
	InsufficientPerm bool
}

func NewAuthError(message string, wrapped error) *AuthError {
	return &AuthError{
		BaseError: BaseError{
			code:    "auth_error",
			message: message,
			context: make(map[string]any),
			wrapped: wrapped,
		},
	}
}

// NetworkError represents connection/network failures
type NetworkError struct {
	BaseError
	URL        string
	StatusCode int
	Timeout    bool
	DNSFailure bool
}

func NewNetworkError(message string, url string, wrapped error) *NetworkError {
	return &NetworkError{
		BaseError: BaseError{
			code:    "network_error",
			message: message,
			context: map[string]any{"url": url},
			wrapped: wrapped,
		},
		URL: url,
	}
}

// ValidationError represents invalid input/configuration
type ValidationError struct {
	BaseError
	Field   string
	Value   any
	Pattern string // For regex/format validation
}

func NewValidationError(field string, message string) *ValidationError {
	return &ValidationError{
		BaseError: BaseError{
			code:    "validation_error",
			message: message,
			context: map[string]any{"field": field},
		},
		Field: field,
	}
}

// APIError represents server-side errors
type APIError struct {
	BaseError
	StatusCode int
	RequestID  string
	Endpoint   string
	Retryable  bool
}

func NewAPIError(statusCode int, message string, endpoint string) *APIError {
	retryable := statusCode == http.StatusTooManyRequests ||
		statusCode >= 500

	return &APIError{
		BaseError: BaseError{
			code:    "api_error",
			message: message,
			context: map[string]any{
				"statusCode": statusCode,
				"endpoint":   endpoint,
			},
		},
		StatusCode: statusCode,
		Endpoint:   endpoint,
		Retryable:  retryable,
	}
}

// ConfigError represents configuration file issues
type ConfigError struct {
	BaseError
	Path       string
	Permission bool // Permission denied
	NotFound   bool
	InvalidJSON bool
}

func NewConfigError(path string, message string, wrapped error) *ConfigError {
	return &ConfigError{
		BaseError: BaseError{
			code:    "config_error",
			message: message,
			context: map[string]any{"path": path},
			wrapped: wrapped,
		},
		Path: path,
	}
}

// ResourceError represents missing/invalid resources (projects, orgs, templates)
type ResourceError struct {
	BaseError
	ResourceType string // "project", "organization", "template"
	ResourceID   string
	NotFound     bool
}

func NewResourceError(resourceType string, resourceID string, message string) *ResourceError {
	return &ResourceError{
		BaseError: BaseError{
			code:    "resource_error",
			message: message,
			context: map[string]any{
				"resourceType": resourceType,
				"resourceID":   resourceID,
			},
		},
		ResourceType: resourceType,
		ResourceID:   resourceID,
		NotFound:     true,
	}
}

// ErrCancelled is returned when user cancels operation (Ctrl+C)
var ErrCancelled = &BaseError{
	code:    "cancelled",
	message: "Operation cancelled by user",
}

// IsRetryable checks if error is transient and can be retried
func IsRetryable(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Retryable
	}
	if netErr, ok := err.(*NetworkError); ok {
		return netErr.Timeout || netErr.StatusCode == 0
	}
	return false
}
```

**Error Creation Helpers** (~50 LOC):

```go
// Common error constructors for frequent scenarios

func NewTokenExpiredError() *AuthError {
	err := NewAuthError("Authentication token expired", nil)
	err.TokenExpired = true
	return err
}

func NewInvalidCredentialsError() *AuthError {
	return NewAuthError("Invalid credentials", nil)
}

func NewTimeoutError(url string) *NetworkError {
	err := NewNetworkError("Request timed out", url, nil)
	err.Timeout = true
	return err
}

func NewNotFoundError(resourceType string, resourceID string) *ResourceError {
	return NewResourceError(
		resourceType,
		resourceID,
		fmt.Sprintf("%s '%s' not found", resourceType, resourceID),
	)
}
```

**Deliverables Checklist**:

- [ ] `internal/errors/errors.go` (~200 LOC)
  - [ ] `CLIError` interface definition
  - [ ] `BaseError` with wrapping/unwrapping
  - [ ] 6 concrete error types (Auth, Network, Validation, API, Config, Resource)
  - [ ] `Error()`, `Unwrap()`, `Is()`, `As()` methods
  - [ ] `MarshalJSON()` for API responses
  - [ ] Helper constructors for common scenarios
- [ ] `internal/errors/errors_test.go` (~200 LOC)
  - [ ] 5 test scenarios (wrapping, Is, As, unwrap, JSON)
  - [ ] Edge cases: nil wrapped error, empty context
  - [ ] Coverage ≥80%
- [ ] Verify: `go build ./internal/errors && go test ./internal/errors -v`

**Integration with Phase 3 (HTTP Client)**:

Update `internal/client/client.go` to return typed errors:

```go
func (c *Client) makeRequest(method string, path string, body any) (*http.Response, error) {
	// ... existing code ...

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		message := string(bodyBytes)

		switch {
		case resp.StatusCode == 401:
			return nil, errors.NewTokenExpiredError()
		case resp.StatusCode == 404:
			return nil, errors.NewAPIError(404, message, path)
		case resp.StatusCode == 429:
			err := errors.NewAPIError(429, "Rate limited", path)
			err.Retryable = true
			return nil, err
		case resp.StatusCode >= 500:
			return nil, errors.NewAPIError(resp.StatusCode, message, path)
		default:
			return nil, errors.NewAPIError(resp.StatusCode, message, path)
		}
	}

	return resp, nil
}
```

---

### Task 11.2: Error Formatting & Output (1.5h)

**Goal**: Create user-friendly error formatters with color/emoji support, actionable suggestions, and debug mode for stack traces.

**Estimated Time**: 1.5 hours

**Test-First Workflow**:

1. Write tests for formatting outputs (5 scenarios)
2. Run tests (expect failures)
3. Implement formatters
4. Verify outputs match expected format

**Test Scenarios** (5 required):

1. **Basic Format (User-Friendly Message)**:

   - Given: AuthError "Token expired"
   - When: Call `FormatError(err, false)`
   - Then: Returns "❌ Authentication token expired\n💡 Tip: Run 'emergent login' to re-authenticate"

2. **Format with Actionable Suggestions**:

   - Given: ResourceError for project "my-app" not found
   - When: Call `FormatError(err, false)`
   - Then: Returns "❌ Project 'my-app' not found\n💡 Tip: Run 'emergent projects list' to see available projects"

3. **Debug Mode (Stack Traces + Context)**:

   - Given: NetworkError with URL "https://api.emergent.dev/projects"
   - When: Call `FormatError(err, true)`
   - Then: Returns error message + context (URL, status) + full stack trace

4. **Color Stripping for Log Files**:

   - Given: Formatted error with ANSI color codes
   - When: Call `StripColors(formatted)`
   - Then: Returns plain text without escape sequences

5. **Multi-Error Aggregation**:
   - Given: 3 validation errors (missing name, invalid email, password too short)
   - When: Call `FormatMultipleErrors(errs, false)`
   - Then: Returns bullet list with each error + tips

**Implementation Guidance**:

**Formatter Core** (`internal/errors/formatter.go`):

```go
package errors

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// ANSI color codes
const (
	ColorRed     = "\033[31m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorReset   = "\033[0m"
)

// FormatError formats error for display to user
// debugMode=true includes stack traces and full context
func FormatError(err error, debugMode bool) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder

	// Error message with icon
	sb.WriteString(ColorRed + "❌ " + err.Error() + ColorReset + "\n")

	// Add actionable suggestion if available
	if suggestion := getSuggestion(err); suggestion != "" {
		sb.WriteString(ColorBlue + "💡 Tip: " + suggestion + ColorReset + "\n")
	}

	// Debug mode: add context and stack trace
	if debugMode {
		if cliErr, ok := err.(CLIError); ok && len(cliErr.Context()) > 0 {
			sb.WriteString("\n" + ColorYellow + "Context:" + ColorReset + "\n")
			for k, v := range cliErr.Context() {
				sb.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
			}
		}

		sb.WriteString("\n" + ColorYellow + "Stack Trace:" + ColorReset + "\n")
		sb.WriteString(string(debug.Stack()))
	}

	return sb.String()
}

// getSuggestion returns actionable guidance based on error type
func getSuggestion(err error) string {
	switch e := err.(type) {
	case *AuthError:
		if e.TokenExpired {
			return "Run 'emergent login' to re-authenticate"
		}
		return "Check your credentials and try again"

	case *NetworkError:
		if e.Timeout {
			return "Check your network connection and try again"
		}
		if e.DNSFailure {
			return "Verify the API URL is correct and accessible"
		}
		if e.StatusCode == 429:
			return "Too many requests. Wait a moment and try again"
		}
		if e.StatusCode >= 500:
			return "Server error. Try again in a moment"
		}
		return "Check your network connection"

	case *ConfigError:
		if e.NotFound {
			return "Run 'emergent init' to create a configuration file"
		}
		if e.Permission {
			return "Check file permissions for " + e.Path
		}
		if e.InvalidJSON {
			return "Verify configuration file syntax at " + e.Path
		}
		return "Check configuration file at " + e.Path

	case *ResourceError:
		switch e.ResourceType {
		case "project":
			return "Run 'emergent projects list' to see available projects"
		case "organization":
			return "Run 'emergent orgs list' to see available organizations"
		case "template":
			return "Run 'emergent templates list' to see available templates"
		}

	case *ValidationError:
		if e.Pattern != "" {
			return fmt.Sprintf("%s must match format: %s", e.Field, e.Pattern)
		}
		return fmt.Sprintf("Provide a valid value for %s", e.Field)
	}

	return ""
}

// FormatMultipleErrors formats a list of errors
func FormatMultipleErrors(errs []error, debugMode bool) string {
	if len(errs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(ColorRed + fmt.Sprintf("❌ %d errors occurred:\n" + ColorReset, len(errs)))

	for i, err := range errs {
		sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, err.Error()))
		if suggestion := getSuggestion(err); suggestion != "" {
			sb.WriteString("\n   " + ColorBlue + "💡 " + suggestion + ColorReset)
		}
		sb.WriteString("\n")
	}

	if debugMode {
		sb.WriteString("\n" + ColorYellow + "Run with --debug for full details" + ColorReset + "\n")
	}

	return sb.String()
}

// StripColors removes ANSI color codes for logging to files
func StripColors(text string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(text, "")
}

// FormatForLogging formats error for structured logging (JSON)
func FormatForLogging(err error) map[string]any {
	logEntry := map[string]any{
		"error": err.Error(),
	}

	if cliErr, ok := err.(CLIError); ok {
		logEntry["code"] = cliErr.Code()
		logEntry["context"] = cliErr.Context()
	}

	return logEntry
}
```

**Template System** (6 common scenarios):

```go
// Error message templates for consistency

var errorTemplates = map[string]string{
	"token_expired":       "Authentication token expired",
	"invalid_credentials": "Invalid credentials. Check your email and password",
	"insufficient_perms":  "You don't have permission to access this resource",
	"connection_refused":  "Cannot connect to API server at %s. Is it running?",
	"request_timeout":     "Request timed out after 30s. Check your network connection",
	"dns_failure":         "Cannot resolve hostname %s. Check your internet connection",
	"rate_limited":        "Too many requests. Retrying in %ds...",
	"server_error":        "Server error occurred. Try again in a moment",
	"invalid_format":      "%s must match format: %s",
	"missing_required":    "%s is required. Provide with --%s flag",
	"config_not_found":    "No configuration found. Run 'emergent init' to create one",
	"invalid_config":      "Configuration file is invalid. Check syntax at %s",
}

func GetTemplate(key string, args ...any) string {
	template, ok := errorTemplates[key]
	if !ok {
		return "Unknown error"
	}
	return fmt.Sprintf(template, args...)
}
```

**Deliverables Checklist**:

- [ ] `internal/errors/formatter.go` (~150 LOC)
  - [ ] `FormatError(err, debugMode)` function
  - [ ] `getSuggestion(err)` with 6 error type handlers
  - [ ] `FormatMultipleErrors(errs, debugMode)` for aggregation
  - [ ] `StripColors(text)` for log files
  - [ ] `FormatForLogging(err)` for structured JSON logs
  - [ ] Error message templates (12 common scenarios)
- [ ] `internal/errors/formatter_test.go` (~200 LOC)
  - [ ] 5 test scenarios (basic, suggestions, debug, strip, multi)
  - [ ] Verify color codes present in output
  - [ ] Verify suggestions match error types
  - [ ] Coverage ≥80%
- [ ] Verify: `go build ./internal/errors && go test ./internal/errors -v`

**Integration with Phase 6 (Commands)**:

Update all command files to use formatters:

```go
// cmd/login.go
func runLogin(cmd *cobra.Command, args []string) error {
	err := auth.Login()
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.FormatError(err, debugMode))
		return err
	}
	fmt.Println("✅ Login successful!")
	return nil
}

// cmd/root.go - add global debug flag
func init() {
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Show debug output with stack traces")
}
```

---

### Task 11.3: Error Recovery Patterns (1h)

**Goal**: Implement retry logic with exponential backoff, auth token refresh, and command integration for automatic recovery.

**Estimated Time**: 1 hour

**Test-First Workflow**:

1. Write tests for retry scenarios (3 tests)
2. Run tests (expect failures)
3. Implement retry/refresh logic
4. Verify retries work as expected

**Test Scenarios** (3 required):

1. **Retry Success After Transient Network Failure**:

   - Given: Function that fails twice then succeeds
   - When: Call `RetryWithBackoff(fn, 3, 1*time.Second)`
   - Then: Function executes 3 times, returns success on attempt 3
   - And: Delays between attempts: 1s, 2s

2. **Auth Token Refresh on 401**:

   - Given: API call returns 401 with TokenExpiredError
   - When: Retry handler detects error
   - Then: Calls `auth.RefreshToken()`, retries API call
   - And: Second attempt succeeds with new token

3. **Max Retry Exceeded with Clear Message**:
   - Given: Function fails 5 times with NetworkError
   - When: Call `RetryWithBackoff(fn, 3, 1*time.Second)`
   - Then: Returns error "Max retries (3) exceeded: Network error: ..."
   - And: Suggests user check connection

**Implementation Guidance**:

**Retry Core** (`internal/errors/recovery.go`):

```go
package errors

import (
	"fmt"
	"time"
)

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	OnRetry     func(attempt int, err error) // Callback before each retry
}

// DefaultRetryConfig for most operations
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   1 * time.Second,
	MaxDelay:    30 * time.Second,
}

// RetryWithBackoff retries fn with exponential backoff
func RetryWithBackoff(fn func() error, config RetryConfig) error {
	var lastErr error

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		err := fn()

		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if retryable
		if !IsRetryable(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Max attempts reached
		if attempt == config.MaxAttempts {
			return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxAttempts, err)
		}

		// Calculate delay with exponential backoff
		delay := config.BaseDelay * time.Duration(1<<(attempt-1))
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}

		// Callback before retry
		if config.OnRetry != nil {
			config.OnRetry(attempt, err)
		}

		time.Sleep(delay)
	}

	return lastErr
}

// IsRetryable checks if error should be retried
func IsRetryable(err error) bool {
	switch e := err.(type) {
	case *NetworkError:
		return e.Timeout || e.StatusCode == 0 || e.StatusCode >= 500
	case *APIError:
		return e.Retryable
	default:
		return false
	}
}

// RefreshTokenOnAuth wraps function with automatic token refresh
func RefreshTokenOnAuth(fn func() error, refreshFn func() error) error {
	err := fn()

	if authErr, ok := err.(*AuthError); ok && authErr.TokenExpired {
		// Try refreshing token
		if refreshErr := refreshFn(); refreshErr != nil {
			return fmt.Errorf("token refresh failed: %w", refreshErr)
		}

		// Retry original function
		return fn()
	}

	return err
}
```

**Command Integration Example**:

```go
// cmd/projects.go - list projects with retry
func listProjects(cmd *cobra.Command, args []string) error {
	config := errors.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		MaxDelay:    10 * time.Second,
		OnRetry: func(attempt int, err error) {
			fmt.Printf("⚠️  Attempt %d failed, retrying...\n", attempt)
		},
	}

	err := errors.RetryWithBackoff(func() error {
		projects, err := client.ListProjects()
		if err != nil {
			return err
		}

		// Display projects
		for _, p := range projects {
			fmt.Printf("- %s (%s)\n", p.Name, p.ID)
		}
		return nil
	}, config)

	if err != nil {
		fmt.Fprintln(os.Stderr, errors.FormatError(err, debugMode))
		return err
	}

	return nil
}
```

**Deliverables Checklist**:

- [ ] `internal/errors/recovery.go` (~100 LOC)
  - [ ] `RetryConfig` struct
  - [ ] `RetryWithBackoff(fn, config)` function
  - [ ] `IsRetryable(err)` logic (network timeouts, 5xx, rate limits)
  - [ ] `RefreshTokenOnAuth(fn, refreshFn)` wrapper
  - [ ] Exponential backoff calculation
- [ ] `internal/errors/recovery_test.go` (~150 LOC)
  - [ ] 3 test scenarios (success, refresh, exceeded)
  - [ ] Mock functions that fail/succeed deterministically
  - [ ] Verify backoff timing (delays match expected)
  - [ ] Coverage ≥80%
- [ ] Update `cmd/login.go` to use retry on network failures
- [ ] Update `internal/client/client.go` to integrate token refresh
- [ ] Verify: `go build ./cmd/emergent && go test ./internal/errors -v`

**Retry Decision Tree**:

```
Error Type                               → Retry?   Max Attempts   Backoff
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
NetworkError (timeout)                   → Yes      3              1s, 2s, 4s
NetworkError (connection refused)        → Yes      3              1s, 2s, 4s
APIError (429 rate limit)                → Yes      5              2s, 4s, 8s, 16s, 30s
APIError (500-599 server error)          → Yes      3              1s, 2s, 4s
APIError (400-499 client error)          → No       -              -
AuthError (token expired)                → Yes*     1              Immediate (refresh)
ValidationError                          → No       -              -
ConfigError                              → No       -              -
ResourceError (not found)                → No       -              -
```

\*Only retry after successful token refresh

---

## Phase 11 Summary

**Total Time**: 4.5 hours (2h + 1.5h + 1h)

**Total Deliverables**: 6 files (~1,000 LOC implementation + tests)

- `internal/errors/errors.go` (~200 LOC)
- `internal/errors/errors_test.go` (~200 LOC)
- `internal/errors/formatter.go` (~150 LOC)
- `internal/errors/formatter_test.go` (~200 LOC)
- `internal/errors/recovery.go` (~100 LOC)
- `internal/errors/recovery_test.go` (~150 LOC)

**Architecture Patterns**:

- **Error Wrapping**: Go 1.13+ `Unwrap()` for error chains
- **Type Checking**: `errors.Is()` and `errors.As()` for safe type assertions
- **Context Preservation**: Store URL, status codes, file paths, etc.
- **User-First Messaging**: Hide technical jargon, show actionable steps
- **Debug Mode**: Full details only when explicitly requested
- **Automatic Retry**: Exponential backoff for transient failures
- **Token Refresh**: Transparent re-authentication on expiry

**Dependencies**:

- **Phase 3 (HTTP Client)**: Returns typed errors from API calls
- **Phase 4 (Config & Auth)**: Uses ConfigError/AuthError appropriately
- **Phase 6 (Commands)**: All commands format errors before display
- **Phase 10 (Prompts)**: Handles ErrCancelled gracefully

**Error Message Examples**:

1. **Token Expired**:

   ```
   ❌ Authentication token expired
   💡 Tip: Run 'emergent login' to re-authenticate
   ```

2. **Project Not Found**:

   ```
   ❌ Project 'my-app' not found
   💡 Tip: Run 'emergent projects list' to see available projects
   ```

3. **Network Timeout** (with retry):

   ```
   ⚠️  Attempt 1 failed, retrying...
   ⚠️  Attempt 2 failed, retrying...
   ✅ Connected successfully!
   ```

4. **Rate Limited** (auto-retry):

   ```
   ⚠️  Too many requests. Retrying in 2s...
   ⚠️  Too many requests. Retrying in 4s...
   ✅ Request completed!
   ```

5. **Server Error**:

   ```
   ❌ Server error occurred (500)
   💡 Tip: Try again in a moment
   ```

6. **Invalid Input**:
   ```
   ❌ email must match format: user@example.com
   💡 Tip: Provide a valid email address
   ```

**Verification Strategy**:

- Unit tests: All error types, formatters, retry logic
- Integration tests: Commands use formatters correctly
- Manual testing: Verify output formatting and colors
- Error scenarios: Test each error type with curl/CLI
- Retry testing: Simulate network failures and recovery

**Success Metrics**:

- ≥80% test coverage on all error files
- All commands use typed errors (no generic `error` returns)
- User-facing errors include actionable suggestions
- Debug mode provides full context without overwhelming
- Automatic retry works for transient failures (network, rate limits)
- Token refresh transparent to user

**Next Phase Preview**: Phase 12 (Testing) will verify error handling across all components, including edge cases like simultaneous token expiry and network failure.

## Phase 12: Testing

**Goal**: Achieve ≥80% test coverage with comprehensive unit tests, integration tests with mock API server, and E2E tests against dev server. Ensure CLI reliability through table-driven tests, mock isolation, and realistic failure scenarios.

**Total Phase Duration**: ~6 hours

**Why This Matters**:

- High test coverage prevents regressions during refactoring (e.g., changing HTTP client library)
- Mock API server enables offline development and fast feedback loops (<100ms test runs)
- E2E tests validate full authentication flows, ensuring token refresh and error handling work in production scenarios
- Table-driven tests document expected behaviors and edge cases as executable specifications
- Coverage tracking highlights untested code paths before they cause production issues

**Integration Points**:

- Phase 3 (HTTP Client): Mock HTTP responses for network failure scenarios
- Phase 4 (Auth): Test token expiry, refresh flows, keyring fallback
- Phase 5 (Output): Verify table/JSON/YAML formatting with edge cases (empty, truncated, unicode)
- Phase 6 (Commands): Test command parsing, flag validation, subcommand routing
- Phase 11 (Errors): Verify error types are correctly identified and formatted

### Task 12.1: Unit Tests (2.5h)

**Goal**: Test individual components in isolation (config, credentials, token cache, output formatters, command parsing) using table-driven patterns with subtests.

**Estimated Time**: 2.5 hours

**Test-First Workflow**:

1. Write table-driven test scenarios (5+ cases per component)
2. Create mock implementations for external dependencies (filesystem, keyring, HTTP)
3. Run tests (expect failures)
4. Implement tested components
5. Verify ≥80% coverage per module

**Test Scenarios** (23 required across 5 components):

**Config Loading (5 cases)**:

1. **Valid JSON Config**:

   - Given: File contains `{"theme":"dark","output":"json"}`
   - When: Load config from path
   - Then: Returns `Config{Theme: "dark", Output: "json"}`, no error

2. **Invalid JSON**:

   - Given: File contains `{invalid json`
   - When: Load config from path
   - Then: Returns nil, `ConfigError` with `InvalidJSON: true`

3. **Missing Config File**:

   - Given: Config file does not exist
   - When: Load config from path
   - Then: Returns default config (no error, creates file)

4. **Permission Denied**:

   - Given: Config file exists but is not readable
   - When: Load config from path
   - Then: Returns nil, `ConfigError` with `Permission: true`

5. **Partial Config (Defaults)**:
   - Given: File contains `{"theme":"light"}` (missing other fields)
   - When: Load config from path
   - Then: Returns `Config{Theme: "light", Output: "table"}` (fills defaults)

**Credential Storage (4 cases)**:

1. **Save and Load Credentials**:

   - Given: Empty keyring
   - When: Save API token "test-token-abc123"
   - And: Load credentials
   - Then: Returns "test-token-abc123", no error

2. **Encryption on Save**:

   - Given: Keyring stores encrypted data
   - When: Save token "secret-token"
   - Then: Stored value is base64-encoded ciphertext (not plaintext)

3. **Keyring Not Available (Fallback)**:

   - Given: Keyring service fails (e.g., no D-Bus on Linux)
   - When: Save token "fallback-token"
   - Then: Falls back to encrypted file at `~/.emergent/credentials.enc`, no error

4. **Load Non-Existent Credentials**:
   - Given: Keyring empty, no fallback file
   - When: Load credentials
   - Then: Returns "", `AuthError` with "No credentials stored"

**Token Cache (4 cases)**:

1. **Cache and Retrieve Token**:

   - Given: Empty cache
   - When: Save token with 1-hour expiry
   - And: Load token immediately
   - Then: Returns cached token, `Expired: false`

2. **Expired Token Detection**:

   - Given: Token cached 2 hours ago with 1-hour TTL
   - When: Load token
   - Then: Returns token, `Expired: true`

3. **Token Refresh Updates Cache**:

   - Given: Expired token in cache
   - When: Refresh token (get new token from API)
   - And: Update cache with new token
   - Then: Load returns new token, `Expired: false`

4. **Cache File Corrupted**:
   - Given: Cache file contains malformed JSON
   - When: Load token
   - Then: Returns empty, treats as cache miss (no error)

**Output Formatters (5 cases)**:

1. **Table Format - Normal Rows**:

   - Given: Data `[{Name:"Project A", Status:"Active"}, {Name:"Project B", Status:"Archived"}]`
   - When: Format as table with columns `["Name", "Status"]`
   - Then: Returns aligned table with headers and 2 rows

2. **JSON Format - Empty Results**:

   - Given: Empty slice `[]`
   - When: Format as JSON
   - Then: Returns `[]`, not error or null

3. **YAML Format - Nested Objects**:

   - Given: Data `{Org: {ID: "org-1", Name: "Acme"}, Projects: [{ID: "p1"}]}`
   - When: Format as YAML
   - Then: Returns indented YAML with nested structure

4. **Table Format - Long Text Truncation**:

   - Given: Cell value is 200-character string
   - When: Format as table with max width 50
   - Then: Truncates cell to 47 chars + "..." (50 total)

5. **Table Format - Unicode Handling**:
   - Given: Row contains emoji and CJK characters: `{Name: "项目 🚀", Count: 42}`
   - When: Format as table
   - Then: Aligns correctly (counts unicode width, not byte length)

**Command Parsing (5 cases)**:

1. **Valid Command with Flags**:

   - Given: Args `["projects", "list", "--org=org-1", "--format=json"]`
   - When: Parse command
   - Then: Returns `Command: "projects list"`, `Flags: {Org: "org-1", Format: "json"}`

2. **Unknown Flag**:

   - Given: Args `["projects", "create", "--invalid-flag"]`
   - When: Parse command
   - Then: Returns `ValidationError` with "unknown flag: --invalid-flag"

3. **Missing Required Argument**:

   - Given: Command `projects create` requires `--name` flag
   - When: Args `["projects", "create"]` (missing --name)
   - Then: Returns `ValidationError` with "required flag: --name"

4. **Subcommand Routing**:

   - Given: Root command has subcommands `login`, `projects`, `templates`
   - When: Args `["projects", "create", "--name=Test"]`
   - Then: Routes to `ProjectsCmd.Create()` handler

5. **Help Flag Bypasses Validation**:
   - Given: Command requires `--name` flag
   - When: Args `["projects", "create", "--help"]`
   - Then: Prints help text, does not validate required flags

**Implementation Guidance**:

**Table-Driven Test Pattern** (`internal/config/config_test.go`):

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergentmethods/emergent-cli/internal/config"
	"github.com/emergentmethods/emergent-cli/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		fileContent string
		wantConfig  *config.Config
		wantErr     bool
		errType     error
	}{
		{
			name:        "valid JSON config",
			fileContent: `{"theme":"dark","output":"json"}`,
			wantConfig:  &config.Config{Theme: "dark", Output: "json"},
			wantErr:     false,
		},
		{
			name:        "invalid JSON",
			fileContent: `{invalid json`,
			wantConfig:  nil,
			wantErr:     true,
			errType:     &errors.ConfigError{InvalidJSON: true},
		},
		{
			name:        "missing file (creates default)",
			fileContent: "", // File doesn't exist
			wantConfig:  config.DefaultConfig(),
			wantErr:     false,
		},
		{
			name:        "partial config with defaults",
			fileContent: `{"theme":"light"}`,
			wantConfig:  &config.Config{Theme: "light", Output: "table"}, // Output defaults to "table"
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: Create temp directory for test config
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.json")

			if tt.fileContent != "" {
				err := os.WriteFile(configPath, []byte(tt.fileContent), 0600)
				require.NoError(t, err)
			}

			// Execute: Load config
			cfg, err := config.Load(configPath)

			// Verify: Check error type and result
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorAs(t, err, &tt.errType)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantConfig, cfg)
			}
		})
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		Theme:  "dark",
		Output: "json",
		APIBaseURL: "https://api.dev.emergent-company.ai",
	}

	// Save config
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	// Verify file exists and contains correct JSON
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"theme":"dark"`)
	assert.Contains(t, string(data), `"output":"json"`)
}
```

**Mock Filesystem Pattern** (`internal/config/mock_fs.go`):

```go
package config

import "errors"

// MockFS implements filesystem operations for testing
type MockFS struct {
	Files       map[string][]byte // path -> content
	Permissions map[string]int    // path -> chmod mode
	ReadError   error             // Simulate read failure
	WriteError  error             // Simulate write failure
}

func NewMockFS() *MockFS {
	return &MockFS{
		Files:       make(map[string][]byte),
		Permissions: make(map[string]int),
	}
}

func (m *MockFS) ReadFile(path string) ([]byte, error) {
	if m.ReadError != nil {
		return nil, m.ReadError
	}
	data, ok := m.Files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	// Check permissions
	mode, ok := m.Permissions[path]
	if ok && mode&0400 == 0 { // Not readable
		return nil, errors.New("permission denied")
	}
	return data, nil
}

func (m *MockFS) WriteFile(path string, data []byte, perm int) error {
	if m.WriteError != nil {
		return m.WriteError
	}
	m.Files[path] = data
	m.Permissions[path] = perm
	return nil
}

func (m *MockFS) Exists(path string) bool {
	_, ok := m.Files[path]
	return ok
}
```

**Mock Keyring Pattern** (`internal/auth/mock_keyring.go`):

```go
package auth

import "errors"

// MockKeyring implements keyring.Keyring for testing
type MockKeyring struct {
	Items      map[string]string // service+user -> password
	FailSave   bool              // Simulate save failure
	FailGet    bool              // Simulate get failure
	FailDelete bool              // Simulate delete failure
}

func NewMockKeyring() *MockKeyring {
	return &MockKeyring{
		Items: make(map[string]string),
	}
}

func (m *MockKeyring) Set(service, user, password string) error {
	if m.FailSave {
		return errors.New("keyring: save failed")
	}
	key := service + ":" + user
	m.Items[key] = password
	return nil
}

func (m *MockKeyring) Get(service, user string) (string, error) {
	if m.FailGet {
		return "", errors.New("keyring: get failed")
	}
	key := service + ":" + user
	pwd, ok := m.Items[key]
	if !ok {
		return "", errors.New("keyring: not found")
	}
	return pwd, nil
}

func (m *MockKeyring) Delete(service, user string) error {
	if m.FailDelete {
		return errors.New("keyring: delete failed")
	}
	key := service + ":" + user
	delete(m.Items, key)
	return nil
}
```

**Coverage Verification**:

```bash
# Run tests with coverage
go test ./internal/config -cover -coverprofile=config.cover
go test ./internal/auth -cover -coverprofile=auth.cover
go test ./internal/output -cover -coverprofile=output.cover
go test ./cmd -cover -coverprofile=cmd.cover

# View coverage reports
go tool cover -html=config.cover
go tool cover -html=auth.cover

# Check coverage percentages
go test ./... -cover | grep "coverage:"
# Should show ≥80% for each module
```

**Deliverables**:

- [ ] `internal/config/config_test.go` (5 test scenarios, table-driven)
- [ ] `internal/config/mock_fs.go` (mock filesystem for config tests)
- [ ] `internal/auth/credentials_test.go` (4 test scenarios)
- [ ] `internal/auth/mock_keyring.go` (mock keyring implementation)
- [ ] `internal/auth/token_cache_test.go` (4 test scenarios)
- [ ] `internal/output/formatters_test.go` (5 test scenarios: table, JSON, YAML, truncation, unicode)
- [ ] `cmd/root_test.go` (5 command parsing scenarios)
- [ ] Coverage reports showing ≥80% for each module

**Integration Points**:

- Phase 4 (Auth): Credential storage uses keyring with fallback; tests verify both paths
- Phase 5 (Output): Formatters handle edge cases (empty, long text, unicode) correctly
- Phase 6 (Commands): Command parsing validates flags and routes to correct handlers
- Phase 11 (Errors): Tests verify error types are correctly wrapped and unwrapped

### Task 12.2: Integration Tests (2h)

**Goal**: Test components working together with mock HTTP API server, verifying full workflows (login → list → create) and error recovery paths.

**Estimated Time**: 2 hours

**Test-First Workflow**:

1. Create mock HTTP server responding to API endpoints
2. Write test scenarios for multi-step workflows (5 scenarios)
3. Run tests (expect failures)
4. Implement integration between components (HTTP client + config + auth)
5. Verify tests pass with realistic API interactions

**Test Scenarios** (10 required across 5 workflows):

**Mock API Server (2 cases)**:

1. **Successful Login Flow**:

   - Given: Mock server at `http://localhost:9999`
   - And: POST `/auth/login` returns `{"access_token":"mock-token-abc123","expires_in":3600}`
   - When: Run `emergent login --email=test@example.com --password=secret`
   - Then: Saves token to keyring, saves expiry to cache, returns success

2. **API Error Handling**:
   - Given: Mock server at `http://localhost:9999`
   - And: POST `/auth/login` returns 401 with `{"error":"Invalid credentials"}`
   - When: Run `emergent login --email=wrong@example.com --password=wrong`
   - Then: Returns `AuthError`, does not save credentials, prints actionable error message

**Full Login Flow (2 cases)**:

1. **First Login (No Cached Token)**:

   - Given: No saved credentials, no cached token
   - When: User provides email/password
   - Then: HTTP client POSTs to `/auth/login`, receives token, saves to keyring and cache, updates config with user info

2. **Login with Expired Token Refresh**:
   - Given: Cached token expired 1 hour ago
   - When: User runs `emergent projects list`
   - Then: HTTP client detects expiry, POSTs to `/auth/refresh`, gets new token, updates cache, retries original request

**Project Creation (2 cases)**:

1. **Create Project Successfully**:

   - Given: Authenticated with valid token
   - When: Run `emergent projects create --name="Test Project" --org=org-1`
   - Then: POSTs to `/projects` with body `{"name":"Test Project","org_id":"org-1"}`, receives `{"id":"proj-abc","name":"Test Project"}`, prints success message

2. **Create Project - Org Not Found**:
   - Given: Authenticated with valid token
   - When: Run `emergent projects create --name="Test" --org=invalid-org`
   - Then: Server returns 404 `{"error":"Organization not found"}`, CLI prints `ResourceError` with "Organization 'invalid-org' not found"

**Config Persistence (2 cases)**:

1. **Config Survives Restarts**:

   - Given: User sets `emergent config set theme dark`
   - When: CLI exits and restarts
   - Then: Load config from `~/.emergent/config.json`, theme is still "dark"

2. **Config Merge with Defaults**:
   - Given: Config file missing `api_base_url` field
   - When: Load config
   - Then: Merges with defaults, uses `https://api.emergent-company.ai` (production URL)

**Error Recovery (2 cases)**:

1. **Network Timeout with Retry**:

   - Given: Mock server delays response by 35 seconds (exceeds 30s timeout)
   - When: Run `emergent projects list`
   - Then: HTTP client times out, retries once (exponential backoff), returns `NetworkError` with "Request timed out after 30s"

2. **Server 5xx with Retry**:
   - Given: Mock server returns 503 Service Unavailable
   - When: Run `emergent projects list`
   - Then: HTTP client retries 3 times (with backoff), returns `APIError` with "Server unavailable (status 503), please try again later"

**Implementation Guidance**:

**Mock HTTP Server Setup** (`tests/integration/mock_server.go`):

```go
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"
)

// MockServer wraps httptest.Server with helper methods
type MockServer struct {
	*httptest.Server
	Handlers map[string]http.HandlerFunc
	Requests []MockRequest // Log of received requests
}

type MockRequest struct {
	Method string
	Path   string
	Body   string
	Headers http.Header
	Time   time.Time
}

func NewMockServer() *MockServer {
	ms := &MockServer{
		Handlers: make(map[string]http.HandlerFunc),
		Requests: []MockRequest{},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log request
		body, _ := io.ReadAll(r.Body)
		ms.Requests = append(ms.Requests, MockRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Body:    string(body),
			Headers: r.Header,
			Time:    time.Now(),
		})

		// Route to registered handler
		key := r.Method + " " + r.URL.Path
		if h, ok := ms.Handlers[key]; ok {
			h(w, r)
			return
		}

		// Default: 404
		http.NotFound(w, r)
	})

	ms.Server = httptest.NewServer(handler)
	return ms
}

func (ms *MockServer) RegisterHandler(method, path string, handler http.HandlerFunc) {
	ms.Handlers[method+" "+path] = handler
}

// Helper: JSON response
func (ms *MockServer) JSONResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Helper: Login endpoint (returns mock token)
func (ms *MockServer) MockLogin(email, password string) {
	ms.RegisterHandler("POST", "/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.Email == email && req.Password == password {
			ms.JSONResponse(w, 200, map[string]any{
				"access_token": "mock-token-abc123",
				"expires_in":   3600,
				"user": map[string]string{
					"id":    "user-1",
					"email": email,
				},
			})
		} else {
			ms.JSONResponse(w, 401, map[string]string{
				"error": "Invalid credentials",
			})
		}
	})
}

// Helper: Projects list endpoint
func (ms *MockServer) MockProjectsList(projects []map[string]any) {
	ms.RegisterHandler("GET", "/projects", func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header
		token := r.Header.Get("Authorization")
		if token == "" || token != "Bearer mock-token-abc123" {
			ms.JSONResponse(w, 401, map[string]string{"error": "Unauthorized"})
			return
		}

		ms.JSONResponse(w, 200, map[string]any{
			"projects": projects,
		})
	})
}
```

**Integration Test Example** (`tests/integration/login_test.go`):

```go
package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergentmethods/emergent-cli/cmd"
	"github.com/emergentmethods/emergent-cli/internal/config"
	"github.com/emergentmethods/emergent-cli/tests/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullLoginFlow(t *testing.T) {
	// Setup: Mock server + temp config directory
	server := integration.NewMockServer()
	defer server.Close()

	server.MockLogin("test@example.com", "secret123")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Override config path for test
	os.Setenv("EMERGENT_CONFIG_PATH", configPath)
	os.Setenv("EMERGENT_API_URL", server.URL)
	defer os.Unsetenv("EMERGENT_CONFIG_PATH")
	defer os.Unsetenv("EMERGENT_API_URL")

	// Execute: Run login command
	rootCmd := cmd.NewRootCmd()
	rootCmd.SetArgs([]string{"login", "--email=test@example.com", "--password=secret123"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify: Config file created with user info
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", cfg.User.Email)
	assert.Equal(t, "user-1", cfg.User.ID)

	// Verify: Token saved to keyring (mock)
	// (Would use actual keyring mock here)

	// Verify: HTTP request logged
	assert.Len(t, server.Requests, 1)
	assert.Equal(t, "POST", server.Requests[0].Method)
	assert.Equal(t, "/auth/login", server.Requests[0].Path)
	assert.Contains(t, server.Requests[0].Body, `"email":"test@example.com"`)
}

func TestProjectCreation(t *testing.T) {
	// Setup
	server := integration.NewMockServer()
	defer server.Close()

	server.RegisterHandler("POST", "/projects", func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		assert.Equal(t, "Bearer mock-token-abc123", r.Header.Get("Authorization"))

		// Parse request body
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "Test Project", req["name"])
		assert.Equal(t, "org-1", req["org_id"])

		// Return created project
		server.JSONResponse(w, 201, map[string]any{
			"id":     "proj-abc",
			"name":   req["name"],
			"org_id": req["org_id"],
		})
	})

	// Setup config with saved token
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := &config.Config{
		APIBaseURL: server.URL,
		User: config.UserInfo{
			ID:    "user-1",
			Email: "test@example.com",
		},
	}
	config.Save(cfg, configPath)

	// Mock token in keyring/cache
	// (Set up mock credential storage)

	os.Setenv("EMERGENT_CONFIG_PATH", configPath)
	defer os.Unsetenv("EMERGENT_CONFIG_PATH")

	// Execute: Create project
	rootCmd := cmd.NewRootCmd()
	rootCmd.SetArgs([]string{"projects", "create", "--name=Test Project", "--org=org-1"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify: POST request sent
	assert.Len(t, server.Requests, 1)
	assert.Equal(t, "POST", server.Requests[0].Method)
}
```

**Test Data Fixtures** (`tests/integration/fixtures/`):

```
fixtures/
├── valid-config.json          # Sample config for tests
├── expired-token.json         # Token cache with expired timestamp
├── projects-response.json     # Sample API response for /projects
└── orgs-response.json         # Sample API response /orgs
```

**Deliverables**:

- [ ] `tests/integration/mock_server.go` (HTTP test server with helper methods)
- [ ] `tests/integration/login_test.go` (2 login flow scenarios)
- [ ] `tests/integration/projects_test.go` (2 project creation scenarios)
- [ ] `tests/integration/config_test.go` (2 config persistence scenarios)
- [ ] `tests/integration/error_recovery_test.go` (2 retry scenarios)
- [ ] `tests/integration/fixtures/` directory with sample API responses
- [ ] Integration test runner script: `make test-integration`

**Integration Points**:

- Phase 3 (HTTP Client): Mock server returns realistic API responses with delays/errors
- Phase 4 (Auth): Full login flow saves token, refresh flow updates cache
- Phase 6 (Commands): Commands execute against mock server, parse responses correctly
- Phase 11 (Errors): Network/API errors trigger correct retry logic

### Task 12.3: E2E Tests (1.5h)

**Goal**: Test full CLI against real dev server (`https://api.dev.emergent-company.ai`) with actual HTTP requests, authentication, and database state changes.

**Estimated Time**: 1.5 hours

**Test-First Workflow**:

1. Write E2E test scenarios (4 scenarios)
2. Set up test user credentials in dev environment
3. Run tests against dev server (expect failures initially)
4. Fix any issues exposed by E2E tests
5. Add cleanup logic (delete created resources after tests)

**Test Scenarios** (4 required):

**Login → List → Create Flow (1 scenario)**:

1. **Full User Journey**:
   - Given: Dev server is accessible at `https://api.dev.emergent-company.ai`
   - And: Test user credentials: `test-cli@emergent.ai` / `E2E_TEST_PASSWORD` (from env)
   - When: Run `emergent login --email=test-cli@emergent.ai --password=$E2E_TEST_PASSWORD`
   - Then: Receives access token, saves to keyring
   - When: Run `emergent projects list`
   - Then: Receives list of projects (may be empty), prints table
   - When: Run `emergent projects create --name="E2E Test Project"`
   - Then: Creates project on server, returns project ID
   - When: Run `emergent projects list` again
   - Then: New project appears in list
   - **Cleanup**: Delete created project with `emergent projects delete <id>`

**Code Generation (1 scenario)**:

1. **Generate TypeScript Types**:
   - Given: Authenticated user with project "E2E Test Project"
   - When: Run `emergent codegen ts --project="E2E Test Project" --output=./generated`
   - Then: Generates TypeScript files in `./generated/` directory
   - And: Files contain interface definitions matching GraphQL schema
   - **Cleanup**: Remove `./generated/` directory

**MCP Server Lifecycle (1 scenario)**:

1. **Start and Stop MCP Server**:
   - Given: Authenticated user
   - When: Run `emergent mcp start --port=3456` (background process)
   - Then: MCP server starts, listens on port 3456
   - When: Check process with `ps aux | grep emergent-mcp`
   - Then: Process is running
   - When: Run `emergent mcp stop`
   - Then: Process terminates gracefully, port 3456 is freed
   - **Cleanup**: Ensure MCP server is stopped even if test fails

**Error Handling (1 scenario)**:

1. **Invalid Credentials**:
   - Given: Dev server accessible
   - When: Run `emergent login --email=invalid@example.com --password=wrongpass`
   - Then: Server returns 401, CLI prints "Invalid credentials"
   - And: CLI suggests "Check your email/password and try again"
   - And: No token is saved to keyring
   - When: Run `emergent projects list` (without authentication)
   - Then: CLI prints "Not authenticated. Run 'emergent login' first"

**Implementation Guidance**:

**E2E Test Runner** (`tests/e2e/run.sh`):

```bash
#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "Running E2E tests against dev server..."

# Check environment
if [ -z "$E2E_TEST_PASSWORD" ]; then
    echo "${RED}ERROR: E2E_TEST_PASSWORD not set${NC}"
    exit 1
fi

# Build CLI binary
echo "Building CLI..."
go build -o ./bin/emergent-test ./cmd/emergent

# Set test config path (isolated from real config)
export EMERGENT_CONFIG_PATH="$HOME/.emergent-test/config.json"
mkdir -p "$HOME/.emergent-test"

# Run test scenarios
echo ""
echo "Test 1: Login → List → Create Flow"
./bin/emergent-test login --email="test-cli@emergent.ai" --password="$E2E_TEST_PASSWORD"

echo "Listing projects..."
./bin/emergent-test projects list

echo "Creating test project..."
PROJECT_ID=$(./bin/emergent-test projects create --name="E2E Test $(date +%s)" --format=json | jq -r '.id')
echo "Created project: $PROJECT_ID"

echo "Verifying project appears in list..."
./bin/emergent-test projects list | grep "$PROJECT_ID" || (echo "${RED}FAIL: Project not in list${NC}" && exit 1)

echo "Cleanup: Deleting test project..."
./bin/emergent-test projects delete "$PROJECT_ID"

echo ""
echo "${GREEN}✓ Test 1 passed${NC}"

# Test 2: Code generation
echo ""
echo "Test 2: Code Generation"
# (Similar pattern for codegen test)

# Test 3: MCP lifecycle
echo ""
echo "Test 3: MCP Server Lifecycle"
# (Similar pattern for MCP test)

# Test 4: Error handling
echo ""
echo "Test 4: Error Handling"
./bin/emergent-test login --email="invalid@example.com" --password="wrongpass" 2>&1 | grep "Invalid credentials" || (echo "${RED}FAIL: Expected 'Invalid credentials' error${NC}" && exit 1)
echo "${GREEN}✓ Test 4 passed${NC}"

# Cleanup
rm -rf "$HOME/.emergent-test"
rm ./bin/emergent-test

echo ""
echo "${GREEN}All E2E tests passed!${NC}"
```

**E2E Test in Go** (`tests/e2e/full_flow_test.go`):

```go
// +build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginListCreate(t *testing.T) {
	// Skip if not running E2E tests
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Setup: Build CLI binary
	buildCmd := exec.Command("go", "build", "-o", "./bin/emergent-test", "./cmd/emergent")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build CLI")
	defer os.Remove("./bin/emergent-test")

	// Setup: Test config directory
	configDir := t.TempDir()
	os.Setenv("EMERGENT_CONFIG_PATH", configDir+"/config.json")
	defer os.Unsetenv("EMERGENT_CONFIG_PATH")

	// Test 1: Login
	email := "test-cli@emergent.ai"
	password := os.Getenv("E2E_TEST_PASSWORD")
	require.NotEmpty(t, password, "E2E_TEST_PASSWORD must be set")

	loginCmd := exec.Command("./bin/emergent-test", "login",
		"--email="+email,
		"--password="+password)
	output, err := loginCmd.CombinedOutput()
	require.NoError(t, err, "Login failed: %s", string(output))
	assert.Contains(t, string(output), "Logged in successfully")

	// Test 2: List projects
	listCmd := exec.Command("./bin/emergent-test", "projects", "list")
	output, err = listCmd.CombinedOutput()
	require.NoError(t, err, "List failed: %s", string(output))
	// May be empty list initially

	// Test 3: Create project
	projectName := "E2E Test " + time.Now().Format("20060102-150405")
	createCmd := exec.Command("./bin/emergent-test", "projects", "create",
		"--name="+projectName,
		"--format=json")
	output, err = createCmd.CombinedOutput()
	require.NoError(t, err, "Create failed: %s", string(output))

	// Extract project ID from JSON response
	projectID := extractProjectID(t, string(output))
	require.NotEmpty(t, projectID)

	// Test 4: Verify project in list
	listCmd = exec.Command("./bin/emergent-test", "projects", "list")
	output, err = listCmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), projectID, "Created project not in list")

	// Cleanup: Delete project
	deleteCmd := exec.Command("./bin/emergent-test", "projects", "delete", projectID)
	output, err = deleteCmd.CombinedOutput()
	require.NoError(t, err, "Delete failed: %s", string(output))
}

func extractProjectID(t *testing.T, jsonOutput string) string {
	// Parse JSON to extract "id" field
	// (Use json.Unmarshal or simple string parsing)
	start := strings.Index(jsonOutput, `"id":"`)
	if start == -1 {
		return ""
	}
	start += 6 // len(`"id":"`)
	end := strings.Index(jsonOutput[start:], `"`)
	if end == -1 {
		return ""
	}
	return jsonOutput[start : start+end]
}
```

**CI/CD Integration** (`.github/workflows/e2e-tests.yml`):

```yaml
name: E2E Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run E2E Tests
        env:
          E2E_TEST_PASSWORD: ${{ secrets.E2E_TEST_PASSWORD }}
        run: |
          chmod +x tests/e2e/run.sh
          ./tests/e2e/run.sh

      - name: Upload logs on failure
        if: failure()
        uses: actions/upload-artifact@v3
        with:
          name: e2e-logs
          path: ~/.emergent-test/
```

**Deliverables**:

- [ ] `tests/e2e/run.sh` (Bash script running all E2E scenarios)
- [ ] `tests/e2e/full_flow_test.go` (Go E2E test: login → list → create)
- [ ] `tests/e2e/codegen_test.go` (Code generation E2E test)
- [ ] `tests/e2e/mcp_test.go` (MCP server lifecycle E2E test)
- [ ] `tests/e2e/error_handling_test.go` (Error scenario E2E test)
- [ ] `.github/workflows/e2e-tests.yml` (CI workflow for E2E tests)
- [ ] `docs/E2E_TESTING.md` (Guide for running E2E tests locally and in CI)

**Integration Points**:

- Phase 3 (HTTP Client): Real HTTP requests to dev server with actual network conditions
- Phase 4 (Auth): Full OAuth flow with real Zitadel tokens
- Phase 6 (Commands): All commands execute against real API, verify responses
- Phase 11 (Errors): Test how CLI handles real server errors (503, 401, timeouts)

### Task 12.4: Real Server Integration Tests (2h)

**Goal**: Verify CLI functionality against actual Emergent API server with real OAuth flows, database persistence, and API operations. These tests complement the mock-based integration tests (12.2) by validating production-like behavior including actual network calls, database transactions, and auth server interactions.

**Estimated Time**: 2 hours

**Why This Matters**:

- Mock tests verify component interactions but can't catch API contract mismatches
- Real server tests validate end-to-end flows including auth, networking, database persistence
- Catches issues like: invalid request schemas, authentication edge cases, database constraints, API rate limiting
- Provides confidence that CLI works against actual production API, not just mocks
- Tests cross-service integration (CLI → API → Database → Auth Server)

**Prerequisites** (Test Environment Setup):

```bash
# Local dev server must be running
make dev-server  # Starts on localhost:3001

# Database seeded with test organization + project
emergent-cli bootstrap-test-env

# Test user credentials in environment
export TEST_SERVER_URL=http://localhost:3001
export TEST_ORG_ID=<uuid-from-bootstrap>
export TEST_PROJECT_ID=<uuid-from-bootstrap>
export TEST_USER_EMAIL=test-cli@emergent.dev
export TEST_USER_PASSWORD=<generated-password>

# Build CLI binary for testing
go build -o ./bin/emergent-test ./cmd/emergent
```

**Test Categories** (28 scenarios total, ~1,200 LOC):

**Category 1: Authentication Flow** (5 scenarios, `tests/integration/auth_test.go`, ~200 LOC):

1. **Initial Login (OAuth Code Flow)**:

   - **Setup**: Clean keyring (no stored credentials), server running
   - **Execute**: Run `emergent login --email=test-cli@emergent.dev --server-url=http://localhost:3001`
   - **Verify**:
     - CLI opens browser to auth server's login page
     - User completes OAuth flow (in test: automated with headless browser or mock user agent)
     - CLI receives authorization code via callback
     - CLI exchanges code for access token + refresh token
     - Tokens stored in system keyring with correct service name (`emergent.cli.<profile>`)
     - Config file updated with active profile
     - CLI prints "Logged in successfully as test-cli@emergent.dev"
   - **Assertions**:

     ```go
     // Token stored in keyring
     token, err := keyring.Get("emergent.cli.default", "access_token")
     require.NoError(t, err)
     assert.NotEmpty(t, token)

     // Config file updated
     cfg := loadConfig(t)
     assert.Equal(t, "test-cli@emergent.dev", cfg.ActiveProfile.Email)

     // Can immediately call protected API
     resp := execCLI(t, "projects", "list")
     assert.Contains(t, resp, "Test Project")
     ```

2. **Token Refresh on Expiry**:

   - **Setup**: Valid but expired access token in keyring, valid refresh token
   - **Execute**: Run `emergent projects list` (requires auth)
   - **Verify**:
     - CLI detects 401 Unauthorized response
     - CLI uses refresh token to obtain new access token
     - New token stored in keyring (overwriting expired one)
     - Original command retries and succeeds with new token
     - User sees output without being prompted to re-login
   - **Assertions**:

     ```go
     // Expire the access token manually
     expireAccessToken(t, client)

     // Command should still succeed (auto-refresh)
     resp := execCLI(t, "projects", "list")
     assert.Contains(t, resp, "Test Project")

     // Token in keyring should be new
     newToken := getKeychainToken(t)
     assert.NotEqual(t, oldToken, newToken)
     ```

3. **Logout (Credential Deletion)**:

   - **Setup**: Active logged-in session with tokens in keyring
   - **Execute**: Run `emergent logout`
   - **Verify**:
     - CLI calls revoke token endpoint (if API supports)
     - Tokens deleted from keyring (all profiles or just active)
     - Config file cleared of credentials (profile remains but no tokens)
     - Subsequent commands requiring auth prompt for login
   - **Assertions**:

     ```go
     execCLI(t, "logout")

     // Keyring empty
     _, err := keyring.Get("emergent.cli.default", "access_token")
     assert.Error(t, err)

     // Config has no tokens
     cfg := loadConfig(t)
     assert.Empty(t, cfg.ActiveProfile.AccessToken)

     // Protected commands fail
     resp, err := execCLI(t, "projects", "list")
     assert.Error(t, err)
     assert.Contains(t, err.Error(), "Not authenticated")
     ```

4. **Invalid Credentials Handling**:

   - **Setup**: Clean state, server running
   - **Execute**: Run `emergent login --email=invalid@example.com --password=wrongpass`
   - **Verify**:
     - Server returns 401 Unauthorized
     - CLI prints clear error: "Invalid credentials. Check your email/password and try again."
     - CLI suggests: "Visit https://emergent.dev/forgot-password to reset."
     - No token saved to keyring (no partial state)
     - Config file not modified
   - **Assertions**:

     ```go
     resp, err := execCLI(t, "login", "--email=invalid@example.com", "--password=wrong")
     assert.Error(t, err)
     assert.Contains(t, err.Error(), "Invalid credentials")

     // No tokens stored
     _, err = keyring.Get("emergent.cli.default", "access_token")
     assert.Error(t, err)
     ```

5. **Keyring Unavailable Fallback**:
   - **Setup**: Mock keyring to return "not supported" error (simulate headless Linux)
   - **Execute**: Run `emergent login` and complete OAuth flow
   - **Verify**:
     - CLI detects keyring unavailable
     - CLI falls back to storing tokens in config file (with warning)
     - CLI prints: "Warning: System keyring unavailable. Storing credentials in config file (less secure)."
     - Tokens stored in `~/.emergent/config.json` with `0600` permissions
     - Subsequent commands read tokens from config file
   - **Assertions**:

     ```go
     mockKeychainUnavailable(t)

     execCLI(t, "login", "--email=test@example.com")

     // Token in config file
     cfg := loadConfig(t)
     assert.NotEmpty(t, cfg.ActiveProfile.AccessToken)

     // Warning printed
     assert.Contains(t, output, "System keyring unavailable")
     ```

**Category 2: Project Management** (6 scenarios, `tests/integration/projects_test.go`, ~250 LOC):

1. **List Projects (Verify API Response Parsing)**:

   - **Execute**: `emergent projects list`
   - **Verify**: Parses API JSON, displays table with correct columns (ID, Name, Created), sorts by creation date descending
   - **Assertions**: Output contains at least test project, columns aligned, no parsing errors

2. **Create New Project (Verify DB Persistence)**:

   - **Execute**: `emergent projects create --name="CLI Test Project $(date +%s)"`
   - **Verify**:
     - API responds with 201 Created and project object
     - CLI displays "Created project: <name> (ID: <uuid>)"
     - Query database directly to confirm row exists
     - Project appears in subsequent `projects list` call
   - **Cleanup**: Delete project via CLI or API

3. **Get Project Details (Verify Data Completeness)**:

   - **Execute**: `emergent projects get <test-project-id> --format=json`
   - **Verify**:
     - Response includes all fields: id, name, created_at, updated_at, org_id, kb_purpose
     - JSON is valid and parseable
     - Field types match expectations (timestamps as RFC3339, UUIDs as strings)

4. **Update Project Metadata**:

   - **Setup**: Create project with name "Before Update"
   - **Execute**: `emergent projects update <id> --name="After Update" --kb-purpose="Test purpose"`
   - **Verify**:
     - API responds with 200 OK
     - Subsequent GET shows updated values
     - Database row reflects changes
     - CLI prints "Updated project: After Update"

5. **Switch Active Project Context**:

   - **Setup**: Two projects exist (A and B), A is active
   - **Execute**: `emergent projects use <project-B-id>`
   - **Verify**:
     - Config file updated with active_project_id = B's ID
     - Subsequent commands (e.g., `emergent documents list`) query project B's documents
     - CLI prints "Switched to project: <B's name>"

6. **Delete Project (Verify Cascade)**:
   - **Setup**: Create project with documents and graph objects
   - **Execute**: `emergent projects delete <project-id> --force`
   - **Verify**:
     - API responds with 204 No Content
     - Database row soft-deleted (deleted_at timestamp set) OR hard-deleted
     - Related documents no longer accessible
     - Project no longer in `projects list` output

**Category 3: Code Generation** (5 scenarios, `tests/integration/codegen_test.go`, ~200 LOC):

1. **Generate TypeScript Types from GraphQL Schema**:

   - **Setup**: Project with graph types defined (Person, Location, Event)
   - **Execute**: `emergent codegen types --language=typescript --output=./generated/types.ts`
   - **Verify**:
     - CLI fetches schema from `/graph/schema` endpoint
     - Generates valid TypeScript interfaces
     - Output file created at specified path
     - File contains expected types: `export interface Person { ... }`
     - Types include all fields with correct TypeScript types (string, number, Date, etc.)

2. **Generate Python Models from Schema**:

   - **Execute**: `emergent codegen types --language=python --output=./generated/models.py`
   - **Verify**:
     - Generates Pydantic models or dataclasses
     - Correct Python types: `str`, `int`, `datetime`, `Optional[...]`
     - File imports required libraries (`from typing import Optional`)

3. **Custom Template Rendering**:

   - **Setup**: Project with custom template in template pack (e.g., API client generator)
   - **Execute**: `emergent codegen template api-client --template-pack=<pack-id> --output=./client.go`
   - **Verify**:
     - CLI fetches template from API
     - Renders template with project context (project ID, org ID, base URL)
     - Output file created with generated code
     - Code is syntactically valid (can compile/parse)

4. **Output File Creation Verification**:

   - **Setup**: Output directory doesn't exist yet
   - **Execute**: `emergent codegen types --output=./path/to/new/dir/types.ts`
   - **Verify**:
     - CLI creates parent directories (`mkdir -p` behavior)
     - File written successfully
     - CLI prints "Generated types.ts (1234 bytes)"
     - File permissions are `0644` (readable by all, writable by owner)

5. **Schema Validation Errors**:
   - **Setup**: Project with invalid schema (e.g., circular references, missing required fields)
   - **Execute**: `emergent codegen types`
   - **Verify**:
     - CLI detects validation errors from API response (422 Unprocessable Entity)
     - Prints clear error message: "Schema validation failed: <specific error>"
     - Suggests fix: "Check type definitions at <dashboard-url>/graph/schema"
     - Exits with non-zero code (1)
     - No partial output files created

**Category 4: Document Operations** (4 scenarios, `tests/integration/documents_test.go`, ~150 LOC):

1. **Upload Document to Knowledge Base**:

   - **Setup**: Local file `test-doc.md` with content "Integration test document"
   - **Execute**: `emergent documents upload ./test-doc.md --project=<project-id>`
   - **Verify**:
     - CLI uploads file via multipart form POST
     - API responds with 201 Created and document object (id, name, status)
     - CLI prints "Uploaded test-doc.md (ID: <uuid>, Status: pending)"
     - Document appears in `documents list`
     - Query database to confirm document row exists

2. **List Documents with Filters**:

   - **Setup**: Project with 5 documents (3 processed, 2 pending)
   - **Execute**: `emergent documents list --status=processed --format=json`
   - **Verify**:
     - API query includes `?status=processed` filter
     - Response contains only processed documents (count = 3)
     - JSON output is parseable and matches API schema

3. **Delete Document**:

   - **Setup**: Document with ID `<doc-id>` exists
   - **Execute**: `emergent documents delete <doc-id>`
   - **Verify**:
     - API responds with 204 No Content
     - Document no longer in `documents list`
     - Database row soft-deleted or removed

4. **Verify Embeddings Created**:
   - **Setup**: Upload document and wait for processing (or mock job completion)
   - **Execute**: Query via API or CLI (if supported): `emergent documents get <doc-id> --show-chunks`
   - **Verify**:
     - Document status = `processed`
     - Chunks exist in database with embeddings (query `kb.chunks` table)
     - Chunk count > 0
     - Each chunk has `embedding_vec` populated (non-null pgvector)

**Category 5: MCP Server Integration** (3 scenarios, `tests/integration/mcp_test.go`, ~150 LOC):

1. **Start MCP Server in stdio Mode**:

   - **Execute**: `emergent mcp start --mode=stdio` (run as background process)
   - **Verify**:
     - CLI starts MCP server subprocess
     - Server listens on stdin/stdout for MCP protocol messages
     - Server responds to `initialize` request with capabilities
     - CLI prints "MCP server started on stdio"
     - Server PID tracked for cleanup

2. **Execute Tool via MCP Protocol**:

   - **Setup**: MCP server running (from test above)
   - **Execute**: Send MCP `tools/call` request with `search_knowledge_base` tool + query
   - **Verify**:
     - Server processes request and calls Emergent API
     - Server returns valid MCP response with search results
     - Response matches expected schema (tool result with content array)
     - No protocol errors in server logs

3. **Verify Claude Desktop Connection**:
   - **Setup**: MCP server running, Claude Desktop config file at `~/.claude/mcp_config.json`
   - **Execute**: Verify config file structure manually (or have CLI command do it)
   - **Verify**:
     - Config includes `emergent-cli` MCP server entry
     - Server command is correct: `["emergent", "mcp", "start", "--mode=stdio"]`
     - Server args include project ID if configured
     - Config is valid JSON (parseable)

**Category 6: Error Scenarios** (4 scenarios, `tests/integration/errors_test.go`, ~150 LOC):

1. **Network Timeout Handling (Server Unreachable)**:

   - **Setup**: Configure CLI to use server URL that doesn't exist (e.g., `http://localhost:9999`)
   - **Execute**: `emergent projects list`
   - **Verify**:
     - CLI attempts connection with reasonable timeout (5-10 seconds)
     - Timeout occurs, CLI doesn't hang indefinitely
     - CLI prints clear error: "Failed to connect to server at http://localhost:9999. Check server URL and network connection."
     - Suggests: "Run 'emergent config set server-url <url>' to update server URL."
     - Exits with code 1

2. **Invalid API Token (401 Response)**:

   - **Setup**: Store invalid/expired token in keyring
   - **Execute**: `emergent projects list` (should trigger refresh)
   - **Verify**:
     - CLI detects 401 response
     - If refresh token also invalid: prints "Authentication expired. Please login again: emergent login"
     - Clears invalid tokens from keyring
     - Exits with code 1 (not attempting further requests)

3. **Rate Limiting (429 Response with Retry)**:

   - **Setup**: Mock server to return 429 on first request, 200 on retry
   - **Execute**: `emergent projects list`
   - **Verify**:
     - CLI detects 429 Too Many Requests
     - CLI reads `Retry-After` header (e.g., "5" seconds)
     - CLI prints "Rate limited. Retrying in 5 seconds..."
     - CLI waits specified duration (not blindly retrying)
     - CLI retries request and succeeds
     - If retry also rate-limited: backs off exponentially (5s, 10s, 20s) up to max retries (3)

4. **Server Error (500 Response)**:
   - **Setup**: Mock server to return 500 Internal Server Error
   - **Execute**: `emergent documents upload ./test.md`
   - **Verify**:
     - CLI detects 500 response
     - CLI prints: "Server error occurred. Please try again later or contact support."
     - CLI includes request ID if present: "Request ID: <uuid> (include this when reporting issues)"
     - Exits with code 1
     - Does NOT retry automatically (server errors aren't transient like network issues)

**Category 7: Output Formatting** (3 scenarios, included in above tests):

1. **Table Output with Real Data**:

   - **Execute**: `emergent projects list`
   - **Verify**:
     - Columns aligned (ID, Name, Created at)
     - Dates formatted consistently (RFC3339 or human-readable)
     - Table fits terminal width (or truncates gracefully)

2. **JSON Output Parsing**:

   - **Execute**: `emergent projects list --format=json`
   - **Verify**:
     - Output is valid JSON array
     - Each object matches API schema
     - Can pipe to `jq` without errors: `emergent projects list --format=json | jq '.[] | .name'`

3. **YAML Output Validation**:
   - **Execute**: `emergent config show --format=yaml`
   - **Verify**:
     - Output is valid YAML
     - Indentation consistent (2 or 4 spaces)
     - Can parse with `yq` or Python yaml library

**Implementation Pattern for Each Test**:

```go
// +build integration

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers
func requireServerRunning(t *testing.T) {
	t.Helper()
	resp, err := http.Get(os.Getenv("TEST_SERVER_URL") + "/health")
	require.NoError(t, err, "Dev server must be running on TEST_SERVER_URL")
	require.Equal(t, 200, resp.StatusCode)
}

func setupAuthenticatedCLI(t *testing.T) *CLIClient {
	t.Helper()
	email := os.Getenv("TEST_USER_EMAIL")
	password := os.Getenv("TEST_USER_PASSWORD")
	require.NotEmpty(t, email, "TEST_USER_EMAIL must be set")
	require.NotEmpty(t, password, "TEST_USER_PASSWORD must be set")

	// Run login (automates OAuth via headless browser or direct token endpoint)
	cmd := exec.Command("./bin/emergent-test", "login",
		"--email="+email,
		"--password="+password,
		"--server-url="+os.Getenv("TEST_SERVER_URL"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Login failed: %s", string(output))
	require.Contains(t, string(output), "Logged in successfully")

	return &CLIClient{binaryPath: "./bin/emergent-test"}
}

func cleanupTestProject(t *testing.T, cli *CLIClient, projectID string) {
	t.Helper()
	cli.Run("projects", "delete", projectID, "--force")
	// Ignore errors on cleanup (project might not exist)
}

// Example test
func TestRealServer_ProjectCreateAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup
	requireServerRunning(t)
	cli := setupAuthenticatedCLI(t)

	// Create project
	projectName := "Integration Test " + time.Now().Format("20060102-150405")
	output := cli.RunJSON(t, "projects", "create",
		"--name="+projectName,
		"--format=json")

	var project map[string]interface{}
	err := json.Unmarshal([]byte(output), &project)
	require.NoError(t, err)

	projectID := project["id"].(string)
	require.NotEmpty(t, projectID)
	defer cleanupTestProject(t, cli, projectID)

	// Verify appears in list
	listOutput := cli.Run(t, "projects", "list")
	assert.Contains(t, listOutput, projectID)
	assert.Contains(t, listOutput, projectName)

	// Verify database persistence (requires DB access)
	verifyProjectInDatabase(t, projectID, projectName)
}

func verifyProjectInDatabase(t *testing.T, projectID, expectedName string) {
	t.Helper()
	db := connectTestDatabase(t)
	defer db.Close()

	var name string
	err := db.QueryRow("SELECT name FROM kb.projects WHERE id = $1", projectID).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, expectedName, name)
}
```

**Test Infrastructure Requirements** (`tests/integration/helpers.go`, ~100 LOC):

```go
// CLIClient wraps CLI binary for testing
type CLIClient struct {
	binaryPath string
	timeout    time.Duration
}

func (c *CLIClient) Run(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Command failed: %s", string(output))
	return string(output)
}

func (c *CLIClient) RunJSON(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	output := c.Run(t, args...)
	var result map[string]interface{}
	err := json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "Failed to parse JSON output")
	return result
}

// Database helpers
func connectTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_PORT"),
		os.Getenv("POSTGRES_DB"))

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)
	return db
}

// Keychain helpers
func getKeychainToken(t *testing.T, profile string) string {
	t.Helper()
	token, err := keyring.Get("emergent.cli."+profile, "access_token")
	if err != nil {
		return ""
	}
	return token
}

func clearKeychain(t *testing.T) {
	t.Helper()
	_ = keyring.Delete("emergent.cli.default", "access_token")
	_ = keyring.Delete("emergent.cli.default", "refresh_token")
}
```

**Deliverables**:

**Total Deliverables**: 7 files (~1,300 LOC tests + infrastructure)

- `tests/integration/auth_test.go` (~200 LOC, 5 scenarios): OAuth flow, token refresh, logout, invalid credentials, keyring fallback
- `tests/integration/projects_test.go` (~250 LOC, 6 scenarios): List, create, get, update, switch context, delete
- `tests/integration/codegen_test.go` (~200 LOC, 5 scenarios): TypeScript/Python generation, custom templates, file creation, validation
- `tests/integration/documents_test.go` (~150 LOC, 4 scenarios): Upload, list with filters, delete, embeddings verification
- `tests/integration/mcp_test.go` (~150 LOC, 3 scenarios): Start server, execute tool, Claude Desktop config
- `tests/integration/errors_test.go` (~150 LOC, 4 scenarios): Timeout, 401, 429 retry, 500 error
- `tests/integration/helpers.go` (~100 LOC): Test utilities (CLIClient, database, keychain helpers)
- `scripts/bootstrap-test-env.sh` (~80 LOC): Automates test environment setup (creates org, project, seed data)

**Test Execution**:

```bash
# Run real server integration tests (requires dev server running)
go test -tags=integration ./tests/integration/... -v

# Run specific test category
go test -tags=integration ./tests/integration/projects_test.go -v

# Run with coverage
go test -tags=integration ./tests/integration/... -coverprofile=integration-coverage.out

# CI/CD: full suite with server startup
make test-integration-full  # Starts server, runs tests, stops server
```

**Key Differences from Mock Tests (Task 12.2)**:

| Aspect           | Mock Tests (12.2)       | Real Server Tests (12.4)         |
| ---------------- | ----------------------- | -------------------------------- |
| Speed            | <100ms per test         | 1-5s per test                    |
| Dependencies     | None (offline)          | Server + DB + Auth + Network     |
| Data             | Hardcoded fixtures      | Real DB persistence              |
| Auth             | Fake tokens             | Real OAuth flow + token refresh  |
| Purpose          | Fast feedback loop      | Production validation            |
| When to Run      | Every commit (pre-push) | Before release + nightly CI      |
| API Calls        | Mocked HTTP responses   | Real network requests            |
| Database         | N/A                     | Actual SQL queries + constraints |
| Error Simulation | Return mock error codes | Trigger real server errors       |

**Integration Points**:

- **Phase 3 (HTTP Client)**: Real network calls with actual timeouts, retries, connection handling
- **Phase 4 (Auth)**: Full OAuth code flow with real Zitadel auth server
- **Phase 5 (Output)**: Verify formatters work with real API response schemas
- **Phase 6 (Commands)**: All commands execute against real API, verify actual responses
- **Phase 11 (Errors)**: Test how CLI handles real server errors (503, 401, timeouts) not just mock responses

**Success Criteria**:

- [ ] All 28 real server integration tests pass consistently
- [ ] Tests complete in <3 minutes total (acceptable for pre-release checks)
- [ ] Zero flaky tests (no random failures due to timing/state)
- [ ] Tests can run in CI environment (automated server startup/teardown)
- [ ] Test cleanup leaves database in clean state (no orphaned data)
- [ ] Coverage complements mock tests (catches issues mocks can't detect)

**CI/CD Configuration** (`.github/workflows/integration-tests.yml`):

```yaml
name: Real Server Integration Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    types: [opened, synchronize, labeled]
  schedule:
    - cron: '0 2 * * *' # Nightly at 2 AM UTC

jobs:
  integration:
    runs-on: ubuntu-latest
    if: |
      github.event_name == 'push' ||
      github.event_name == 'schedule' ||
      contains(github.event.pull_request.labels.*.name, 'run-integration-tests')

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: spec
          POSTGRES_PASSWORD: spec
          POSTGRES_DB: spec
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      zitadel:
        image: ghcr.io/zitadel/zitadel:latest
        env:
          ZITADEL_MASTERKEY: integration-test-key
        options: >-
          --health-cmd "curl -f http://localhost:8080/ready"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 10

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'

      - name: Start Emergent API Server
        run: |
          docker-compose up -d server
          sleep 10  # Wait for server to be healthy
          curl -f http://localhost:3001/health || exit 1

      - name: Bootstrap Test Environment
        run: |
          chmod +x scripts/bootstrap-test-env.sh
          ./scripts/bootstrap-test-env.sh
        env:
          TEST_SERVER_URL: http://localhost:3001
          POSTGRES_URL: postgres://spec:spec@localhost:5432/spec

      - name: Build CLI Binary
        run: go build -o ./bin/emergent-test ./cmd/emergent

      - name: Run Integration Tests
        run: |
          go test -tags=integration ./tests/integration/... -v -timeout=10m
        env:
          TEST_SERVER_URL: http://localhost:3001
          TEST_ORG_ID: ${{ env.BOOTSTRAP_ORG_ID }}
          TEST_PROJECT_ID: ${{ env.BOOTSTRAP_PROJECT_ID }}
          TEST_USER_EMAIL: test-cli@emergent.dev
          TEST_USER_PASSWORD: ${{ secrets.TEST_USER_PASSWORD }}
          POSTGRES_HOST: localhost
          POSTGRES_PORT: 5432
          POSTGRES_USER: spec
          POSTGRES_PASSWORD: spec
          POSTGRES_DB: spec

      - name: Upload Coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./integration-coverage.out
          flags: integration

      - name: Upload Logs on Failure
        if: failure()
        uses: actions/upload-artifact@v3
        with:
          name: integration-test-logs
          path: |
            ~/.emergent/logs/
            ./tests/integration/*.log
```

**Documentation**: Create `docs/INTEGRATION_TESTING.md` (~150 LOC):

- Prerequisites (dev server setup, test environment variables)
- Running tests locally (full suite, single category, specific test)
- Test environment bootstrap script usage
- Debugging failed integration tests (server logs, database queries)
- Writing new integration tests (patterns, helpers, best practices)
- CI/CD integration (when tests run, how to trigger manually)

## Phase 12 Summary

**Total Tests Implemented**: ~68 scenarios (23 unit + 10 integration + 4 E2E + 28 real server integration + 3 mock infrastructure)

**Total Code**: ~2,000 LOC

- Unit tests: ~800 LOC (5 test files)
- Integration tests: ~700 LOC (mock server + 5 workflow tests)
- E2E tests: ~500 LOC (4 scenario scripts + CI config)

**Time Breakdown**:

- Task 12.1 (Unit Tests): 2.5 hours
- Task 12.2 (Integration Tests): 2 hours
- Task 12.3 (E2E Tests): 1.5 hours
- **Total**: 6 hours

**Testing Pyramid**:

```
      E2E (4 tests)           ← Slowest, most realistic
     /              \
    /  Integration   \        ← Medium speed, mock API
   /    (10 tests)    \
  /                    \
 /   Unit (23 tests)    \     ← Fastest, isolated components
/_______________________\
```

**Coverage Strategy**:

| Module                 | Unit Tests | Integration Tests | E2E Tests | Target Coverage |
| ---------------------- | ---------- | ----------------- | --------- | --------------- |
| Config loading         | 5          | 2                 | -         | ≥85%            |
| Credential storage     | 4          | 2                 | 1         | ≥80%            |
| Token cache            | 4          | 2                 | 1         | ≥80%            |
| Output formatters      | 5          | -                 | -         | ≥90%            |
| Command parsing        | 5          | -                 | -         | ≥85%            |
| HTTP client            | -          | 4                 | 4         | ≥75% (via int)  |
| Full workflows         | -          | -                 | 4         | ≥60% (via E2E)  |
| **Overall CLI Target** | -          | -                 | -         | **≥80%**        |

**Quality Gates**:

- [ ] All unit tests pass with `go test ./...`
- [ ] Coverage ≥80% per module: `go test ./... -cover`
- [ ] All integration tests pass: `make test-integration`
- [ ] All E2E tests pass against dev server: `./tests/e2e/run.sh`
- [ ] CI pipeline green (unit + integration + E2E)
- [ ] No panics or race conditions: `go test -race ./...`

**Architecture Integration**:

```
Phase 12 (Testing)
├─→ Phase 3: Mock HTTP client responses (network errors, timeouts)
├─→ Phase 4: Test auth flows (login, token refresh, keyring fallback)
├─→ Phase 5: Test output formatting (table, JSON, YAML, edge cases)
├─→ Phase 6: Test command routing (flags, subcommands, validation)
├─→ Phase 10: Test interactive prompts (Ctrl+C handling, validation)
└─→ Phase 11: Test error types (wrapping, formatting, retry logic)
```

**Test Execution Commands**:

```bash
# Unit tests only (fast: <5 seconds)
go test ./internal/... -cover

# Integration tests (medium: ~30 seconds)
make test-integration

# E2E tests (slow: ~2 minutes)
E2E_TEST_PASSWORD=secret ./tests/e2e/run.sh

# All tests
make test-all

# Coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

**CI/CD Verification**:

```yaml
# Required CI checks before merge
- Unit tests pass (≥80% coverage)
- Integration tests pass (all scenarios)
- E2E tests pass (against dev server)
- No race conditions detected
- All error paths exercised
```

**Next Phase**: After testing infrastructure is complete, proceed to Phase 13 (Documentation & Distribution) with confidence that all features are verified and covered by automated tests.

## Phase 13: Documentation & Distribution

**Goal**: Create comprehensive user-facing documentation, automate cross-platform builds with GitHub Actions, and verify installation works on clean systems. Enable users to discover, install, configure, and integrate the CLI within 5 minutes.

**Total Phase Duration**: ~6 hours

**Why This Matters**:

- Documentation is the first user touchpoint - poor docs lead to support burden and low adoption
- Automated builds eliminate manual release toil and ensure consistent binary quality across 6+ platforms
- Distribution via package managers (Homebrew, Scoop) reduces installation friction from "download + chmod" to one command
- Verification on clean systems catches installation issues before users encounter them
- CI/CD integration examples accelerate time-to-value for developer users

**Integration Points**:

- Phase 1-6 (Commands): All CLI functionality documented with examples
- Phase 4 (Auth): OAuth flow explained in setup guide
- Phase 9 (MCP Server): Claude Desktop integration instructions
- Phase 10 (Docs Server): Documentation server usage documented
- Phase 11 (Errors): Error handling patterns explained in troubleshooting section

### Task 13.1: Documentation (2.5h)

**Goal**: Create complete, beginner-friendly documentation covering installation (4 methods), configuration (all options), command usage (15+ commands), CI/CD integration (3 platforms), and MCP setup. Documentation must enable a new user to go from "heard about tool" to "first successful command" in under 5 minutes.

**Estimated Time**: 2.5 hours

**Documentation Structure** (8 primary documents, ~2000 LOC total):

**Document 1: README.md** (Primary entry point, ~200 LOC):

- **Hero Section** (15 LOC):
  - One-sentence value proposition: "Manage Emergent projects, generate code, and integrate with Claude Desktop—all from your terminal."
  - Badge row: Build status, Go version, License, Downloads
  - Animated GIF showing `emergent login` → `emergent projects list` → `emergent codegen`
- **Quick Start** (40 LOC):
  - Installation (4 methods, each with one-liner)
  - First-time setup: `emergent login` flow with screenshot
  - First command: `emergent projects list` with expected output
- **Features Grid** (25 LOC):
  - 🔐 OAuth authentication (zero-copy token flows)
  - 📦 Project management (CRUD + context switching)
  - 🎨 Code generation (TypeScript/Python from GraphQL)
  - 📚 MCP server (Claude Desktop integration)
  - 🚀 Fast (<50ms cold start, <15MB binary)
- **Installation Methods** (60 LOC):
  - **macOS**: `brew tap emergent/tap && brew install emergent` (30 seconds)
  - **Windows**: `scoop bucket add emergent https://github.com/emergent/scoop-bucket && scoop install emergent` (45 seconds)
  - **Linux**: `curl -fsSL https://install.emergent.dev | sh` (1 minute)
  - **Go Install**: `go install github.com/emergentmethods/emergent-cli@latest` (requires Go 1.23+)
  - **Direct Download**: Links to GitHub Releases with SHA256 checksums
- **Documentation Links** (20 LOC):
  - [Installation Guide](docs/installation.md) (all platforms, troubleshooting)
  - [Configuration Guide](docs/configuration.md) (all options, examples)
  - [Command Reference](docs/commands/) (auto-generated from Cobra)
  - [CI/CD Integration](docs/ci-cd-integration.md) (GitHub Actions, GitLab CI, CircleCI)
  - [Claude Desktop Setup](docs/claude-desktop-setup.md) (MCP configuration)
- **Contributing & License** (15 LOC)
- **Support Links** (10 LOC): Issues, Discussions, Security Policy

**Document 2: docs/installation.md** (Detailed installation guide, ~300 LOC):

- **Prerequisites** (20 LOC):
  - Operating System: macOS 11+, Linux (glibc 2.28+), Windows 10+
  - Architecture: x86_64, arm64 (Apple Silicon), armv7 (Raspberry Pi)
  - Internet connection for OAuth (initial setup)
  - Optional: Go 1.23+ (for `go install` method)
- **Installation Methods - Detailed** (200 LOC):

  - **macOS via Homebrew** (50 LOC):

    ```bash
    # Add tap
    brew tap emergent/tap

    # Install CLI
    brew install emergent

    # Verify installation
    emergent --version  # Should show: emergent version X.Y.Z

    # Update later
    brew upgrade emergent
    ```

    **Troubleshooting**:

    - "Tap not found" → Verify GitHub repo exists: `https://github.com/emergent/homebrew-tap`
    - "Formula not found" → Run `brew update` first
    - Permission errors → Check Homebrew installation: `brew doctor`

  - **Windows via Scoop** (50 LOC):

    ```powershell
    # Add bucket
    scoop bucket add emergent https://github.com/emergent/scoop-bucket

    # Install CLI
    scoop install emergent

    # Verify
    emergent --version

    # Update later
    scoop update emergent
    ```

    **Troubleshooting**:

    - "Bucket not found" → Check network connection
    - "Hash mismatch" → Run `scoop cache rm emergent && scoop install emergent`

  - **Linux via Install Script** (70 LOC):

    ```bash
    # Download and run
    curl -fsSL https://install.emergent.dev | sh

    # Script does:
    # 1. Detects OS and architecture
    # 2. Downloads appropriate binary
    # 3. Verifies SHA256 checksum
    # 4. Installs to /usr/local/bin
    # 5. Makes executable

    # Manual verification
    emergent --version
    sha256sum /usr/local/bin/emergent  # Compare with GitHub Release
    ```

    **Install to custom location**:

    ```bash
    curl -fsSL https://install.emergent.dev | INSTALL_DIR=$HOME/.local/bin sh
    ```

    **Troubleshooting**:

    - Permission denied → Run with sudo: `curl -fsSL ... | sudo sh`
    - Binary not found → Add to PATH: `export PATH="$HOME/.local/bin:$PATH"`

  - **Direct Download** (30 LOC):
    - Links to GitHub Releases page
    - Platform-specific download instructions
    - Checksum verification commands

- **Post-Installation** (50 LOC):
  - First-time setup: `emergent login` (OAuth flow walkthrough)
  - Shell completion: `emergent completion bash|zsh|fish|powershell`
  - Configuration: `emergent config init` (creates default config)
  - Verify installation: `emergent doctor` (checks dependencies)
- **Uninstallation** (30 LOC):
  - Homebrew: `brew uninstall emergent && brew untap emergent/tap`
  - Scoop: `scoop uninstall emergent`
  - Script: `sudo rm /usr/local/bin/emergent`
  - Clean config: `rm -rf ~/.emergent`

**Document 3: docs/configuration.md** (Configuration reference, ~250 LOC):

- **Configuration File Location** (20 LOC):
  - Default: `~/.emergent/config.json`
  - Override with `EMERGENT_CONFIG` environment variable
  - Example: `EMERGENT_CONFIG=/etc/emergent/config.json emergent projects list`
- **Configuration Schema** (80 LOC):
  ```json
  {
    "api": {
      "base_url": "https://api.emergent.dev",
      "timeout": "30s",
      "retry_max": 3,
      "retry_backoff": "exponential"
    },
    "auth": {
      "method": "oauth", // oauth, token
      "token_cache_ttl": "1h",
      "auto_refresh": true
    },
    "output": {
      "format": "table", // table, json, yaml, csv
      "theme": "auto", // auto, light, dark
      "color": true,
      "truncate": 80,
      "pager": "auto" // auto, always, never
    },
    "mcp": {
      "server_port": 3000,
      "transport": "stdio", // stdio, sse
      "log_level": "info"
    },
    "telemetry": {
      "enabled": false,
      "anonymous_id": "uuid-here"
    }
  }
  ```
- **Option Reference** (120 LOC):
  - **api.base_url**: API endpoint (default: https://api.emergent.dev)
  - **api.timeout**: Request timeout (default: 30s, range: 5s-5m)
  - **api.retry_max**: Max retry attempts (default: 3, range: 0-10)
  - **auth.method**: Authentication method (oauth required for login, token for CI/CD)
  - **output.format**: Output format (table best for humans, json for scripts)
  - **output.theme**: Color theme (auto detects terminal background)
  - **mcp.transport**: MCP protocol (stdio for Claude Desktop, sse for web)
- **Environment Variables** (30 LOC):
  - `EMERGENT_API_TOKEN`: Override OAuth (for CI/CD)
  - `EMERGENT_CONFIG`: Config file path
  - `EMERGENT_NO_COLOR`: Disable colors (for CI logs)
  - `EMERGENT_LOG_LEVEL`: Debug logging (debug, info, warn, error)

**Document 4: docs/commands/README.md** (Command reference index, ~150 LOC):

- **Command Hierarchy** (50 LOC):
  ```
  emergent
  ├── login              # Authenticate via OAuth
  ├── logout             # Clear credentials
  ├── whoami             # Show current user
  ├── organizations      # Org management
  │   ├── list
  │   ├── get <id>
  │   └── switch <id>
  ├── projects           # Project management
  │   ├── list
  │   ├── get <id>
  │   ├── create
  │   ├── update <id>
  │   ├── delete <id>
  │   └── switch <id>
  ├── templates          # Template operations
  │   ├── list
  │   └── get <id>
  ├── codegen            # Code generation
  │   ├── typescript
  │   └── python
  ├── mcp                # MCP server
  │   ├── start
  │   └── version
  ├── docs               # Documentation server
  │   └── serve
  ├── config             # Configuration
  │   ├── init
  │   ├── get <key>
  │   └── set <key> <value>
  ├── completion         # Shell completion
  │   └── bash|zsh|fish|powershell
  └── version            # Show version
  ```
- **Quick Examples** (100 LOC):
  - **Authentication**: `emergent login` (opens browser, completes OAuth)
  - **List Projects**: `emergent projects list --org=my-org`
  - **Create Project**: `emergent projects create --name="New Project" --org=my-org`
  - **Generate TypeScript**: `emergent codegen typescript --schema=schema.graphql --output=./generated`
  - **Start MCP Server**: `emergent mcp start` (for Claude Desktop)
  - **Local Documentation**: `emergent docs serve` (opens http://localhost:8080)

**Document 5: docs/ci-cd-integration.md** (CI/CD guide, ~400 LOC):

- **General Principles** (40 LOC):
  - Use API token authentication (not OAuth) in CI
  - Store token in secrets manager (GitHub Secrets, GitLab Variables, etc.)
  - Pin CLI version for reproducibility
  - Use `--format=json` for machine-readable output
  - Set `EMERGENT_NO_COLOR=1` for clean logs
- **GitHub Actions Example** (150 LOC):

  ```yaml
  name: Generate Code from Schema

  on:
    push:
      branches: [main]
      paths:
        - 'schema/**/*.graphql'

  jobs:
    codegen:
      runs-on: ubuntu-latest

      steps:
        - name: Checkout code
          uses: actions/checkout@v4

        - name: Install Emergent CLI
          run: |
            curl -fsSL https://install.emergent.dev | sh
            echo "$HOME/.local/bin" >> $GITHUB_PATH

        - name: Verify installation
          run: emergent --version

        - name: Generate TypeScript types
          env:
            EMERGENT_API_TOKEN: ${{ secrets.EMERGENT_API_TOKEN }}
          run: |
            emergent codegen typescript \
              --schema=schema/api.graphql \
              --output=src/generated \
              --org=${{ vars.EMERGENT_ORG_ID }} \
              --project=${{ vars.EMERGENT_PROJECT_ID }}

        - name: Commit generated code
          run: |
            git config user.name "GitHub Actions"
            git config user.email "actions@github.com"
            git add src/generated
            git diff --staged --quiet || git commit -m "chore: regenerate types"
            git push
  ```

  **Tips**:

  - Use `actions/cache` to cache CLI binary between runs
  - Set `EMERGENT_LOG_LEVEL=debug` for troubleshooting
  - Use matrix strategy for multi-platform generation

- **GitLab CI Example** (120 LOC):

  ```yaml
  variables:
    EMERGENT_NO_COLOR: '1'
    EMERGENT_LOG_LEVEL: 'info'

  codegen:
    stage: build
    image: golang:1.23-alpine
    before_script:
      - apk add --no-cache curl
      - curl -fsSL https://install.emergent.dev | sh
      - export PATH="$HOME/.local/bin:$PATH"
    script:
      - emergent codegen typescript \
        --schema=schema/api.graphql \
        --output=src/generated \
        --org=$EMERGENT_ORG_ID \
        --project=$EMERGENT_PROJECT_ID
    artifacts:
      paths:
        - src/generated/
    only:
      changes:
        - schema/**/*.graphql
  ```

- **CircleCI Example** (90 LOC):

  ```yaml
  version: 2.1

  jobs:
    codegen:
      docker:
        - image: cimg/go:1.23
      steps:
        - checkout
        - run:
            name: Install CLI
            command: |
              curl -fsSL https://install.emergent.dev | sh
              echo 'export PATH=$HOME/.local/bin:$PATH' >> $BASH_ENV
        - run:
            name: Generate code
            command: |
              emergent codegen typescript \
                --schema=schema/api.graphql \
                --output=src/generated
        - persist_to_workspace:
            root: .
            paths:
              - src/generated

  workflows:
    main:
      jobs:
        - codegen:
            filters:
              branches:
                only: main
  ```

**Document 6: docs/claude-desktop-setup.md** (MCP integration guide, ~250 LOC):

- **Prerequisites** (20 LOC):
  - Claude Desktop app installed (download from claude.ai)
  - Emergent CLI installed and authenticated
  - Access to Claude Desktop config file
- **Configuration Steps** (80 LOC):
  1. **Start MCP Server** (test it works):
     ```bash
     emergent mcp start  # Should show: MCP server listening on stdio
     ```
  2. **Locate Claude Desktop Config**:
     - macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
     - Windows: `%APPDATA%\Claude\claude_desktop_config.json`
     - Linux: `~/.config/Claude/claude_desktop_config.json`
  3. **Add Emergent MCP Server**:
     ```json
     {
       "mcpServers": {
         "emergent": {
           "command": "emergent",
           "args": ["mcp", "start"],
           "env": {
             "EMERGENT_API_TOKEN": "your-token-here"
           }
         }
       }
     }
     ```
  4. **Restart Claude Desktop** (quit completely, then reopen)
  5. **Verify Connection**:
     - Open chat, type: "List my Emergent projects"
     - Claude should use MCP to call `emergent projects list`
- **Available MCP Tools** (60 LOC):
  - `list_projects`: List all projects in organization
  - `get_project`: Get project details by ID
  - `create_project`: Create new project
  - `list_templates`: List available templates
  - `generate_code`: Generate TypeScript/Python code
- **Troubleshooting** (50 LOC):
  - "MCP server not found" → Verify `emergent` is in PATH: `which emergent`
  - "Authentication failed" → Check `EMERGENT_API_TOKEN` is valid
  - "Connection timeout" → Check firewall, try `emergent mcp start` manually
  - Enable debug logging: Add `"EMERGENT_LOG_LEVEL": "debug"` to env
- **Security Considerations** (40 LOC):
  - Token stored in plaintext config → Use keyring for production
  - Rotate tokens regularly
  - Limit token scope to specific org/project if possible

**Document 7: docs/troubleshooting.md** (Common issues, ~200 LOC):

- **Installation Issues** (60 LOC):
  - "Command not found" → Add to PATH or use full path
  - "Permission denied" → Check file permissions: `ls -l $(which emergent)`
  - Checksum mismatch → Re-download from official source
- **Authentication Issues** (50 LOC):
  - OAuth callback fails → Check localhost:8080 not blocked
  - "Invalid credentials" → Run `emergent logout && emergent login`
  - Token expired → Should auto-refresh, check `auth.auto_refresh` config
- **Network Issues** (40 LOC):
  - Connection timeout → Check `api.base_url` and network
  - SSL/TLS errors → Update CA certificates or set `EMERGENT_INSECURE_SKIP_VERIFY=1` (dev only)
- **MCP Server Issues** (50 LOC):
  - Claude Desktop can't connect → Verify stdio transport working: `echo '{}' | emergent mcp start`
  - "Method not found" → Update CLI to latest version

**Document 8: Auto-Generated Command Docs** (15 files, ~50 LOC each, ~750 LOC total):

Generated by Cobra using `emergent docs generate --output=docs/commands/`:

- `docs/commands/emergent.md` (root command)
- `docs/commands/emergent_login.md`
- `docs/commands/emergent_projects.md`
- `docs/commands/emergent_projects_list.md`
- `docs/commands/emergent_projects_create.md`
- ... (one file per command/subcommand)

Each file includes:

- Command syntax
- Description
- Flags (global and local)
- Examples (3-5 per command)
- See Also links (related commands)

**Implementation Scripts**:

**Script 1: docs/generate.sh** (Auto-generate command docs):

```bash
#!/bin/bash
set -euo pipefail

# Generate command documentation using Cobra
echo "Generating command documentation..."
go run main.go docs generate --output=docs/commands/

# Generate completion scripts
echo "Generating shell completions..."
mkdir -p completions
go run main.go completion bash > completions/emergent.bash
go run main.go completion zsh > completions/_emergent
go run main.go completion fish > completions/emergent.fish
go run main.go completion powershell > completions/emergent.ps1

echo "Documentation generated successfully!"
echo "  Command docs: docs/commands/"
echo "  Completions: completions/"
```

**Script 2: scripts/validate-docs.sh** (Validate documentation quality):

````bash
#!/bin/bash
set -euo pipefail

echo "Validating documentation..."

# Check all links work
markdown-link-check docs/**/*.md || echo "Warning: Some links broken"

# Check code blocks are valid
for file in docs/**/*.md; do
  echo "Checking $file..."
  grep -E '```(bash|go|json|yaml)' "$file" || continue
  # Extract and validate code blocks
done

# Check installation script works
echo "Testing installation script..."
bash scripts/install.sh --dry-run

echo "Documentation validation complete!"
````

**Deliverables Checklist**:

- [ ] `README.md` (200 LOC, primary entry point)
- [ ] `docs/installation.md` (300 LOC, detailed install guide)
- [ ] `docs/configuration.md` (250 LOC, config reference)
- [ ] `docs/commands/README.md` (150 LOC, command index)
- [ ] `docs/ci-cd-integration.md` (400 LOC, CI/CD examples)
- [ ] `docs/claude-desktop-setup.md` (250 LOC, MCP guide)
- [ ] `docs/troubleshooting.md` (200 LOC, common issues)
- [ ] `docs/commands/*.md` (750 LOC, auto-generated)
- [ ] `scripts/install.sh` (Unix installation script)
- [ ] `scripts/install.ps1` (Windows installation script)
- [ ] `docs/generate.sh` (doc generation automation)
- [ ] `scripts/validate-docs.sh` (documentation validation)

**Quality Checklist**:

- [ ] All code examples tested and working
- [ ] Screenshots/GIFs for OAuth flow and main commands
- [ ] Links validated (no 404s)
- [ ] Searchable (good headings, keywords)
- [ ] Accessible (clear language, no jargon without explanation)
- [ ] Platform-specific instructions accurate (macOS, Linux, Windows)
- [ ] Examples use realistic data (not "foo", "bar")
- [ ] Error messages documented with solutions

### Task 13.2: Build Automation (2h)

**Goal**: Automate cross-platform builds (6 targets), binary signing, checksum generation, and artifact upload to GitHub Releases using GitHub Actions and GoReleaser. Enable one-command releases: `git tag v1.0.0 && git push --tags`.

**Estimated Time**: 2 hours

**Build Matrix** (6 platform/architecture combinations):

| Platform | Architecture | Target Triple | Binary Name                |
| -------- | ------------ | ------------- | -------------------------- |
| macOS    | x86_64       | darwin-amd64  | emergent-darwin-amd64      |
| macOS    | arm64        | darwin-arm64  | emergent-darwin-arm64      |
| Linux    | x86_64       | linux-amd64   | emergent-linux-amd64       |
| Linux    | arm64        | linux-arm64   | emergent-linux-arm64       |
| Windows  | x86_64       | windows-amd64 | emergent-windows-amd64.exe |
| Windows  | arm64        | windows-arm64 | emergent-windows-arm64.exe |

**GitHub Actions Workflow** (`.github/workflows/release.yml`, ~150 LOC):

```yaml
name: Release

on:
  push:
    tags:
      - 'v*' # Trigger on version tags: v1.0.0, v1.2.3, etc.

permissions:
  contents: write # Required for creating releases
  packages: write # Required for Docker images (future)

jobs:
  release:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0 # Full history for changelog generation

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - name: Run tests
        run: go test -v -race -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage.out

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v5
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          # Optional: Code signing certificates
          # MACOS_CERTIFICATE: ${{ secrets.MACOS_CERTIFICATE }}
          # MACOS_CERTIFICATE_PWD: ${{ secrets.MACOS_CERTIFICATE_PWD }}
          # WINDOWS_CERTIFICATE: ${{ secrets.WINDOWS_CERTIFICATE }}

      - name: Update Homebrew formula
        if: success()
        run: |
          # Trigger homebrew-tap repository update
          curl -X POST \
            -H "Authorization: token ${{ secrets.HOMEBREW_TAP_TOKEN }}" \
            -H "Accept: application/vnd.github.v3+json" \
            https://api.github.com/repos/emergent/homebrew-tap/dispatches \
            -d '{"event_type":"update-formula","client_payload":{"version":"${{ github.ref_name }}"}}'

      - name: Update Scoop manifest
        if: success()
        run: |
          # Trigger scoop-bucket repository update
          curl -X POST \
            -H "Authorization: token ${{ secrets.SCOOP_BUCKET_TOKEN }}" \
            -H "Accept: application/vnd.github.v3+json" \
            https://api.github.com/repos/emergent/scoop-bucket/dispatches \
            -d '{"event_type":"update-manifest","client_payload":{"version":"${{ github.ref_name }}"}}'
```

**GoReleaser Configuration** (`.goreleaser.yml`, ~120 LOC):

````yaml
# GoReleaser configuration for emergent-cli
# Documentation: https://goreleaser.com

version: 2

before:
  hooks:
    # Ensure dependencies are up-to-date
    - go mod tidy
    # Run tests before release
    - go test -v ./...

builds:
  - id: emergent
    main: ./cmd/emergent
    binary: emergent

    # Build flags
    flags:
      - -trimpath # Remove file system paths from binary

    ldflags:
      # Embed version info at build time
      - -s -w # Strip debug info (reduces binary size)
      - -X github.com/emergentmethods/emergent-cli/internal/version.Version={{.Version}}
      - -X github.com/emergentmethods/emergent-cli/internal/version.Commit={{.ShortCommit}}
      - -X github.com/emergentmethods/emergent-cli/internal/version.Date={{.Date}}
      - -X github.com/emergentmethods/emergent-cli/internal/version.BuiltBy=goreleaser

    # Target platforms
    goos:
      - darwin
      - linux
      - windows

    goarch:
      - amd64
      - arm64

    # Build tags
    tags:
      - netgo # Use pure Go network stack (better compatibility)

    # Environment variables for build
    env:
      - CGO_ENABLED=0 # Static binary (no C dependencies)

archives:
  - id: default
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

    name_template: >-
      {{ .ProjectName }}-
      {{ .Version }}-
      {{ .Os }}-
      {{ .Arch }}
      {{- if .Arm }}v{{ .Arm }}{{ end }}

    files:
      - LICENSE
      - README.md
      - docs/**/*
      - completions/*

checksum:
  name_template: 'checksums.txt'
  algorithm: sha256

changelog:
  sort: asc
  use: github
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'
      - 'typo'
  groups:
    - title: Features
      regexp: '^feat:'
      order: 0
    - title: Bug Fixes
      regexp: '^fix:'
      order: 1
    - title: Others
      order: 999

release:
  github:
    owner: emergentmethods
    name: emergent-cli

  draft: false
  prerelease: auto # Auto-detect from version (v1.0.0-beta.1 = prerelease)

  name_template: '{{.ProjectName}} v{{.Version}}'

  header: |
    ## Emergent CLI {{ .Version }}

    **Release Date**: {{ .Date }}
    **Git Commit**: {{ .ShortCommit }}

    ### Installation

    **macOS (Homebrew)**:
    ```bash
    brew tap emergent/tap
    brew install emergent
    ```

    **Windows (Scoop)**:
    ```powershell
    scoop bucket add emergent https://github.com/emergent/scoop-bucket
    scoop install emergent
    ```

    **Linux**:
    ```bash
    curl -fsSL https://install.emergent.dev | sh
    ```

    **Direct Download**: See assets below for your platform.

  footer: |
    ### Checksums

    Verify your download:
    ```bash
    sha256sum -c checksums.txt
    ```

    ### Documentation

    - [Installation Guide](https://github.com/emergentmethods/emergent-cli/blob/main/docs/installation.md)
    - [Configuration Guide](https://github.com/emergentmethods/emergent-cli/blob/main/docs/configuration.md)
    - [Command Reference](https://github.com/emergentmethods/emergent-cli/blob/main/docs/commands/)
````

**Homebrew Formula Template** (`homebrew/emergent.rb`, ~80 LOC):

```ruby
class Emergent < Formula
  desc "CLI tool for managing Emergent projects and generating code"
  homepage "https://github.com/emergentmethods/emergent-cli"
  version "{{.Version}}"

  on_macos do
    if Hardware::CPU.intel?
      url "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-darwin-amd64.tar.gz"
      sha256 "{{.Sha256Darwin_amd64}}"
    elsif Hardware::CPU.arm?
      url "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-darwin-arm64.tar.gz"
      sha256 "{{.Sha256Darwin_arm64}}"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-linux-amd64.tar.gz"
      sha256 "{{.Sha256Linux_amd64}}"
    elsif Hardware::CPU.arm?
      url "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-linux-arm64.tar.gz"
      sha256 "{{.Sha256Linux_arm64}}"
    end
  end

  def install
    bin.install "emergent"

    # Install shell completions
    generate_completions_from_executable(bin/"emergent", "completion")

    # Install man pages (if generated)
    # man1.install "docs/man/emergent.1"
  end

  test do
    assert_match "emergent version", shell_output("#{bin}/emergent --version")

    # Test help output
    assert_match "Manage Emergent projects", shell_output("#{bin}/emergent --help")
  end
end
```

**Scoop Manifest Template** (`scoop/emergent.json`, ~60 LOC):

```json
{
  "version": "{{.Version}}",
  "description": "CLI tool for managing Emergent projects and generating code",
  "homepage": "https://github.com/emergentmethods/emergent-cli",
  "license": "MIT",
  "architecture": {
    "64bit": {
      "url": "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-windows-amd64.zip",
      "hash": "{{.Sha256Windows_amd64}}",
      "bin": "emergent.exe"
    },
    "arm64": {
      "url": "https://github.com/emergentmethods/emergent-cli/releases/download/v{{.Version}}/emergent-{{.Version}}-windows-arm64.zip",
      "hash": "{{.Sha256Windows_arm64}}",
      "bin": "emergent.exe"
    }
  },
  "checkver": {
    "github": "https://github.com/emergentmethods/emergent-cli"
  },
  "autoupdate": {
    "architecture": {
      "64bit": {
        "url": "https://github.com/emergentmethods/emergent-cli/releases/download/v$version/emergent-$version-windows-amd64.zip"
      },
      "arm64": {
        "url": "https://github.com/emergentmethods/emergent-cli/releases/download/v$version/emergent-$version-windows-arm64.zip"
      }
    },
    "hash": {
      "url": "$baseurl/checksums.txt"
    }
  }
}
```

**Manual Build Script** (`scripts/build-all.sh`, ~100 LOC):

```bash
#!/bin/bash
# Manual cross-compilation script (for local testing)
set -euo pipefail

VERSION=${VERSION:-dev}
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS="-s -w \
  -X github.com/emergentmethods/emergent-cli/internal/version.Version=$VERSION \
  -X github.com/emergentmethods/emergent-cli/internal/version.Commit=$COMMIT \
  -X github.com/emergentmethods/emergent-cli/internal/version.Date=$DATE \
  -X github.com/emergentmethods/emergent-cli/internal/version.BuiltBy=manual"

echo "Building emergent-cli v$VERSION..."
echo "Commit: $COMMIT"
echo "Date: $DATE"

mkdir -p dist

# Build for all platforms
PLATFORMS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

for platform in "${PLATFORMS[@]}"; do
  GOOS=${platform%/*}
  GOARCH=${platform#*/}

  OUTPUT="dist/emergent-$VERSION-$GOOS-$GOARCH"
  [[ $GOOS == "windows" ]] && OUTPUT="$OUTPUT.exe"

  echo "Building for $GOOS/$GOARCH..."
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build \
    -trimpath \
    -ldflags="$LDFLAGS" \
    -o "$OUTPUT" \
    ./cmd/emergent

  # Generate SHA256 checksum
  if [[ $GOOS == "darwin" ]] || [[ $GOOS == "linux" ]]; then
    shasum -a 256 "$OUTPUT" >> dist/checksums.txt
  fi
done

echo "Build complete! Artifacts in dist/"
ls -lh dist/
```

**Platform-Specific Build Considerations**:

**macOS Considerations** (~40 LOC):

- **Code Signing** (required for Gatekeeper on macOS 10.15+):
  - Obtain Apple Developer certificate
  - Sign binary: `codesign --sign "Developer ID Application: Your Name" emergent`
  - Verify: `codesign --verify --verbose emergent`
- **Notarization** (required for macOS 10.15+):
  - Submit to Apple: `xcrun notarytool submit emergent.zip --apple-id ... --password ...`
  - Staple ticket: `xcrun stapler staple emergent`
- **Universal Binaries** (optional, single binary for Intel + Apple Silicon):
  - Combine: `lipo -create emergent-amd64 emergent-arm64 -output emergent`
  - Verify: `lipo -info emergent`

**Windows Considerations** (~30 LOC):

- **Authenticode Signing** (optional, removes "Unknown Publisher" warning):
  - Obtain code signing certificate
  - Sign: `signtool sign /f cert.pfx /p password /tr http://timestamp.digicert.com emergent.exe`
- **Manifest** (embed app metadata):
  - Create `emergent.exe.manifest` with version, description
  - Embed during build: Use `goversioninfo` or `rsrc` tool

**Linux Considerations** (~20 LOC):

- **Static Linking** (CGO_ENABLED=0 ensures no glibc dependency):
  - Verify: `ldd emergent` should show "not a dynamic executable"
- **Distribution Compatibility**:
  - Built binary works on any Linux with kernel 2.6.23+ (2007)
  - No glibc version dependency

**Binary Size Optimization** (~30 LOC):

- **Base build**: ~25MB (with symbols)
- **Strip debug info** (`-s -w` ldflags): ~15MB (40% reduction)
- **UPX compression** (optional): ~6MB (75% reduction)
  ```bash
  upx --best --lzma emergent  # Aggressive compression
  ```
  **Trade-offs**:
  - Slower cold start (~100ms vs ~50ms)
  - Some antivirus flagging
  - Use only for size-constrained environments

**Deliverables Checklist**:

- [ ] `.github/workflows/release.yml` (150 LOC, GitHub Actions workflow)
- [ ] `.goreleaser.yml` (120 LOC, GoReleaser configuration)
- [ ] `homebrew/emergent.rb` (80 LOC, Homebrew formula template)
- [ ] `scoop/emergent.json` (60 LOC, Scoop manifest template)
- [ ] `scripts/build-all.sh` (100 LOC, manual build script)
- [ ] `docs/releasing.md` (100 LOC, release process documentation)

**Release Process Documentation** (`docs/releasing.md`, ~100 LOC):

````markdown
# Release Process

## Prerequisites

- [ ] All tests passing on main branch
- [ ] CHANGELOG.md updated with unreleased changes
- [ ] Version number decided (semantic versioning: MAJOR.MINOR.PATCH)

## Steps

1. **Create release branch**:
   ```bash
   git checkout -b release/v1.2.3
   ```
````

2. **Update version references**:

   - [ ] `internal/version/version.go` (default version string)
   - [ ] `README.md` (installation examples)
   - [ ] `CHANGELOG.md` (move Unreleased → v1.2.3)

3. **Commit and push**:

   ```bash
   git add .
   git commit -m "chore: prepare release v1.2.3"
   git push origin release/v1.2.3
   ```

4. **Create and push tag**:

   ```bash
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

5. **GitHub Actions automatically**:

   - Runs tests
   - Builds 6 platform binaries
   - Generates checksums
   - Creates GitHub Release (draft)
   - Updates Homebrew formula
   - Updates Scoop manifest

6. **Manually review and publish**:

   - [ ] Go to GitHub Releases
   - [ ] Review changelog and artifacts
   - [ ] Publish release (converts draft → published)

7. **Post-release**:
   - [ ] Merge release branch to main
   - [ ] Delete release branch
   - [ ] Announce on communication channels

## Hotfix Process

For urgent fixes:

```bash
git checkout -b hotfix/v1.2.4 v1.2.3  # Branch from tag
# Make fix
git commit -m "fix: critical bug"
git tag -a v1.2.4 -m "Hotfix v1.2.4"
git push origin v1.2.4
```

## Rollback

If release has critical issues:

1. Delete tag: `git push --delete origin v1.2.3`
2. Delete GitHub Release
3. Revert Homebrew/Scoop commits
4. Fix issues and re-release as v1.2.4

````

### Task 13.3: Final Verification (1.5h)

**Goal**: Verify CLI installs correctly on clean systems (3 OSes), meets performance requirements (binary size <15MB, cold start <50ms), and all features work end-to-end. Catch platform-specific issues before users encounter them.

**Estimated Time**: 1.5 hours

**Verification Matrix** (3 operating systems × 4 verification types = 12 checks):

| OS       | Installation | Performance | Functionality | Documentation |
| -------- | ------------ | ----------- | ------------- | ------------- |
| macOS    | ✅            | ✅           | ✅             | ✅             |
| Linux    | ✅            | ✅           | ✅             | ✅             |
| Windows  | ✅            | ✅           | ✅             | ✅             |

**Clean System Installation Tests**:

**Docker-Based Test Script** (`scripts/test-install.sh`, ~150 LOC):

```bash
#!/bin/bash
# Test installation on clean Docker containers
set -euo pipefail

TEST_VERSION=${1:-latest}  # Version to test (or "latest")

echo "Testing emergent-cli installation (version: $TEST_VERSION)..."

# Test on Ubuntu (mimics Debian-based Linux)
echo "=== Testing on Ubuntu ==="
docker run --rm -it \
  -v "$(pwd)/scripts/install.sh:/tmp/install.sh" \
  ubuntu:22.04 bash -c "
    set -euxo pipefail
    apt-get update -qq
    apt-get install -y curl ca-certificates

    # Test installation
    bash /tmp/install.sh

    # Verify binary
    emergent --version

    # Test basic command
    emergent --help | grep 'Manage Emergent projects'

    # Check binary size
    SIZE=\$(stat -f%z /usr/local/bin/emergent 2>/dev/null || stat -c%s /usr/local/bin/emergent)
    echo \"Binary size: \$SIZE bytes\"
    if [ \$SIZE -gt 15728640 ]; then  # 15MB
      echo \"ERROR: Binary too large (\$SIZE > 15MB)\"
      exit 1
    fi

    # Test cold start time
    START=\$(date +%s%N)
    emergent --version > /dev/null
    END=\$(date +%s%N)
    DURATION=\$(( (END - START) / 1000000 ))  # Convert to milliseconds
    echo \"Cold start time: \${DURATION}ms\"
    if [ \$DURATION -gt 50 ]; then
      echo \"WARNING: Cold start slower than 50ms target\"
    fi

    echo \"✅ Ubuntu test passed\"
  "

# Test on Alpine (mimics lightweight Linux)
echo "=== Testing on Alpine ==="
docker run --rm -it \
  -v "$(pwd)/scripts/install.sh:/tmp/install.sh" \
  alpine:3.18 sh -c "
    set -euxo pipefail
    apk add --no-cache curl ca-certificates bash

    bash /tmp/install.sh
    emergent --version

    # Verify static binary (no dynamic dependencies)
    if ldd /usr/local/bin/emergent 2>&1 | grep -q 'not a dynamic executable'; then
      echo '✅ Static binary verified'
    else
      echo '❌ ERROR: Binary has dynamic dependencies'
      ldd /usr/local/bin/emergent
      exit 1
    fi

    echo '✅ Alpine test passed'
  "

echo "✅ All Docker tests passed"
````

**macOS Verification Script** (`scripts/verify-macos.sh`, ~100 LOC):

```bash
#!/bin/bash
# Verify macOS installation and functionality
set -euo pipefail

echo "Verifying emergent-cli on macOS..."

# 1. Test Homebrew installation
echo "=== Testing Homebrew Installation ==="
brew tap emergent/tap || echo "Tap already added"
brew install emergent || brew upgrade emergent

# Verify installation
which emergent
emergent --version

# 2. Test functionality
echo "=== Testing Functionality ==="

# Test help
emergent --help | grep -q "Manage Emergent projects" || exit 1

# Test configuration
emergent config init
[ -f ~/.emergent/config.json ] || exit 1

# Test completion generation
emergent completion bash > /tmp/emergent.bash
[ -s /tmp/emergent.bash ] || exit 1  # File not empty

# 3. Test binary properties
echo "=== Testing Binary Properties ==="

# Check code signing (macOS 10.15+)
codesign -vv /opt/homebrew/bin/emergent 2>&1 | grep -q "valid on disk" || {
  echo "WARNING: Binary not code-signed (may trigger Gatekeeper)"
}

# Check architecture (Apple Silicon should be arm64)
ARCH=$(file /opt/homebrew/bin/emergent | grep -o 'arm64\|x86_64')
echo "Architecture: $ARCH"

# 4. Test MCP server
echo "=== Testing MCP Server ==="
timeout 5s emergent mcp start --test-mode || {
  echo "✅ MCP server starts (timeout expected in test mode)"
}

# 5. Performance benchmarks
echo "=== Performance Benchmarks ==="

# Cold start time (5 samples)
TOTAL=0
for i in {1..5}; do
  START=$(gdate +%s%N)  # GNU date (brew install coreutils)
  emergent --version > /dev/null
  END=$(gdate +%s%N)
  DURATION=$(( (END - START) / 1000000 ))
  echo "Sample $i: ${DURATION}ms"
  TOTAL=$((TOTAL + DURATION))
done
AVG=$((TOTAL / 5))
echo "Average cold start: ${AVG}ms (target: <50ms)"

# Binary size
SIZE=$(stat -f%z /opt/homebrew/bin/emergent)
SIZE_MB=$((SIZE / 1024 / 1024))
echo "Binary size: ${SIZE_MB}MB (target: <15MB)"

echo "✅ macOS verification complete"
```

**Linux Verification Script** (`scripts/verify-linux.sh`, ~80 LOC):

```bash
#!/bin/bash
# Verify Linux installation and functionality
set -euo pipefail

echo "Verifying emergent-cli on Linux..."

# 1. Test curl installer
echo "=== Testing Curl Installer ==="
curl -fsSL https://install.emergent.dev | sh
export PATH="$HOME/.local/bin:$PATH"

# Verify
which emergent
emergent --version

# 2. Verify static binary
echo "=== Verifying Static Binary ==="
if ldd ~/.local/bin/emergent 2>&1 | grep -q 'not a dynamic executable'; then
  echo "✅ Static binary (no glibc dependency)"
else
  echo "❌ ERROR: Dynamic dependencies found:"
  ldd ~/.local/bin/emergent
  exit 1
fi

# 3. Test functionality
echo "=== Testing Functionality ==="
emergent --help | grep -q "Manage Emergent projects" || exit 1

# Shell completion
emergent completion bash > /tmp/emergent.bash
[ -s /tmp/emergent.bash ] || exit 1

# 4. Performance
echo "=== Performance Benchmarks ==="

# Cold start (5 samples)
TOTAL=0
for i in {1..5}; do
  START=$(date +%s%N)
  emergent --version > /dev/null
  END=$(date +%s%N)
  DURATION=$(( (END - START) / 1000000 ))
  TOTAL=$((TOTAL + DURATION))
done
AVG=$((TOTAL / 5))
echo "Average cold start: ${AVG}ms (target: <50ms)"

# Binary size
SIZE=$(stat -c%s ~/.local/bin/emergent)
SIZE_MB=$((SIZE / 1024 / 1024))
echo "Binary size: ${SIZE_MB}MB (target: <15MB)"

echo "✅ Linux verification complete"
```

**Windows Verification Script** (`scripts/verify-windows.ps1`, ~100 LOC):

```powershell
# Verify Windows installation and functionality
$ErrorActionPreference = "Stop"

Write-Host "Verifying emergent-cli on Windows..." -ForegroundColor Cyan

# 1. Test Scoop installation
Write-Host "=== Testing Scoop Installation ===" -ForegroundColor Yellow
scoop bucket add emergent https://github.com/emergent/scoop-bucket
scoop install emergent

# Verify installation
$emergentPath = (Get-Command emergent).Source
Write-Host "Installed at: $emergentPath"
emergent --version

# 2. Test functionality
Write-Host "=== Testing Functionality ===" -ForegroundColor Yellow

# Test help
$helpOutput = emergent --help
if ($helpOutput -notmatch "Manage Emergent projects") {
    Write-Error "Help output missing expected text"
}

# Test configuration
emergent config init
if (-not (Test-Path "$env:USERPROFILE\.emergent\config.json")) {
    Write-Error "Config file not created"
}

# Test completion
emergent completion powershell > "$env:TEMP\emergent.ps1"
if ((Get-Item "$env:TEMP\emergent.ps1").Length -eq 0) {
    Write-Error "Completion script empty"
}

# 3. Test binary properties
Write-Host "=== Testing Binary Properties ===" -ForegroundColor Yellow

# Check digital signature (if signed)
$signature = Get-AuthenticodeSignature $emergentPath
if ($signature.Status -eq "Valid") {
    Write-Host "✅ Binary digitally signed" -ForegroundColor Green
} else {
    Write-Host "⚠️  Binary not signed (users will see SmartScreen warning)" -ForegroundColor Yellow
}

# 4. Performance benchmarks
Write-Host "=== Performance Benchmarks ===" -ForegroundColor Yellow

# Cold start time (5 samples)
$durations = @()
for ($i = 1; $i -le 5; $i++) {
    $start = Get-Date
    emergent --version | Out-Null
    $end = Get-Date
    $duration = ($end - $start).TotalMilliseconds
    $durations += $duration
    Write-Host "Sample $i: $([math]::Round($duration, 2))ms"
}
$avg = ($durations | Measure-Object -Average).Average
Write-Host "Average cold start: $([math]::Round($avg, 2))ms (target: <50ms)"

# Binary size
$size = (Get-Item $emergentPath).Length
$sizeMB = [math]::Round($size / 1MB, 2)
Write-Host "Binary size: ${sizeMB}MB (target: <15MB)"

Write-Host "✅ Windows verification complete" -ForegroundColor Green
```

**Performance Benchmarking Script** (`scripts/benchmark-startup.sh`, ~60 LOC):

```bash
#!/bin/bash
# Benchmark CLI cold start performance
set -euo pipefail

SAMPLES=${1:-100}  # Number of samples (default: 100)

echo "Benchmarking emergent cold start ($SAMPLES samples)..."

DURATIONS=()

for i in $(seq 1 $SAMPLES); do
  START=$(date +%s%N)
  emergent --version > /dev/null 2>&1
  END=$(date +%s%N)
  DURATION=$(( (END - START) / 1000000 ))  # Convert to ms
  DURATIONS+=($DURATION)

  # Progress indicator
  if [ $((i % 10)) -eq 0 ]; then
    echo -n "."
  fi
done
echo ""

# Calculate statistics
MIN=${DURATIONS[0]}
MAX=${DURATIONS[0]}
TOTAL=0

for d in "${DURATIONS[@]}"; do
  TOTAL=$((TOTAL + d))
  [ $d -lt $MIN ] && MIN=$d
  [ $d -gt $MAX ] && MAX=$d
done

AVG=$((TOTAL / SAMPLES))

# Calculate median
IFS=$'\n' SORTED=($(sort -n <<<"${DURATIONS[*]}"))
MID=$((SAMPLES / 2))
MEDIAN=${SORTED[$MID]}

# Calculate percentiles
P50=${SORTED[$((SAMPLES * 50 / 100))]}
P90=${SORTED[$((SAMPLES * 90 / 100))]}
P95=${SORTED[$((SAMPLES * 95 / 100))]}
P99=${SORTED[$((SAMPLES * 99 / 100))]}

echo "=== Cold Start Performance (${SAMPLES} samples) ==="
echo "  Min:    ${MIN}ms"
echo "  Max:    ${MAX}ms"
echo "  Avg:    ${AVG}ms"
echo "  Median: ${MEDIAN}ms"
echo "  P50:    ${P50}ms"
echo "  P90:    ${P90}ms"
echo "  P95:    ${P95}ms"
echo "  P99:    ${P99}ms"

# Verdict
if [ $P95 -le 50 ]; then
  echo "✅ PASS: P95 ≤ 50ms target"
else
  echo "❌ FAIL: P95 (${P95}ms) > 50ms target"
  exit 1
fi
```

**Functional Verification Checklist** (`docs/verification-checklist.md`, ~120 LOC):

```markdown
# Final Verification Checklist

## Pre-Release Verification

### Installation Testing

- [ ] **macOS (Intel)**

  - [ ] Homebrew installation works
  - [ ] Binary runs without Gatekeeper warnings
  - [ ] Shell completion installs correctly
  - [ ] Can uninstall cleanly

- [ ] **macOS (Apple Silicon)**

  - [ ] Homebrew installation works (native arm64)
  - [ ] Binary runs without Rosetta 2
  - [ ] Performance meets targets (<50ms)

- [ ] **Linux (Ubuntu 22.04)**

  - [ ] Curl script installs correctly
  - [ ] Binary is statically linked (no glibc dependency)
  - [ ] Works without additional packages

- [ ] **Linux (Alpine)**

  - [ ] Binary works on musl libc
  - [ ] No missing shared libraries

- [ ] **Windows 10**

  - [ ] Scoop installation works
  - [ ] Binary runs without SmartScreen warnings (if signed)
  - [ ] PowerShell completion works

- [ ] **Windows 11**
  - [ ] Same as Windows 10
  - [ ] Works on ARM64 (Surface Pro X)

### Performance Testing

- [ ] **Binary Size**

  - [ ] macOS amd64: <15MB
  - [ ] macOS arm64: <15MB
  - [ ] Linux amd64: <15MB
  - [ ] Linux arm64: <15MB
  - [ ] Windows amd64: <15MB
  - [ ] Windows arm64: <15MB

- [ ] **Cold Start Time** (P95 < 50ms)
  - [ ] macOS: \_\_\_ms
  - [ ] Linux: \_\_\_ms
  - [ ] Windows: \_\_\_ms

### Functionality Testing

- [ ] **Authentication**

  - [ ] `emergent login` opens browser and completes OAuth
  - [ ] Token stored securely (keyring or encrypted file)
  - [ ] `emergent whoami` shows current user
  - [ ] `emergent logout` clears credentials

- [ ] **Project Management**

  - [ ] `emergent projects list` returns projects
  - [ ] `emergent projects create` creates project
  - [ ] `emergent projects get <id>` shows details
  - [ ] `emergent projects update <id>` modifies project
  - [ ] `emergent projects delete <id>` removes project

- [ ] **Code Generation**

  - [ ] `emergent codegen typescript` generates valid TS code
  - [ ] `emergent codegen python` generates valid Python code
  - [ ] Generated code compiles/imports without errors

- [ ] **MCP Server**

  - [ ] `emergent mcp start` starts server
  - [ ] Claude Desktop can connect and call tools
  - [ ] MCP tools return correct data

- [ ] **Documentation Server**

  - [ ] `emergent docs serve` starts HTTP server
  - [ ] Can access docs at http://localhost:8080
  - [ ] All documentation pages render correctly

- [ ] **Configuration**
  - [ ] `emergent config init` creates config file
  - [ ] `emergent config get output.format` returns value
  - [ ] `emergent config set output.format json` persists change
  - [ ] Environment variables override config

### Documentation Testing

- [ ] **README.md**

  - [ ] Installation instructions work
  - [ ] Quick start example works
  - [ ] All links resolve (no 404s)

- [ ] **Installation Guide**

  - [ ] Platform-specific instructions tested
  - [ ] Troubleshooting steps accurate

- [ ] **Configuration Guide**

  - [ ] All config options documented
  - [ ] Examples tested and working

- [ ] **Command Reference**

  - [ ] Auto-generated docs up-to-date
  - [ ] Examples in each command doc work

- [ ] **CI/CD Integration Guide**

  - [ ] GitHub Actions example tested
  - [ ] GitLab CI example tested
  - [ ] CircleCI example tested

- [ ] **Claude Desktop Setup**
  - [ ] MCP configuration steps work
  - [ ] Troubleshooting accurate

### Package Manager Testing

- [ ] **Homebrew**

  - [ ] Formula syntax valid: `brew audit emergent`
  - [ ] Test install: `brew test emergent`
  - [ ] Tap repository accessible

- [ ] **Scoop**
  - [ ] Manifest syntax valid
  - [ ] Checksum verification works
  - [ ] Autoupdate config correct

### Security Testing

- [ ] **Code Signing**

  - [ ] macOS binary signed (if applicable)
  - [ ] Windows binary signed (if applicable)
  - [ ] Signatures verify correctly

- [ ] **Dependency Audit**
  - [ ] `go mod verify` passes
  - [ ] No known vulnerabilities: `govulncheck ./...`
  - [ ] Dependencies up-to-date

### Release Artifacts

- [ ] **GitHub Release**

  - [ ] All 6 platform binaries present
  - [ ] Checksums file present and valid
  - [ ] Release notes accurate
  - [ ] Links in release notes work

- [ ] **Installation Scripts**
  - [ ] `scripts/install.sh` downloads correct binary
  - [ ] `scripts/install.ps1` works on Windows

## Post-Release Verification

- [ ] **Homebrew tap updated** (auto or manual)
- [ ] **Scoop bucket updated** (auto or manual)
- [ ] **Documentation site deployed**
- [ ] **Announcement published**
- [ ] **GitHub release published** (not draft)
- [ ] **Download links tested**
- [ ] **Proposal status updated** to complete

## Rollback Plan

If critical issues discovered:

1. **Immediate**: Delete GitHub Release and tag
2. **Urgent**: Revert Homebrew/Scoop commits
3. **Communication**: Post issue on GitHub with workaround
4. **Fix**: Create hotfix branch, test thoroughly
5. **Re-release**: New version with fixes (bump patch number)
```

**Deliverables Checklist**:

- [ ] `scripts/test-install.sh` (150 LOC, Docker-based installation tests)
- [ ] `scripts/verify-macos.sh` (100 LOC, macOS verification script)
- [ ] `scripts/verify-linux.sh` (80 LOC, Linux verification script)
- [ ] `scripts/verify-windows.ps1` (100 LOC, Windows verification script)
- [ ] `scripts/benchmark-startup.sh` (60 LOC, performance benchmarking)
- [ ] `docs/verification-checklist.md` (120 LOC, manual verification steps)
- [ ] Proposal updated with completion status and release URL

**Verification Completion Criteria**:

- [ ] All platform installation tests pass (macOS, Linux, Windows)
- [ ] Binary size ≤15MB on all platforms
- [ ] Cold start time P95 ≤50ms on all platforms
- [ ] All core commands functional (login, projects, codegen, mcp, docs)
- [ ] Documentation accurate and complete (no broken links)
- [ ] Package manager integrations working (Homebrew, Scoop)
- [ ] No security vulnerabilities (`govulncheck` clean)
- [ ] Release artifacts present and checksums valid

## Phase 13 Summary

**Total Estimated Time**: 6 hours (2.5h + 2h + 1.5h)

**Deliverables Summary** (~20 files):

**Documentation** (8 files):

- `README.md` (200 LOC)
- `docs/installation.md` (300 LOC)
- `docs/configuration.md` (250 LOC)
- `docs/commands/README.md` (150 LOC)
- `docs/ci-cd-integration.md` (400 LOC)
- `docs/claude-desktop-setup.md` (250 LOC)
- `docs/troubleshooting.md` (200 LOC)
- `docs/commands/*.md` (750 LOC auto-generated)

**Build Automation** (6 files):

- `.github/workflows/release.yml` (150 LOC)
- `.goreleaser.yml` (120 LOC)
- `homebrew/emergent.rb` (80 LOC)
- `scoop/emergent.json` (60 LOC)
- `scripts/build-all.sh` (100 LOC)
- `docs/releasing.md` (100 LOC)

**Verification** (7 files):

- `scripts/test-install.sh` (150 LOC)
- `scripts/verify-macos.sh` (100 LOC)
- `scripts/verify-linux.sh` (80 LOC)
- `scripts/verify-windows.ps1` (100 LOC)
- `scripts/benchmark-startup.sh` (60 LOC)
- `docs/verification-checklist.md` (120 LOC)
- `scripts/install.sh` (100 LOC, Unix installer)
- `scripts/install.ps1` (80 LOC, Windows installer)

**Total Lines of Code**: ~3,800 LOC across 23 files

**Platform Coverage Matrix**:

```
Platform    Arch        Package Manager    Distribution Channel
─────────────────────────────────────────────────────────────────
macOS       x86_64      Homebrew          GitHub Releases
macOS       arm64       Homebrew          GitHub Releases
Linux       x86_64      Direct Download   GitHub Releases
Linux       arm64       Direct Download   GitHub Releases
Linux       armv7       Direct Download   GitHub Releases
Windows     x86_64      Scoop             GitHub Releases
Windows     arm64       Direct Download   GitHub Releases
```

**Distribution Channels**:

- **Primary**: GitHub Releases (all platforms, all architectures)
- **macOS**: Homebrew (tap: `emergent/tap`) - automated updates via workflow
- **Windows**: Scoop (bucket: `emergent-bucket`) - automated updates via workflow
- **Universal**: Direct download via curl/PowerShell scripts

**Release Process** (One-Command Release):

```bash
# 1. Create and push tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# 2. GitHub Actions automatically:
#    - Runs tests
#    - Builds 6 platform binaries
#    - Generates checksums
#    - Creates GitHub Release (draft)
#    - Updates Homebrew tap
#    - Updates Scoop bucket

# 3. Review and publish release
#    - Go to GitHub Releases
#    - Review changelog and artifacts
#    - Publish release (draft → published)
```

**Success Criteria** (All must be met):

- [ ] Documentation complete: 8 primary docs + auto-generated command reference
- [ ] Automated builds: 6 platform binaries generated on tag push
- [ ] Package managers: Homebrew formula + Scoop manifest auto-updated
- [ ] Installation verified: Tested on clean macOS, Linux, Windows systems
- [ ] Performance met: Binary size <15MB, cold start P95 <50ms
- [ ] Functionality verified: All core commands (login, projects, codegen, mcp, docs) working
- [ ] Distribution working: Users can install via 4 methods (Homebrew, Scoop, curl, go install)
- [ ] MCP integration: Claude Desktop can connect and use CLI tools
- [ ] CI/CD examples: GitHub Actions, GitLab CI, CircleCI templates tested

**Post-Phase 13 State**:

- Users can discover CLI via README and install in <5 minutes
- Releases fully automated: tag → build → distribute → update package managers
- Multi-platform support verified on clean systems
- Documentation enables self-service troubleshooting
- CI/CD integration examples accelerate adoption by dev teams

## Estimated Effort

| Phase                         | Estimated Time |
| ----------------------------- | -------------- |
| Phase 1: Project Setup        | 2 hours        |
| Phase 2: Configuration        | 3 hours        |
| Phase 3: Authentication       | 4 hours        |
| Phase 4: API Client           | 6 hours        |
| Phase 5: Output Formatters    | 3 hours        |
| Phase 6: Commands             | 10 hours       |
| Phase 7: Template Pack API    | 3 hours        |
| Phase 8: Documentation Server | 4 hours        |
| Phase 9: MCP Proxy Server     | 6 hours        |
| Phase 10: Interactive Prompts | 2 hours        |
| Phase 11: Error Handling      | 2 hours        |
| Phase 12: Testing             | 5 hours        |
| Phase 13: Documentation       | 4 hours        |
| **Total**                     | **~54 hours**  |
