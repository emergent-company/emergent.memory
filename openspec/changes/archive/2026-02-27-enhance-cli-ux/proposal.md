## Why

The existing `emergent-cli` (Cobra-based) provides basic command execution but lacks modern CLI UX features that improve productivity and discoverability. Users cannot easily explore available resources, must memorize exact command syntax, and receive plain text output that's hard to scan. Enhanced tab completion, interactive browsing modes, and rich formatting (tables, colors) will make the CLI faster and more intuitive for both new and power users.

## What Changes

- **Tab Completion**: Add shell completion for commands, flags, and dynamic resources (projects, documents, etc.)
- **Interactive Browsing Mode**: Implement a TUI (terminal UI) for exploring resources - think `htop` for Emergent objects
- **Rich Output Formatting**: Replace plain text with formatted tables, syntax-highlighted JSON/YAML, colored status indicators, and progress bars
- **Resource Navigation**: Add commands to list, filter, and inspect projects, documents, extractions, and other entities with pagination
- **Query Interface**: Provide structured query capabilities with filters and search across resources
- **Smart Defaults**: Infer context (current project, recent resources) to reduce typing

## Capabilities

### New Capabilities

- `cli-tab-completion`: Shell integration for bash/zsh/fish completion with dynamic resource suggestions
- `cli-interactive-mode`: TUI browsing interface for navigating project hierarchy and resources
- `cli-rich-output`: Enhanced output formatting with tables, colors, progress indicators, and syntax highlighting
- `cli-resource-queries`: Structured commands for listing, filtering, and searching resources with pagination

### Modified Capabilities

<!-- No existing capabilities require spec-level requirement changes - this is additive -->

## Impact

- **Code**:
  - `tools/emergent-cli/` - Add new packages for TUI, completion generation, and output formatting
  - New dependencies: likely `bubbletea` (TUI framework), `glamour` (markdown rendering), `lipgloss` (styling)
- **User Experience**: Users will need to install shell completion (opt-in) and learn new interactive mode commands
- **Documentation**: CLI docs need updates for new modes and completion setup
- **Testing**: New integration tests for TUI flows and completion script generation
