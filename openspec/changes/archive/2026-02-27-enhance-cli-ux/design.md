## Context

The `emergent-cli` is a Cobra-based Go CLI at `tools/emergent-cli/` that provides basic command execution for managing projects, documents, and resources. It currently supports `--output` formats (table, json, yaml) and basic CRUD operations but lacks:

- Shell completion (Cobra supports this but it's not wired up)
- Interactive modes for exploring resources
- Rich terminal formatting (colors, styled tables, progress indicators)
- Dynamic resource completion (project names, document IDs, etc.)

The CLI is used by developers and power users who need to script operations or quickly query the knowledge base without opening the admin UI. Improving discoverability and reducing friction will increase CLI adoption and productivity.

**Current stack:**

- Go CLI with `github.com/spf13/cobra` for commands
- `github.com/spf13/viper` for configuration
- Basic table output (no styling library currently)

**Constraints:**

- Must remain backwards compatible with existing command structure
- Cannot require server-side changes for basic completion (should work offline for static completions)
- TUI mode should be optional (existing commands work as before)

## Goals / Non-Goals

**Goals:**

- Enable shell tab completion for all commands, flags, and dynamic resources (projects, documents)
- Provide an optional `emergent-cli browse` TUI mode for interactive exploration
- Enhance default output with styled tables, colors, and better readability
- Support filtering and pagination for list commands (e.g., `emergent-cli projects list --filter name=foo --limit 20`)
- Generate completion scripts for bash, zsh, fish, and PowerShell

**Non-Goals:**

- Full-featured IDE or visual editor within the CLI
- Real-time collaboration features in TUI mode
- Replacing the admin web UI (CLI is for power users and automation)
- GUI application or Electron wrapper

## Decisions

### 1. Use Cobra's Built-in Completion + Custom Dynamic Completions

**Decision:** Leverage Cobra's `GenBashCompletion`, `GenZshCompletion`, etc., and add `ValidArgsFunction` for dynamic resource completions.

**Rationale:**

- Cobra already supports shell completion generation - no need for external libraries
- `ValidArgsFunction` lets us call the API to fetch project names, document IDs dynamically
- Alternatives considered:
  - **posener/complete**: More control but requires reimplementing what Cobra offers
  - **Static completions only**: Simpler but poor UX (no project/document name suggestions)

**Implementation:**

- Add `completion` subcommand: `emergent-cli completion bash > /etc/bash_completion.d/emergent-cli`
- Implement `ValidArgsFunction` for commands that take resource IDs/names (fetch from API with caching)

### 2. Use Bubble Tea for TUI (Interactive Mode)

**Decision:** Implement `emergent-cli browse` using `charmbracelet/bubbletea` with `bubbles` components (list, table, paginator).

**Rationale:**

- Industry standard for Go TUIs (used by `gum`, `gh dash`, `lazygit`)
- Composable model-view-update architecture scales well
- Rich ecosystem: `lipgloss` (styling), `glamour` (markdown), `bubbles` (widgets)
- Alternatives considered:
  - **tview**: More widget-heavy, less composable for our use case
  - **termui**: Chart-focused, overkill for resource browsing
  - **Custom ncurses bindings**: Too low-level, reinventing the wheel

**Implementation:**

- `browse` command launches TUI with tabbed views: Projects, Documents, Extractions
- Tree navigation for project hierarchy (project → documents → extractions)
- Vim-style keybindings (`j/k` navigation, `/` search, `q` quit)

### 3. Use Lipgloss + Table for Rich Output Formatting

**Decision:** Replace plain output with `lipgloss`-styled tables using `charmbracelet/lipgloss` and custom table rendering.

**Rationale:**

- Lipgloss provides terminal-aware styling (colors, borders, alignment)
- Can incrementally enhance existing commands without breaking `--output json/yaml`
- Alternatives considered:
  - **olekukonko/tablewriter**: Popular but less flexible styling
  - **Keep plain text**: Poor UX, hard to scan large outputs

**Implementation:**

- Create `internal/ui` package with reusable table/list renderers
- Respect `--no-color` flag and `NO_COLOR` env var
- Add progress bars for long-running operations (e.g., bulk uploads)

### 4. Resource Queries via Flags (Not a Query DSL)

**Decision:** Add filtering via standard flags (`--filter`, `--limit`, `--offset`, `--sort`) rather than a custom query language.

**Rationale:**

- Flags are standard CLI UX (e.g., `kubectl get pods --selector`, `gh issue list --state open`)
- Easier to implement and document than a DSL like `emergent-cli query "project.name = foo"`
- Alternatives considered:
  - **Custom query DSL**: More powerful but steep learning curve, parsing complexity
  - **JMESPath/jq integration**: Requires JSON output, less discoverable

**Implementation:**

- `--filter key=value[,key=value]` parsed into API query params
- `--limit`, `--offset` for pagination
- `--sort field[:asc|desc]` for ordering

### 5. Leverage Viper for Configuration Management

**Decision:** Extend existing Viper configuration to support new CLI preferences (cache TTL, default query options, UI settings).

**Rationale:**

- Viper is already integrated for basic config (`~/.emergent/config.yaml`)
- Provides consistent config precedence: flags > env vars > config file > defaults
- Users can set personal defaults without always passing flags
- Alternatives considered:
  - **Separate config system**: Redundant when Viper already exists
  - **Flags only**: Tedious for frequently-used options

**Implementation:**

- Extend `~/.emergent/config.yaml` schema:
  ```yaml
  server: https://api.dev.emergent-company.ai
  output: table
  cache:
    ttl: 5m
    enabled: true
  ui:
    compact: false
    color: auto # auto, always, never
    pager: true
  query:
    defaultLimit: 50
    defaultSort: updated_at:desc
  completion:
    timeout: 2s
  ```
- Bind new flags to Viper config keys
- Document config schema in CLI README

## Risks / Trade-offs

**Risk:** Dynamic completions slow down tab completion if API is slow
→ **Mitigation:** Cache completions locally (`~/.emergent/cache/`) with TTL, fallback to empty if timeout

**Risk:** TUI mode increases binary size significantly (Bubble Tea + deps)
→ **Mitigation:** Acceptable trade-off (estimated +2-3MB), still <10MB binary. Consider build tags if needed later.

**Risk:** Users unfamiliar with TUI keybindings
→ **Mitigation:** Show help panel (`?` key) with keybindings, document in CLI help

**Trade-off:** Rich output uses more terminal space (styled tables vs plain text)
→ **Mitigation:** Support `--compact` flag for dense output, respect terminal width

**Risk:** Completion script installation requires manual user action
→ **Mitigation:** Provide clear docs + `emergent-cli completion --help` with copy-paste instructions

## Migration Plan

**Deployment:**

1. Release as new minor version (backwards compatible)
2. Existing commands work unchanged
3. Users opt-in to completion via `emergent-cli completion <shell>`
4. TUI mode is new `browse` command (existing users unaffected)

**Rollout:**

- Phase 1: Rich output formatting (no breaking changes, visual improvement)
- Phase 2: Shell completion (opt-in installation)
- Phase 3: TUI browse mode (new feature)

**Rollback:**

- If issues arise, users can continue using existing commands
- No server-side changes required, so rollback is client-only

## Open Questions

1. **Should we support remote completion caching across machines?**

   - Current plan: Local cache per machine
   - Alternative: Sync cache via server API (more complex, may not be worth it)

2. **How deep should TUI navigation go?**

   - Current plan: Projects → Documents → Extractions (3 levels)
   - Should we support drilling into extraction results/chunks? (Could be noisy)

3. **Should `--output table` be styled by default or require `--styled` flag?**
   - Leaning toward styled by default (better UX), with `--no-color` to disable
   - Need to verify this doesn't break existing scripts that parse output
