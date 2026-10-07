## Why

A UI/UX pass over the mac connector app found accessibility names missing on empty-title toggles and icon-only controls, editable MCP argument rows keyed by collection index (so typed text and focus can jump to the wrong row on add/remove), a soft-deprecated `MenuStyle`, a menu-bar item whose left- and right-click paths expose different actions and whose sign-out is unconfirmed, no Settings/About application menu, duplicated sign-in and version content, and no localization foundation at all.

## What Changes

- Give every interactive control a non-empty accessible name, including empty-title switches and icon-only controls (a tooltip is not a name).
- Identify list and form rows by element identity, not position, so adding/removing a row preserves the other rows' text, focus, and state.
- Replace the soft-deprecated `MenuStyle.borderlessButton` with the supported modifier combination (`.menuStyle(.button)` + `.buttonStyle(.borderless)`), preserving the current visual appearance.
- Make the menu-bar item expose the same actions on primary and secondary click, and require confirmation before signing out from it, matching the in-window confirmation.
- Add a standard application menu with Settings (⌘,) and About while the app is frontmost.
- Present each primary action and version string once per surface; stop showing connector protocol internals (identity provider, OAuth flow, callback scheme) as primary body copy.
- Add a String Catalog and make all user-facing text localizable: no runtime case transforms on user text, list values via `formatted()` instead of `joined(separator:)`, semantic text styles instead of fixed point sizes, and `LocalizedStringResource` on non-view types.

## Capabilities

### New Capabilities

- `mac-connector-localization`: user-facing connector text is localizable, follows system text styles (so it scales and does not clip longer translations), and lays out for right-to-left locales.

### Modified Capabilities

- `mac-connector-app`: adds accessible control naming, stable row identity, a standard Settings/About application menu, a unified and confirmation-guarded menu-bar action set, and single-presentation of identity/version content. The existing "Run as a menu-bar agent app" requirement is updated to cover both click paths and confirmed sign-out.

## Impact

- **Views**: `MainWindowView.swift`, `MenuBarView.swift`, `StatusItemController.swift`, `ProjectAccountPage.swift`, `ProjectSwitcherView.swift`, `MCPServersPage.swift`, `MCPServerDetailView.swift`, `AddMCPServerSheet.swift`, `MCPRegistryBrowseSheet.swift`, `MCPFormEditors.swift`, `ToolsPage.swift`, `PermissionsPage.swift`, `ConnectionPage.swift`, `AboutPage.swift`, `AccountToolbarView.swift`, `AgentDetailView.swift`, `DashboardPage.swift`, `ConnectorTheme.swift`, `IdentifierReveal.swift`, `MemoryConnectorApp.swift`.
- **Build**: `project.yml` gains a String Catalog source entry.
- **Tests**: unit tests for row-identity behavior, accessible names, and localized-string helpers; menu-bar behavior requires a manual smoke on a Mac.
- **No server, API, or CLI changes.**
