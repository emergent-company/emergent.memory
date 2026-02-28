## Why

The Terminal User Interface (TUI) currently lacks the ability to execute searches when the user triggers the Search input (`/`), despite having a stubbed `performSearch` function. However, the exact functionality we need is already implemented in `performQuery`. We can leverage this existing implementation to wire up the search feature.

## What Changes

- Remove the empty `performSearch` function stub in `tools/emergent-cli/internal/tui/tui.go`.
- Update `handleSearchInput` to reuse `performQuery` which correctly invokes the SDK's search functionality.
- Update `handleSearchInput` to automatically switch the TUI to the Query view so the user can immediately see their search results.

## Capabilities

### New Capabilities
- `tui-search`: The ability to perform a search from the TUI's search input (`/`) and view the results in the Query view.

### Modified Capabilities

## Impact

- `tools/emergent-cli/internal/tui/tui.go`: This is where the core logic will be modified to wire up the search input to the existing query functionality.