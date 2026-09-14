## Purpose

Lets a signed-in user view, triage, and dismiss Emergent Memory notifications, with real-time updates pushed over Server-Sent Events.

## ADDED Requirements

### Requirement: List notifications

The gateway SHALL list the signed-in user's notifications, most recent first, and SHALL support filtering by tab (`important`, `other`, `snoozed`, `cleared`), unread-only, category, and free-text search over title and message.

#### Scenario: List all notifications

- **WHEN** a signed-in user opens the notifications inbox
- **THEN** the gateway shows the user's notifications ordered by recency, each with its title, message, severity, importance, category, related resource, action link, and read/dismissed state

#### Scenario: Filter by tab

- **WHEN** a user selects a tab such as `important` or `snoozed`
- **THEN** the gateway lists only the notifications belonging to that tab

#### Scenario: No notifications

- **WHEN** a signed-in user has no notifications for the selected tab
- **THEN** the gateway shows an empty state, not an error

### Requirement: Report unread counts

The gateway SHALL report unread notification counts so the UI can render a badge, and SHALL keep the badge current as notifications are read or dismissed.

#### Scenario: Badge counts

- **WHEN** a signed-in user loads the console
- **THEN** the gateway returns unread counts (total unread, important, pending questions) for the badge

### Requirement: Mark notifications read

The gateway SHALL let a user mark a single notification read or unread, and SHALL let a user mark all notifications read at once.

#### Scenario: Mark single read

- **WHEN** a user marks one notification read
- **THEN** the gateway marks it read in Memory and its unread count decreases

#### Scenario: Mark all read

- **WHEN** a user marks all notifications read
- **THEN** the gateway marks every current notification read and the unread count drops to zero

### Requirement: Dismiss and restore notifications

The gateway SHALL let a user dismiss a notification, moving it to the cleared tab, and SHALL let a user restore a cleared notification.

#### Scenario: Dismiss notification

- **WHEN** a user dismisses a notification
- **THEN** the notification disappears from the default view and appears in the cleared tab

#### Scenario: Restore cleared notification

- **WHEN** a user restores a cleared notification
- **THEN** the notification returns to the default view

### Requirement: Resolve actionable notifications

The gateway SHALL let a user resolve an actionable notification (accept or reject), such as a merge suggestion, after which it no longer counts toward pending limits.

#### Scenario: Resolve actionable notification

- **WHEN** a user resolves an actionable notification
- **THEN** the gateway forwards the resolve to Memory and the notification stops counting toward pending limits

### Requirement: Real-time notification updates

The gateway SHALL push notification changes to the browser in real time over a server-sent event stream, so new, updated, and read notifications appear without a manual refresh, and SHALL reconnect automatically if the stream drops.

#### Scenario: New notification arrives

- **WHEN** Memory emits a notification event for the active project
- **THEN** the gateway relays it to the open browser stream and the inbox updates in place

#### Scenario: Stream reconnects

- **WHEN** the event stream connection drops
- **THEN** the browser reconnects and resumes receiving events without user action

### Requirement: Session-scoped access

The gateway SHALL serve notifications for the signed-in session only, using the session's Memory token, and MUST NOT expose the token to the browser.

#### Scenario: Notification calls use the session token

- **WHEN** a signed-in user reads or mutates notifications
- **THEN** the gateway calls Memory with the user's bearer token, never a shared server token

#### Scenario: Token never exposed

- **WHEN** the gateway serves the notification list or event stream to the browser
- **THEN** no Memory token appears in the response, HTML, or client-visible headers
