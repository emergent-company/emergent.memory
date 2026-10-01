## Purpose

Provide a per-user inbox for the Memory platform: a durable log of account-wide events and per-project activity, with an explicit scope separating two inboxes, a stable event taxonomy, opt-in delivery for project noise, actionable items that require a decision, and real-time unread updates. The existing `kb.notifications` store and list/read/dismiss semantics are extended rather than replaced.

## ADDED Requirements

### Requirement: Two inbox scopes

The system SHALL classify every notification as either **account** scope (global events about the user or their memberships) or **project** scope (activity within a single project), and SHALL expose the two as separate inboxes.

#### Scenario: Account event lands in the account inbox

- **WHEN** a user is added to a project, or a user's role or permissions change
- **THEN** the notification is created with account scope and appears in the account inbox, not in any project inbox

#### Scenario: Project activity lands in the project inbox

- **WHEN** a project-scoped event occurs (for example an agent configuration change) in a project the user can access
- **THEN** the notification is created with project scope and appears only in that project's inbox

### Requirement: Explicit notification scope

Every notification record SHALL carry an explicit scope value of `account` or `project`. Project-scoped records SHALL reference a project. Account-scoped records MAY reference a project for link/context without becoming project-scoped.

#### Scenario: Scope is stored, not inferred

- **WHEN** a notification is created
- **THEN** its stored scope reflects the requested scope, regardless of whether a project reference is present

#### Scenario: Account event references a project

- **WHEN** a user is added to project `P`
- **THEN** the notification has scope `account` and references `P` for its link, and it does not appear in `P`'s project inbox

### Requirement: Notification event taxonomy

The system SHALL assign every notification a stable event key drawn from a documented taxonomy, where each key declares its scope, its default delivery, whether it is required, and whether it is actionable.

#### Scenario: Event key recorded

- **WHEN** a notification is created from a known system event
- **THEN** the record carries the taxonomy event key for that event

#### Scenario: Unknown event key

- **WHEN** a notification is requested with an event key not present in the taxonomy
- **THEN** the create call fails validation rather than silently defaulting to a delivered notification

### Requirement: Mandatory account notifications

Account-scope notifications (membership changes, permission and role changes, security events, invitations, and system announcements) SHALL always be delivered to the inbox and SHALL NOT be suppressible by user preferences.

#### Scenario: Permission change is always delivered

- **WHEN** a user's role changes
- **THEN** the account notification is created even if the user has disabled every optional notification

### Requirement: Opt-in project notifications

Project-scope notifications SHALL be created only when the user has opted in for that event key in that project, EXCEPT event keys declared **required** in the taxonomy, which SHALL always be delivered.

#### Scenario: Opted-out project event is suppressed

- **WHEN** a project event with an optional key occurs and the user has not opted in for that key in that project
- **THEN** no notification is created

#### Scenario: Opted-in project event is delivered

- **WHEN** a project event with an optional key occurs and the user has opted in for that key in that project
- **THEN** the notification is created and appears in that project's inbox

#### Scenario: Required project event ignores preferences

- **WHEN** a required project event occurs (for example a task assigned to the user)
- **THEN** the notification is created regardless of the user's preferences

### Requirement: Notification preferences

The system SHALL let a user view and change notification preferences scoped by project, event key, and channel, and changes SHALL apply to notifications created after the change.

#### Scenario: List effective preferences

- **WHEN** a user opens notification preferences for a project
- **THEN** the system returns every project event key with its effective enabled state and default

#### Scenario: Update a preference

- **WHEN** a user enables an optional event key for a project
- **THEN** subsequent notifications for that key in that project are delivered

#### Scenario: Account preferences are not user-suppressible

- **WHEN** a user attempts to disable delivery of an account-scope key
- **THEN** the system does not suppress delivery of that key

### Requirement: Actionable notifications

Notifications that require a user decision SHALL be marked actionable, SHALL expose their available actions and current action state, and SHALL leave the action-required view once resolved.

#### Scenario: Invitation is actionable

- **WHEN** a user receives a project or organization invitation
- **THEN** the notification is actionable and offers accept and decline actions

#### Scenario: Approval links to its request

- **WHEN** an approval request targets the user
- **THEN** the notification is actionable and links to the underlying request to act on it

#### Scenario: Resolve clears the action state

- **WHEN** a user resolves an actionable notification
- **THEN** its action state is recorded and it no longer counts toward the action-required view

### Requirement: Single producer entry point

Notifications SHALL be created through a single service entry point that applies scope, taxonomy, preferences, and grouping, rather than by ad-hoc direct inserts.

#### Scenario: Create applies scope and preferences

- **WHEN** any domain requests a notification through the producer entry point
- **THEN** the resulting record has the correct scope, event key, and grouping, and is suppressed or delivered according to preferences

#### Scenario: Grouping coalesces related events

- **WHEN** multiple events share a group key for the same user
- **THEN** they are coalesced according to the existing grouping behavior instead of producing unbounded duplicate rows

### Requirement: Real-time notification delivery

Creating a notification SHALL publish a notification event on the platform event bus so connected clients can reflect new and unread changes without a manual refresh.

#### Scenario: Event published on create

- **WHEN** a notification is created through the producer entry point
- **THEN** a notification entity event is published carrying the notification and its project scope

#### Scenario: Counts reconcile after reconnect

- **WHEN** a real-time event is missed (for example after a stream drop)
- **THEN** the client's unread counts reconcile from the counts endpoint on load or reconnect

### Requirement: List notifications by scope

The notification list SHALL support filtering by scope and, for project scope, by project, in addition to the existing tab, unread-only, category, and search filters. The counts endpoint SHALL honour the same scope filter.

#### Scenario: List the account inbox

- **WHEN** a user requests the account scope
- **THEN** the response contains only account-scope notifications for that user

#### Scenario: List a project inbox

- **WHEN** a user requests project scope for a project they can access
- **THEN** the response contains only that project's notifications

#### Scenario: Counts match the scope filter

- **WHEN** counts are requested for a scope
- **THEN** the returned counts correspond to that scope only

### Requirement: Scope-aware bulk read

Marking all notifications read SHALL be applicable within the current scope so that clearing the account inbox does not clear a project inbox, and vice versa.

#### Scenario: Mark all read within a scope

- **WHEN** a user marks all read for the account scope
- **THEN** only account-scope notifications become read, and project-scope unread state is unchanged
