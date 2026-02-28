# sidebar-navigation Specification

## Purpose
TBD - created by archiving change add-imap-data-sources. Update Purpose after archive.
## Requirements
### Requirement: Data Sources Sidebar Section

The system SHALL provide a "Data Sources" section in the sidebar navigation that dynamically displays available content sources.

#### Scenario: Data Sources section displays in sidebar

- **WHEN** a user views the admin sidebar with a project selected
- **THEN** the sidebar SHALL display a "Data Sources" section
- **AND** the section SHALL appear below the project picker and above other project sections

#### Scenario: Documents item always visible

- **WHEN** a user views the Data Sources section
- **THEN** the section SHALL always display a "Documents" menu item
- **AND** the Documents menu item SHALL navigate to `/admin/data-sources/upload`
- **AND** the Documents item SHALL be visible regardless of whether uploaded documents exist

#### Scenario: Dynamic source type items

- **WHEN** documents exist with `source_type` values other than `'upload'`
- **THEN** the sidebar SHALL display menu items for each distinct source type
- **AND** each menu item SHALL use the display name from the source type plugin (e.g., "Emails" for `source_type: 'email'`)
- **AND** each menu item SHALL navigate to `/admin/data-sources/{sourceType}`

#### Scenario: Source type item visibility based on documents

- **WHEN** no documents exist with a particular `source_type`
- **THEN** that source type SHALL NOT appear in the sidebar
- **AND** when the first document of that type is created, the sidebar SHALL update to show the new item

#### Scenario: Data Sources section reflects active state

- **WHEN** a user navigates to any Data Sources page
- **THEN** the corresponding source type menu item SHALL be highlighted as active
- **AND** the "Data Sources" section SHALL remain expanded

### Requirement: Prominent Cross-Project Navigation Items

The sidebar SHALL display prominent Inbox and All Tasks items above the project picker, without a section title, using larger icons to emphasize their importance.

#### Scenario: Prominent items display above project picker

- **WHEN** user views the admin sidebar
- **THEN** Inbox and All Tasks menu items appear above the project picker dropdown
- **AND** these items have no section title grouping them
- **AND** these items use larger icons than standard menu items for visual prominence

#### Scenario: Inbox shows cross-project notification count

- **WHEN** user has unread notifications across any projects
- **THEN** the Inbox menu item displays a badge with the total unread count
- **AND** the count aggregates notifications from all accessible projects

#### Scenario: All Tasks shows cross-project task count

- **WHEN** user has pending tasks across any projects
- **THEN** the All Tasks menu item displays a badge with the total pending count
- **AND** the count aggregates tasks from all accessible projects

### Requirement: All Tasks Page

The system SHALL provide an "All Tasks" page that displays tasks aggregated from all projects the user has access to.

#### Scenario: Viewing all tasks across projects

- **WHEN** user navigates to `/admin/all-tasks`
- **THEN** the page displays tasks from all accessible projects
- **AND** each task indicates which project it belongs to

#### Scenario: Filtering all tasks by status

- **WHEN** user selects a status filter on the All Tasks page
- **THEN** tasks are filtered by the selected status across all projects

### Requirement: Project Navigation Section

The sidebar SHALL display a "Project" section below the project picker containing items scoped to the currently selected project.

#### Scenario: Project section displays with correct title

- **WHEN** user views the admin sidebar with a project selected
- **THEN** a navigation section titled "Project" appears below the project picker
- **AND** this section contains project-scoped items including Tasks, Documents, Chunks, Objects, Chat, and Agents

#### Scenario: Project Tasks shows project-scoped task count

- **WHEN** user has pending tasks in the currently selected project
- **THEN** the Tasks menu item in the Project section displays a badge with the pending count
- **AND** the count only includes tasks from the selected project

### Requirement: Cross-Project Tasks API

The backend SHALL provide API endpoints for fetching tasks across all projects a user has access to.

#### Scenario: Fetching all tasks across projects

- **WHEN** client calls `GET /api/tasks/all`
- **THEN** the API returns tasks from all projects the authenticated user can access
- **AND** each task includes its project ID and project name

#### Scenario: Fetching aggregated task counts

- **WHEN** client calls `GET /api/tasks/all/counts`
- **THEN** the API returns aggregated counts (pending, accepted, rejected, cancelled) across all accessible projects

