## Purpose

Provide the web console surface for the Inbox subsystem: a header bell with an unread indicator that links to an inbox page, and an inbox page that separates the account inbox from the per-project inbox with tabs, unread marking, and inline actions.

## ADDED Requirements

### Requirement: Notification bell with unread indicator

The console SHALL show a bell in the top navigation that links to the inbox and displays an unread indicator (a dot, or a count when greater than zero) driven by the unread count of the scope the bell links to — the account inbox on every page, or the project inbox while a project-scoped inbox is shown. The bell count and the inbox list for that scope SHALL be read from the same scope-filtered count, so the badge can never disagree with the list (#1342).

#### Scenario: Unread notifications show an indicator

- **WHEN** a signed-in user with unread notifications loads any console page
- **THEN** the bell shows an unread dot or count

#### Scenario: Bell agrees with the inbox for the active scope

- **GIVEN** a user with unread account-scope and unread project-scope notifications
- **WHEN** the account inbox is shown
- **THEN** the bell count equals the account inbox's unread count, not the sum of both scopes
- **AND** when the project inbox is shown, the bell count equals that project inbox's unread count

#### Scenario: No unread notifications

- **WHEN** a signed-in user has no unread notifications
- **THEN** the bell shows no unread indicator

#### Scenario: Bell links to the inbox

- **WHEN** a user activates the bell
- **THEN** the console navigates to the inbox view

### Requirement: Inbox scope switch

The inbox SHALL present two scopes — Account and Project — and SHALL list the notifications belonging to the selected scope.

#### Scenario: Account scope

- **WHEN** the user selects the Account scope
- **THEN** the inbox lists account-scope notifications (global events such as being added to a project or permission changes)
- **AND** the account inbox is global: it SHALL NOT be narrowed to the active project, even though individual account events may link to a project (#1342)

#### Scenario: Project scope

- **WHEN** the user selects the Project scope with an active project
- **THEN** the inbox lists that project's notifications only

#### Scenario: No active project

- **WHEN** the user selects the Project scope with no active project
- **THEN** the console shows an empty state explaining that a project must be selected

### Requirement: Inbox tabs

Within a scope the inbox SHALL offer tabs for All, Unread, Action required, Snoozed, and Cleared, and SHALL show tab counts.

#### Scenario: Filter by tab

- **WHEN** the user selects a tab such as Unread or Action required
- **THEN** the inbox lists only the notifications belonging to that tab

#### Scenario: Empty tab

- **WHEN** the selected tab has no notifications
- **THEN** the console shows an empty state, not an error

### Requirement: Unread marking in the list

The inbox SHALL visually distinguish unread notifications from read ones and SHALL let the user mark a notification read or unread and mark all read within the current scope.

#### Scenario: Unread items are distinguished

- **WHEN** the inbox lists a mix of read and unread notifications
- **THEN** unread items are visually marked as unread

#### Scenario: Mark read updates the indicator

- **WHEN** a user marks a notification read
- **THEN** the item loses its unread marking and the bell indicator updates

### Requirement: Inline actions on actionable notifications

The inbox SHALL render action controls for actionable notifications and SHALL reflect their state after a resolution.

#### Scenario: Invitation actions

- **WHEN** the inbox shows an actionable invitation
- **THEN** the item offers accept and decline controls that resolve the notification inline

#### Scenario: Jump to an actionable target

- **WHEN** the inbox shows an actionable notification with a target (such as an approval request)
- **THEN** the item offers a control that navigates to the target

### Requirement: Project inbox opt-in

The Project inbox SHALL communicate that activity notifications are opt-in and SHALL provide an entry point to manage per-project notification preferences.

#### Scenario: Opt-in entry point

- **WHEN** the user views the Project inbox
- **THEN** an entry point to manage that project's notification preferences is available

#### Scenario: All-optional events and no opt-ins

- **WHEN** the Project inbox has no notifications because the user has not opted into any optional events
- **THEN** the console shows an empty state that points to the notification preferences

### Requirement: Real-time inbox updates

The inbox and its bell indicator SHALL update without a manual refresh when new notifications arrive for the active scope, and SHALL reconcile against the counts endpoint on load, scope switch, and reconnect.

#### Scenario: New notification appears live

- **WHEN** a notification is created for the active scope while the console is open
- **THEN** the inbox list and the bell indicator update without a manual refresh

#### Scenario: Reconcile after reconnect

- **WHEN** the real-time stream reconnects after a drop
- **THEN** the console reconciles the list and counts from the API

### Requirement: Session-scoped inbox access

The console SHALL serve the inbox with the signed-in session's credentials only and MUST NOT expose the session token to the browser.

#### Scenario: Inbox uses the session token

- **WHEN** the console loads or mutates the inbox or its event stream
- **THEN** it uses the signed-in user's token server-side only, and no token appears in the page or client-visible responses
