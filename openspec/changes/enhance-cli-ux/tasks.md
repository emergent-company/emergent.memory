## 1. Project Setup

- [ ] 1.1 Add Go dependencies: bubbletea, bubbles, lipgloss, glamour
- [ ] 1.2 Create `internal/ui` package for output formatting utilities
- [ ] 1.3 Create `internal/completion` package for shell completion logic
- [ ] 1.4 Create `internal/tui` package for interactive browse mode
- [ ] 1.5 Create `internal/cache` package for completion caching
- [ ] 1.6 Extend Viper config schema for cache, ui, query, and completion settings
- [ ] 1.7 Document config schema in tools/emergent-cli/README.md with example config.yaml

## 2. Configuration and Shell Completion Infrastructure

- [ ] 2.1 Bind new flags to Viper (cache.ttl, ui.compact, ui.color, query.defaultLimit, etc.)
- [ ] 2.2 Add config value helpers to read from Viper with defaults
- [ ] 2.3 Add `completion` subcommand to root command with bash/zsh/fish/powershell support
- [ ] 2.4 Implement completion script generation (wrap Cobra's built-in generators)
- [ ] 2.5 Add installation instructions to `completion --help` output
- [ ] 2.6 Create `ValidArgsFunction` for static flag value completions (e.g., --output values)
- [ ] 2.7 Implement cache manager with configurable TTL (default from Viper)

## 3. Dynamic Resource Completion

- [ ] 3.1 Create API client wrapper for completion requests with configurable timeout (default from Viper)
- [ ] 3.2 Implement `ValidArgsFunction` for project name completion
- [ ] 3.3 Implement `ValidArgsFunction` for document ID completion (context-aware for --project flag)
- [ ] 3.4 Add completion caching to `~/.emergent/cache/` directory
- [ ] 3.5 Implement graceful fallback when API is unreachable (return empty suggestions)
- [ ] 3.6 Add cache refresh logic for stale entries (configurable TTL from Viper)

## 4. Rich Output Formatting - Core

- [ ] 4.1 Create styled table renderer using lipgloss in `internal/ui/table.go`
- [ ] 4.2 Implement terminal size detection and responsive column width
- [ ] 4.3 Add color scheme configuration (respect --no-color, NO_COLOR env var, and Viper ui.color setting)
- [ ] 4.4 Create status indicator helpers (green checkmark, red X, yellow warning)
- [ ] 4.5 Implement Unicode/ASCII fallback detection for box-drawing characters
- [ ] 4.6 Read ui.compact default from Viper config

## 5. Rich Output Formatting - Advanced

- [ ] 5.1 Add syntax highlighting for JSON output using lipgloss
- [ ] 5.2 Add syntax highlighting for YAML output using lipgloss
- [ ] 5.3 Implement progress spinner for long API calls (>1 second)
- [ ] 5.4 Implement progress bar for bulk operations (upload, download)
- [ ] 5.5 Add --compact flag support for dense output layout
- [ ] 5.6 Implement text truncation with ellipsis for long values
- [ ] 5.7 Add automatic pager integration (respect $PAGER env var and Viper ui.pager setting)
- [ ] 5.8 Detect piped output and disable progress indicators/colors

## 6. Resource Query Flags

- [ ] 6.1 Add --filter flag parsing (comma-separated key=value pairs)
- [ ] 6.2 Implement filter logic for `projects list` command
- [ ] 6.3 Implement filter logic for `documents list` command
- [ ] 6.4 Add --sort flag parsing (field:asc|desc format, default from Viper query.defaultSort)
- [ ] 6.5 Implement sorting for list commands
- [ ] 6.6 Add --limit and --offset flags for pagination (default limit from Viper query.defaultLimit)
- [ ] 6.7 Implement pagination API integration and metadata display ("Showing X-Y of Z")

## 7. Resource Query - Advanced

- [ ] 7.1 Add --search flag for full-text search across resources
- [ ] 7.2 Add --fields flag for column selection
- [ ] 7.3 Implement date range filtering with --from and --to flags
- [ ] 7.4 Add relative date parsing (e.g., "7d", "1w", "3m")
- [ ] 7.5 Add --count-only flag to show only result count
- [ ] 7.6 Add CSV export format to --output flag options

## 8. TUI - Core Framework

- [ ] 8.1 Create Bubble Tea app structure in `internal/tui/app.go`
- [ ] 8.2 Implement model-view-update pattern for browse command
- [ ] 8.3 Add `browse` subcommand to root command
- [ ] 8.4 Implement keybinding system (j/k navigation, q quit, ? help)
- [ ] 8.5 Add help panel view with keybindings display
- [ ] 8.6 Implement terminal resize handling

## 9. TUI - Project List View

- [ ] 9.1 Create project list model using bubbles list component
- [ ] 9.2 Implement API integration to fetch projects
- [ ] 9.3 Add keyboard navigation (j/k/arrows, Enter to select)
- [ ] 9.4 Style project list with lipgloss (borders, headers, selected state)
- [ ] 9.5 Implement pagination for 100+ projects (load more on scroll)
- [ ] 9.6 Add loading spinner while fetching data

## 10. TUI - Document List View

- [ ] 10.1 Create document list view for selected project
- [ ] 10.2 Implement back navigation (Esc/backspace to project list)
- [ ] 10.3 Display document metadata (title, ID, status, updated date)
- [ ] 10.4 Add Enter key to drill into document details panel
- [ ] 10.5 Implement pagination for document lists

## 11. TUI - Search and Filtering

- [ ] 11.1 Implement search mode (activate with `/` key)
- [ ] 11.2 Add search input box at bottom of screen
- [ ] 11.3 Filter visible items based on search term (case-insensitive)
- [ ] 11.4 Add Esc key to clear search and return to full list
- [ ] 11.5 Show search result count in status bar

## 12. TUI - Multi-Tab Interface

- [ ] 12.1 Create tab bar component at top of TUI
- [ ] 12.2 Implement Tab key cycling through views (Projects, Documents, Extractions)
- [ ] 12.3 Add visual indicator for active tab
- [ ] 12.4 Wire up extraction list view (similar to documents)

## 13. TUI - Polish and Error Handling

- [ ] 13.1 Implement minimum terminal size check (80x24) with warning message
- [ ] 13.2 Add error state handling for API failures (show error message in TUI)
- [ ] 13.3 Add empty state views ("No projects found")
- [ ] 13.4 Implement status bar at bottom showing current context

## 14. Testing

- [ ] 14.1 Add unit tests for completion cache logic
- [ ] 14.2 Add unit tests for filter/sort/pagination parsing
- [ ] 14.3 Add unit tests for table rendering (width calculation, truncation)
- [ ] 14.4 Add integration test for completion script generation
- [ ] 14.5 Add mock API client for TUI model testing
- [ ] 14.6 Test NO_COLOR and --no-color flag behavior

## 15. Documentation

- [ ] 15.1 Update CLI README with completion installation instructions
- [ ] 15.2 Document all new flags (--filter, --sort, --limit, etc.) in command help text
- [ ] 15.3 Add browse mode tutorial to docs (keybindings, navigation)
- [ ] 15.4 Create examples for common query patterns (filtering, sorting, searching)
- [ ] 15.5 Document cache location and TTL configuration

## 16. Backwards Compatibility Verification

- [ ] 16.1 Verify existing commands work unchanged without new flags
- [ ] 16.2 Test that --output json/yaml remain unaffected by new formatting
- [ ] 16.3 Ensure completion is opt-in (doesn't break existing scripts)
- [ ] 16.4 Test that TUI is optional (browse command doesn't interfere with other commands)
