# CLI Tool Implementation Plan

**Status**: ✅ Design Phase Complete (5,086 lines, all MVP phases documented)  
**Target**: Production-ready CLI tool in ~1.5-2 months  
**Coverage**: 7 MVP phases + complete architecture design

---

## Executive Summary

### What We Have

- ✅ **Complete design specification** validated by OpenSpec (`--strict` mode passed)
- ✅ **All MVP phases documented** (5,086 lines covering every command, API, error, test)
- ✅ **Core user workflow** fully mapped: Upload → Extract → Query
- ✅ **Technology stack validated** (Go 1.21, Cobra, Viper, go-resty, OAuth Device Flow)

### What We Need

- 🔲 Go module scaffolding and directory structure
- 🔲 Implementation of 7 MVP phases (Phase 1→2→3→5→7→4→8)
- 🔲 Unit + Integration + E2E test suites
- 🔲 Cross-platform builds (darwin/linux/windows × amd64/arm64)
- 🔲 Documentation (README, command reference, examples)

### Implementation Estimate

**Total**: ~31-46 days (1.5-2 months) across 7 phases + documentation

---

## Phase Implementation Order (Dependency-Driven)

Implementation must follow dependency order, not numeric order:

```
Phase 1 (Config)        [Foundation]
   ↓
Phase 2 (Auth)          [API Access]
   ↓
Phase 3 (Documents)     [First Feature]
   ↓
Phase 5 (Extraction)    [Core Workflow - Job Management]
   ↓
Phase 7 (Template Packs) [Discovery Layer]
   ↓
Phase 4 (Chat)          [Query Interface]
   ↓
Phase 8 (Docs Server)   [Polish - Auto-generated docs + MCP]
```

**Rationale**:

- Phase 1 (Config) → Foundation for all other phases (server URL, credentials, profiles)
- Phase 2 (Auth) → Required for API access in all features
- Phase 3 (Documents) → First feature, validates auth + config working together
- Phase 5 (Extraction) → Core workflow for processing documents
- Phase 7 (Template Packs) → Discovery mechanism for extraction options
- Phase 4 (Chat) → Query extracted knowledge (depends on extraction results)
- Phase 8 (Docs Server) → Final polish, leverages all other phases

---

## Detailed Phase Breakdown

### Phase 1: Config Management (~3-5 days)

**Purpose**: Foundation for all other phases - manage server URLs, credentials, profiles

**Key Deliverables**:

- YAML config file loading/saving (`~/.emergent/config.yaml`)
- Profile support (dev/staging/prod environments)
- Environment variable precedence (CLI flags > Env vars > Config file > Defaults)
- Config validation and error messages

**Components**:

- `internal/config/config.go` - Config struct and Viper binding
- `internal/cmd/config.go` - Config commands (set-server, set-defaults, show, logout)
- Secure credential storage (`~/.emergent/credentials.json`, 0600 permissions)

**Testing**:

- Unit: Config loading, saving, precedence rules
- Integration: Profile switching, env var overrides

**Acceptance Criteria**:

- [ ] Can set server URL and persist to config file
- [ ] Can set org/project defaults and load them
- [ ] Config precedence works correctly (flags override config)
- [ ] Config validation provides clear error messages

---

### Phase 2: Authentication (~5-7 days)

**Purpose**: OAuth Device Flow integration with Zitadel for API access

**Key Deliverables**:

- OAuth Device Flow implementation (reuse api-client-mcp pattern)
- Token caching with automatic refresh (5 min buffer)
- Auth middleware for API client (header injection, 401/403 handling)

**Components**:

- `internal/auth/manager.go` - Authenticate(), token exchange
- `internal/auth/token_cache.go` - Expiry checks, refresh flow
- `internal/auth/credentials.go` - Secure storage (0600 permissions)

**Testing**:

- Unit: Token expiry calculation, refresh logic
- Integration: Full OAuth flow (mock OAuth server)
- E2E: Against dev server (requires browser)

**Acceptance Criteria**:

- [ ] Can authenticate via browser OAuth Device Flow
- [ ] Tokens cached and automatically refreshed
- [ ] Auth errors (401/403) handled with actionable messages
- [ ] Token expiry detected and handled gracefully

---

### Phase 3: Documents Commands (~5-7 days)

**Purpose**: First feature - upload, list, get, delete documents

**Key Deliverables**:

- Complete CRUD operations for documents
- File upload with multipart/form-data
- Output formatters (table/json/yaml/csv)

**Components**:

- `internal/api/documents.go` - List(), Get(), Create(), Delete()
- `internal/cmd/documents.go` - Documents commands
- `internal/output/` - Formatter interface + implementations

**Testing**:

- Unit: API client methods, output formatting
- Integration: Mock API responses
- E2E: Full document workflow (upload → list → get → delete)

**Acceptance Criteria**:

- [ ] Can upload document (PDF, TXT, etc.)
- [ ] Can list documents with filtering (table + JSON output)
- [ ] Can get document details by ID
- [ ] Can delete document with confirmation
- [ ] Error handling covers: file not found, invalid format, upload failures

---

### Phase 5: Extraction Commands (~7-10 days)

**Purpose**: Core workflow - start, monitor, retrieve extraction jobs

**Key Deliverables**:

- Job start with template pack selection
- Smart polling (exponential backoff: 1s → 2s → 5s → 10s)
- Job status with detailed progress
- Results retrieval with pagination

**Components**:

- `internal/api/extraction.go` - Run(), Status(), List(), GetResults()
- `internal/cmd/extraction.go` - Extraction commands
- Smart polling logic with timeout limits

**Testing**:

- Unit: Polling backoff logic, status parsing
- Integration: Mock job lifecycle (pending → processing → completed)
- E2E: Full extraction flow (start → poll until complete → get results)

**Acceptance Criteria**:

- [ ] Can start extraction job with document + template pack
- [ ] Can poll job status until completion
- [ ] Can retrieve extraction results (objects, relationships)
- [ ] Error handling covers: invalid job ID, timeout, extraction failures
- [ ] Polling backs off appropriately (doesn't hammer server)

---

### Phase 7: Template Packs Commands (~3-5 days)

**Purpose**: Discovery mechanism for available extraction templates

**Key Deliverables**:

- List available template packs
- View pack details (entity types, relationship types)
- Validation for custom packs (optional MVP)

**Components**:

- `internal/api/template_packs.go` - List(), Get()
- `internal/cmd/template_packs.go` - Template pack commands
- `internal/validation/template_pack.go` - Validation logic (if custom packs supported)

**Testing**:

- Unit: Pack list parsing, info display
- Integration: Mock pack responses
- E2E: List packs, inspect details

**Acceptance Criteria**:

- [ ] Can list available template packs
- [ ] Can view pack details (entity types, relationship types, prompts)
- [ ] Output formatted clearly (table or JSON)
- [ ] Error handling covers: invalid pack ID, no packs available

---

### Phase 4: Chat Commands (~5-7 days)

**Purpose**: Query extracted knowledge using natural language

**Key Deliverables**:

- Query entire knowledge base or specific document
- Plain text output (no markdown in MVP)
- Smart error messages with actionable guidance

**Components**:

- `internal/api/chat.go` - Send()
- `internal/cmd/chat.go` - Chat send command
- Error handling decision tree (404, 400, 408, 401, 500)

**Testing**:

- Unit: Query formatting, response parsing
- Integration: Mock chat responses
- E2E: Full chat workflow (query → get response with sources)

**Acceptance Criteria**:

- [ ] Can query entire knowledge base
- [ ] Can query specific document (`--document` flag)
- [ ] Response includes sources (document IDs + relevance scores)
- [ ] Error handling covers: no extracted knowledge, document not found, query timeout
- [ ] Error messages provide actionable guidance (e.g., "Run 'extraction start' first")

---

### Phase 8: Documentation Server (~3-5 days)

**Purpose**: Auto-generated HTML docs + MCP proxy for AI agents

**Key Deliverables**:

- HTTP server serving Cobra-generated docs (Tailwind CSS, dark mode)
- MCP proxy (stdio + HTTP transports)
- Responsive mobile layout

**Components**:

- `cmd/docs/generator.go` - Cobra introspection, CommandDoc generation
- `cmd/docs/server.go` - HTTP server, route handlers
- `cmd/docs/templates/` - HTML templates (layout, index, command detail)
- `internal/mcp/` - MCP proxy server (tool registration, request/response handling)

**Testing**:

- Unit: Template rendering, command tree walking
- Integration: HTTP endpoints return 200 OK
- E2E: Browse docs, test MCP proxy with Claude Desktop

**Acceptance Criteria**:

- [ ] Docs server runs and serves HTML docs
- [ ] Dark mode works (3-way: system/dark/light)
- [ ] Mobile responsive (hamburger menu)
- [ ] MCP proxy works with Claude Desktop (stdio transport)
- [ ] Command pages show examples, flags, subcommands

---

## Go Module Structure

```
tools/emergent-cli/
├── cmd/
│   ├── main.go                    # Entry point
│   └── docs/                      # Documentation server
│       ├── generator.go           # Cobra → CommandDoc
│       ├── server.go              # HTTP server
│       └── templates/             # HTML templates
│           ├── layout.html
│           ├── index.html
│           ├── command.html
│           └── partials/
│               ├── sidebar.html
│               ├── header.html
│               └── command-card.html
├── internal/
│   ├── config/                    # Config management (Phase 1)
│   │   └── config.go
│   ├── auth/                      # Authentication (Phase 2)
│   │   ├── manager.go
│   │   ├── token_cache.go
│   │   └── credentials.go
│   ├── api/                       # API clients (Phases 3-7)
│   │   ├── client.go              # Base client
│   │   ├── documents.go
│   │   ├── chat.go
│   │   ├── extraction.go
│   │   ├── template_packs.go
│   │   └── admin.go
│   ├── cmd/                       # Cobra commands
│   │   ├── root.go
│   │   ├── config.go
│   │   ├── documents.go
│   │   ├── chat.go
│   │   ├── extraction.go
│   │   ├── template_packs.go
│   │   ├── admin.go
│   │   ├── server.go
│   │   └── serve.go
│   ├── output/                    # Output formatters
│   │   ├── formatter.go
│   │   ├── table.go
│   │   ├── json.go
│   │   ├── yaml.go
│   │   └── csv.go
│   ├── mcp/                       # MCP proxy (Phase 8)
│   │   ├── server.go
│   │   ├── tools.go
│   │   ├── stdio.go
│   │   └── http.go
│   ├── prompt/                    # Interactive prompts
│   │   └── interactive.go
│   └── errors/                    # Error handling
│       └── errors.go
├── go.mod
├── go.sum
├── Makefile                       # Build automation
├── README.md
└── .gitignore
```

---

## Testing Strategy

### Unit Tests

**Coverage Target**: ≥80% per package

**Focus Areas**:

- Config loading/saving, precedence rules
- Token expiry calculation, refresh logic
- API client request construction
- Output formatter conversions
- Error message generation

**Pattern**:

```go
func TestConfigPrecedence(t *testing.T) {
    // CLI flag > Env var > Config file > Default
    // Test each precedence level
}
```

---

### Integration Tests

**Coverage Target**: All API client methods

**Focus Areas**:

- Mock API server responses (200, 400, 401, 403, 404, 500)
- Configuration file handling (read/write)
- OAuth flow with mock OAuth server

**Pattern**:

```go
func TestDocumentsList(t *testing.T) {
    server := httptest.NewServer(...)
    client := api.NewClient(server.URL)
    // Test with mock responses
}
```

---

### E2E Tests

**Coverage Target**: Core user workflows

**Focus Areas**:

- Upload → Extract → Query workflow
- Authentication flow (OAuth Device Flow)
- Error scenarios (network failures, invalid data)

**Prerequisites**:

- Dev server running
- Test user credentials
- Sample documents

**Pattern**:

```bash
# Test script
emergent-cli config set-server --url http://localhost:3002
emergent-cli auth login
emergent-cli documents upload --file test.pdf
emergent-cli extraction start --document doc_123 --pack customer-research
emergent-cli extraction status job_456
emergent-cli chat send "What are the main themes?"
```

---

## Distribution & Release

### Build Targets

Cross-compile for all major platforms:

- darwin/amd64 (macOS Intel)
- darwin/arm64 (macOS Apple Silicon)
- linux/amd64 (Linux x86-64)
- linux/arm64 (Linux ARM64)
- windows/amd64 (Windows x86-64)

**Build Automation** (Makefile):

```makefile
build:
	go build -o emergent-cli cmd/main.go

build-all:
	GOOS=darwin GOARCH=amd64 go build -o dist/emergent-cli-darwin-amd64 cmd/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/emergent-cli-darwin-arm64 cmd/main.go
	GOOS=linux GOARCH=amd64 go build -o dist/emergent-cli-linux-amd64 cmd/main.go
	GOOS=linux GOARCH=arm64 go build -o dist/emergent-cli-linux-arm64 cmd/main.go
	GOOS=windows GOARCH=amd64 go build -o dist/emergent-cli-windows-amd64.exe cmd/main.go

install:
	go install ./cmd/main.go

test:
	go test ./...

lint:
	golangci-lint run
```

---

### GitHub Actions Workflow

```yaml
name: Release

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Build binaries
        run: make build-all

      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: dist/*
```

---

### Distribution Methods

1. **Primary**: GitHub Releases (pre-built binaries)

   - Download from releases page
   - Extract and add to PATH
   - Works on all platforms

2. **Secondary**: `go install` (for Go developers)

   ```bash
   go install github.com/emergent-company/emergent/tools/emergent-cli@latest
   ```

3. **Future**: Homebrew tap (macOS users)
   ```bash
   brew tap emergent-company/tap
   brew install emergent-cli
   ```

---

## Documentation Requirements

### README.md

**Sections**:

- Installation instructions (all platforms)
- Quick start guide (config → auth → upload → extract → query)
- Command overview with examples
- Configuration guide (profiles, env vars)
- Troubleshooting common issues

---

### Command Reference

**Auto-generated** from Cobra help text:

```bash
emergent-cli help > docs/COMMANDS.md
```

**Content**:

- Full command tree
- All flags and their descriptions
- Usage examples for each command
- Output format examples

---

### Integration Guides

**CI/CD Integration**:

```bash
# Use environment variables for automation
export EMERGENT_SERVER_URL=https://api.emergent.com
export EMERGENT_EMAIL=bot@company.com
export EMERGENT_PASSWORD=secret

emergent-cli documents upload --file report.pdf
emergent-cli extraction start --document $doc_id --pack financial-analysis
```

**Claude Desktop Setup**:

```json
{
  "mcpServers": {
    "emergent": {
      "command": "emergent-cli",
      "args": ["serve", "--mcp-stdio"]
    }
  }
}
```

---

## Dependency Management

### Required Go Modules

```go
require (
    github.com/spf13/cobra v1.8.0
    github.com/spf13/viper v1.18.2
    github.com/go-resty/resty/v2 v2.11.0
    github.com/olekukonko/tablewriter v0.0.5
    github.com/AlecAivazis/survey/v2 v2.3.7
    gopkg.in/yaml.v3 v3.0.1
)
```

**Dependency Review**:

- ✅ All dependencies mature and actively maintained
- ✅ No known security vulnerabilities
- ✅ Stable APIs (no breaking changes expected)

---

## Risk Assessment & Mitigation

### Risk 1: OAuth Device Flow Complexity

**Impact**: High (blocks all API access)  
**Probability**: Medium  
**Mitigation**:

- Reuse api-client-mcp OAuth implementation (proven pattern)
- Test OAuth flow early (Phase 2)
- Provide clear error messages for auth failures
- Fallback: Manual token input if OAuth fails

---

### Risk 2: API Version Drift

**Impact**: Medium (endpoints may change)  
**Probability**: Low  
**Mitigation**:

- Version check on startup (compare CLI version with server version)
- Clear error messages when API incompatibility detected
- Document supported server versions in README
- Semantic versioning (major version bump for breaking changes)

---

### Risk 3: Cross-Platform Build Issues

**Impact**: Medium (some platforms may not work)  
**Probability**: Low  
**Mitigation**:

- Test binaries on all target platforms before release
- Use CI/CD to automate cross-compilation
- Provide checksums for binary verification
- Document platform-specific issues in README

---

### Risk 4: Token Security

**Impact**: High (credential exposure)  
**Probability**: Medium  
**Mitigation**:

- Store credentials in `~/.emergent/credentials.json` with 0600 permissions
- Warn users if file permissions are insecure
- Never log tokens (mask in debug output)
- Environment variable fallback for CI/CD (ephemeral tokens)

---

## Success Criteria

Before declaring Phase 8 complete and CLI tool production-ready:

### Functional Requirements

- [ ] All 7 MVP phases implemented (1→2→3→5→7→4→8)
- [ ] Core workflow works: Upload → Extract → Query
- [ ] Authentication flow works reliably (OAuth Device Flow)
- [ ] All commands have help text and examples
- [ ] Error messages are actionable and user-friendly

---

### Quality Requirements

- [ ] Unit test coverage ≥80% per package
- [ ] Integration tests cover all API client methods
- [ ] E2E tests pass against dev server
- [ ] No hardcoded credentials or tokens
- [ ] All dependencies up-to-date and secure

---

### Distribution Requirements

- [ ] Binaries build successfully for all platforms
- [ ] GitHub Actions workflow works (automated releases)
- [ ] README is complete with installation instructions
- [ ] Command reference is auto-generated and accurate
- [ ] Claude Desktop integration works (MCP proxy)

---

### Documentation Requirements

- [ ] README.md complete with quick start guide
- [ ] Command reference auto-generated from Cobra
- [ ] CI/CD integration examples documented
- [ ] Troubleshooting guide covers common issues
- [ ] Architecture documentation explains design decisions

---

## Next Steps (Decision Required)

**You have 4 options**:

### Option A: Start Implementation (Phase 1)

**Action**: Begin Phase 1 (Config Management) implementation  
**Timeline**: 3-5 days for Phase 1  
**Deliverable**: Working config commands (set-server, set-defaults, show, logout)

### Option B: Create Detailed Implementation Tasks

**Action**: Break down each phase into discrete coding tasks  
**Timeline**: 1-2 hours  
**Deliverable**: Updated `tasks.md` with granular task list

### Option C: Add Phase 6 (Admin Commands)

**Action**: Design admin commands (orgs, projects, users management)  
**Timeline**: ~2-3 hours  
**Deliverable**: Phase 6 design (~100-150 lines)

### Option D: Review & Approve Design

**Action**: Full design review, discuss scope/timeline/priorities  
**Timeline**: 30-60 minutes  
**Deliverable**: Approved design, confirmed implementation approach

---

## Recommendation

**Proceed with Option B** (Create Detailed Implementation Tasks) because:

1. ✅ Design is complete and validated (5,086 lines)
2. ✅ Implementation plan provides high-level structure
3. ⚠️ Need granular tasks for effective execution
4. ⚠️ Tasks.md currently has abstract phases, not concrete tasks
5. ⚠️ Detailed tasks enable parallel work, progress tracking, effort estimation

**Next**: Break down each phase into discrete coding tasks (~20-30 tasks per phase).

**After that**: Begin Phase 1 implementation OR request your approval/feedback.

---

## Questions to Resolve

Before starting implementation, confirm:

1. **Scope**: Skip Phase 6 (Admin Commands) for MVP or include it?
2. **Timeline**: Is 1.5-2 months acceptable?
3. **Resource**: Who will implement? (AI-generated code + human review OR human developers?)
4. **Priorities**: Any commands more urgent than others?
5. **Platform**: Which platforms are highest priority? (darwin > linux > windows?)

**Awaiting your decision**: Which option (A/B/C/D) should we proceed with?
