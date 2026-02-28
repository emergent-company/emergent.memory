# Change: CLI Tool for Remote Server Control (Go Implementation)

## Why

Users need to control the Emergent Server remotely via command line for:

- **Automation scripts**: Batch document imports, scheduled operations
- **CI/CD pipelines**: Automated testing, deployment verification
- **Server administration**: Quick health checks, user management
- **Debugging**: Troubleshooting without browser access
- **Bulk operations**: Mass document processing, extraction jobs
- **AI Agent Integration**: MCP proxy for Claude Desktop and other AI agents

**Current Limitation**: Admin UI requires browser access. No programmatic CLI access exists for automation or headless environments.

## What Changes

### New Capability

- **Spec**: `specs/cli-tool/spec.md` (new capability)
- **Package**: `tools/emergent-cli/` (Go implementation)

### Technology Stack

| Component     | Choice      | Rationale                                   |
| ------------- | ----------- | ------------------------------------------- |
| Language      | Go 1.21+    | Single binary, fast startup, cross-platform |
| CLI Framework | Cobra       | Industry standard (kubectl, gh, docker)     |
| Config        | Viper       | YAML/JSON/env support, Cobra integration    |
| HTTP Client   | go-resty    | Clean API, retry, JSON marshaling           |
| Output        | tablewriter | Terminal-friendly tables                    |
| Prompts       | survey      | Interactive user input                      |

### Authentication

- **Pattern**: Password Grant Flow (reuse api-client-mcp pattern)
- **Storage**: File-based credentials (`~/.emergent/credentials.json`, 0600 permissions)
- **Tokens**: Cached with automatic refresh
- **CI/CD**: Environment variable fallback

### Commands

```
emergent-cli config          # Configuration management
emergent-cli documents       # Document CRUD operations
emergent-cli chat            # Chat interactions
emergent-cli extraction      # Extraction job management
emergent-cli template-packs  # Template pack management
emergent-cli admin           # Organization/project/user management
emergent-cli server          # Health and info
emergent-cli serve           # Docs server + MCP proxy
emergent-cli completion      # Shell completion scripts
```

### Serve Mode (Multi-Purpose Server)

The CLI can run as a persistent server exposing:

| Mode        | Command                                  | Purpose                             |
| ----------- | ---------------------------------------- | ----------------------------------- |
| Docs Server | `serve --docs-port 8080`                 | Auto-generated HTML docs from Cobra |
| MCP stdio   | `serve --mcp-stdio`                      | MCP proxy for Claude Desktop        |
| MCP HTTP    | `serve --mcp-port 3100`                  | MCP proxy for web clients           |
| Combined    | `serve --docs-port 8080 --mcp-port 3100` | Both servers                        |

**MCP Integration**: Each CLI command becomes an MCP tool. AI agents can:

- Upload and manage documents
- Run and monitor extractions
- Query the knowledge base
- Manage template packs

### Output Formats

- `table` (default, human-readable)
- `json` (scripting, jq integration)
- `yaml` (configuration-friendly)
- `csv` (spreadsheet export)

### Distribution

- **Primary**: GitHub Releases (pre-built binaries for darwin/linux/windows, amd64/arm64)
- **Secondary**: `go install` for Go developers
- **Future**: Homebrew tap for macOS users

## Impact

### Affected Specs

- None (completely new capability)

### Affected Code

- **New**: `tools/emergent-cli/` (complete Go package)
- **Reference**: `openspec/specs/api-client-mcp/spec.md` (auth pattern)
- **Reference**: `openspec/specs/authentication/spec.md` (OAuth patterns)

### Breaking Changes

**None** - This is a completely additive change. No existing functionality is modified.

### Backend Changes Required

**None** - CLI uses existing API endpoints. No server modifications needed.

### Dependencies (Go modules)

| Module                              | Purpose                  |
| ----------------------------------- | ------------------------ |
| `github.com/spf13/cobra`            | CLI framework            |
| `github.com/spf13/viper`            | Configuration management |
| `github.com/go-resty/resty/v2`      | HTTP client              |
| `github.com/olekukonko/tablewriter` | Table output             |
| `github.com/AlecAivazis/survey/v2`  | Interactive prompts      |
| `gopkg.in/yaml.v3`                  | YAML encoding            |

## Risks & Mitigations

| Risk                  | Mitigation                                        |
| --------------------- | ------------------------------------------------- |
| Credential exposure   | 0600 file permissions, env var fallback for CI/CD |
| Token leakage in logs | Never log tokens, mask in debug output            |
| API version drift     | Version check on startup, clear error messages    |
| Breaking CLI changes  | Semver, deprecation warnings before removal       |

## Success Criteria

- [ ] All core commands implemented (config, documents, chat, extraction, template-packs, admin, server, serve)
- [ ] Authentication flow works reliably (password grant, token refresh)
- [ ] Binaries build for all target platforms (darwin/linux/windows × amd64/arm64)
- [ ] E2E tests pass against dev server
- [ ] Documentation server works (`serve --docs-port`)
- [ ] MCP proxy works with Claude Desktop (`serve --mcp-stdio`)
- [ ] Documentation complete (README, help text, examples, Claude Desktop setup)
