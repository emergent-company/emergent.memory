## ADDED Requirements

### Requirement: Interactive controls expose accessible names

Every interactive control — including switches, icon-only buttons and menus, and controls in the menu-bar popover — SHALL expose a non-empty accessible name that identifies its subject. Hiding a visible label SHALL NOT leave a control without an accessible name, and a tooltip SHALL NOT be the only accessible name.

#### Scenario: Empty-title toggle has a name

- **WHEN** a switch is rendered with an empty visible title and a hidden label
- **THEN** it exposes an accessible name naming its subject (for example, the server it enables)

#### Scenario: Icon-only account control has a name

- **WHEN** the account control renders as an icon without visible text
- **THEN** it exposes an accessible name ("Account") in addition to its tooltip

#### Scenario: Controls that keep a visible label are unaffected

- **WHEN** a switch has a visible title alongside its hidden label
- **THEN** it retains its accessible name and no duplicate label is introduced

### Requirement: Rows use stable element identity

Lists and forms that can change SHALL identify rows by element identity, not by collection position. Adding or removing a row SHALL preserve the text, focus, and state of the rows that remain.

#### Scenario: Adding a row preserves existing input

- **WHEN** the user types into an editable row and then adds another row
- **THEN** the typed text and focus of the existing row are unchanged

#### Scenario: Removing a row preserves the others

- **WHEN** the user removes a row from the middle of a list
- **THEN** every remaining row keeps its own value and state

#### Scenario: Read-only lists keep stable identity

- **WHEN** a read-only tool or agent list updates
- **THEN** rows are identified by the element, not by its index

### Requirement: Standard application menu exposes Settings and About

While the app is frontmost, its application menu SHALL offer Settings (⌘,) and About, in addition to the actions already reachable from the menu-bar item.

#### Scenario: Settings shortcut opens settings

- **WHEN** the app is frontmost and the user presses ⌘,
- **THEN** the app opens (raising the main window if needed) to its settings content

#### Scenario: About is reachable from the app menu

- **WHEN** the app is frontmost and the user opens the application menu
- **THEN** an About action is present and opens the About page

### Requirement: Identity and version information is presented once per surface

Each surface SHALL present a given primary action at most once, and SHALL NOT repeat the same sign-in prompt, sign-out action, or version string multiple times on one page. Connector protocol internals (identity provider, OAuth flow, callback scheme) SHALL NOT be shown as primary user-facing body copy.

#### Scenario: One sign-in prompt per page

- **WHEN** the Project & Account page renders while signed out
- **THEN** exactly one prominent sign-in prompt is shown on that page

#### Scenario: One sign-out per page

- **WHEN** a page offers sign-out
- **THEN** it offers it once, not in multiple cards

#### Scenario: Version shown once

- **WHEN** the About page renders
- **THEN** the app version and build are shown once rather than repeated across the page

#### Scenario: Protocol internals are not primary copy

- **WHEN** the About page renders
- **THEN** identity-provider and OAuth-flow details are not shown as primary body copy (at most behind a disclosure or tooltip)

## MODIFIED Requirements

### Requirement: Run as a menu-bar agent app

The app SHALL present a menu-bar item with a status icon and SHALL NOT require a main window for normal operation. Activating the item SHALL expose a compact surface (status summary, tool count, open the app, check for updates, Settings, sign out, and Quit), and the same set of actions SHALL be reachable whether the item is activated by primary or secondary click. Signing out from the menu bar SHALL require confirmation before the session is ended.

#### Scenario: Menu-bar presence

- **WHEN** the app is running
- **THEN** its menu-bar item shows engine/hub state and opens the compact surface and Settings

#### Scenario: Both click paths expose the same actions

- **WHEN** the user activates the menu-bar item by primary click or by secondary click
- **THEN** the same actions are available from both, including open, check for updates, Settings, sign out, and Quit

#### Scenario: Menu-bar sign-out is confirmed

- **WHEN** the user chooses sign out from the menu bar
- **THEN** a confirmation is required before the session ends, and cancelling leaves the session intact
