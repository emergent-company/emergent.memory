## ADDED Requirements

### Requirement: Launch interactive browse mode

The CLI SHALL provide a `browse` command that launches a terminal UI for exploring resources.

#### Scenario: Launch browse mode

- **WHEN** user runs `emergent-cli browse`
- **THEN** system launches a full-screen TUI with project list view

#### Scenario: Exit browse mode

- **WHEN** user presses `q` key in browse mode
- **THEN** system exits TUI and returns to shell

#### Scenario: Show help panel

- **WHEN** user presses `?` key in browse mode
- **THEN** system displays keybindings and navigation help

### Requirement: Project list navigation

The CLI browse mode SHALL display a list of projects with basic navigation.

#### Scenario: Display project list

- **WHEN** user launches browse mode
- **THEN** system displays scrollable list of projects with name, ID, and last updated date

#### Scenario: Navigate with keyboard

- **WHEN** user presses `j` or down arrow
- **THEN** cursor moves to next project in list

#### Scenario: Navigate up with keyboard

- **WHEN** user presses `k` or up arrow
- **THEN** cursor moves to previous project in list

#### Scenario: Select project to drill down

- **WHEN** user presses Enter on a project
- **THEN** system navigates to document list for that project

### Requirement: Document list view

The CLI browse mode SHALL display documents within a selected project.

#### Scenario: Display document list for project

- **WHEN** user selects a project
- **THEN** system displays scrollable list of documents with title, ID, and status

#### Scenario: Navigate back to project list

- **WHEN** user presses `Esc` or backspace in document list
- **THEN** system returns to project list view

#### Scenario: Select document to view details

- **WHEN** user presses Enter on a document
- **THEN** system displays document details panel (metadata, extraction status)

### Requirement: Search within views

The CLI browse mode SHALL support searching/filtering within current view.

#### Scenario: Enter search mode

- **WHEN** user presses `/` key
- **THEN** system displays search input at bottom of screen

#### Scenario: Filter list by search term

- **WHEN** user types "report" in search input
- **THEN** system filters visible items to only those containing "report"

#### Scenario: Clear search

- **WHEN** user presses Esc in search mode
- **THEN** system clears search and shows all items again

### Requirement: Multi-tab interface

The CLI browse mode SHALL support tabbed views for different resource types.

#### Scenario: Switch between tabs

- **WHEN** user presses `Tab` key
- **THEN** system cycles through views: Projects, Documents, Extractions

#### Scenario: Display active tab indicator

- **WHEN** browse mode is open
- **THEN** system shows tab bar at top with active tab highlighted

### Requirement: Pagination for large lists

The CLI browse mode SHALL paginate large resource lists to maintain performance.

#### Scenario: Load first page of results

- **WHEN** user opens a view with 1000+ items
- **THEN** system loads and displays first 100 items

#### Scenario: Load more results on scroll

- **WHEN** user scrolls to bottom of list
- **THEN** system fetches and appends next page of results

#### Scenario: Show loading indicator

- **WHEN** system is fetching next page
- **THEN** system displays loading spinner at bottom of list

### Requirement: Responsive terminal layout

The CLI browse mode SHALL adapt to terminal size changes.

#### Scenario: Handle window resize

- **WHEN** user resizes terminal window
- **THEN** system reflows content to fit new dimensions

#### Scenario: Minimum terminal size

- **WHEN** terminal is smaller than 80x24 characters
- **THEN** system displays warning message about minimum size requirement
