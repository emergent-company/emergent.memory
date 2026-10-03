# chat-conversation-lifecycle Specification

## Purpose
Defines the lifecycle contract for chat sessions (conversations): how a session is archived and unarchived without being deleted, how it is permanently deleted, how archived sessions are excluded from the default session list and surfaced through an explicit include-archived filter, and the session-rail filter option and per-row action menu that expose hide, restore, and removal to users.

## Requirements

### Requirement: Archive state is persisted and non-destructive

A chat conversation SHALL carry a persisted archive state, set when the conversation is archived and cleared when it is unarchived. Archiving SHALL NOT delete or mutate the conversation's messages, history, or backing agent session; an archived conversation remains fully retrievable by its id.

#### Scenario: Archive preserves content

- **WHEN** a conversation with messages is archived
- **THEN** its archive state is set, its messages and history remain intact, and loading it by id still returns the conversation with its messages

#### Scenario: Unarchive clears the state

- **WHEN** an archived conversation is unarchived
- **THEN** its archive state is cleared and it is returned by default session lists again

### Requirement: Archive operation

The system SHALL expose an operation that archives a chat conversation on behalf of an authenticated caller. The operation SHALL succeed for a conversation the caller can access under the owner-or-shared predicate, SHALL fail closed to not-found for a conversation the caller cannot access, and SHALL be idempotent when the conversation is already archived.

#### Scenario: Owner archives their conversation

- **WHEN** the owner of a conversation archives it
- **THEN** the operation succeeds and the conversation's archive state is set

#### Scenario: Archive is idempotent

- **WHEN** an already-archived conversation is archived again
- **THEN** the operation succeeds and the archive state remains set

#### Scenario: Foreign conversation fails closed

- **WHEN** a caller archives another user's private conversation
- **THEN** the operation returns not-found, indistinguishable from an unknown id

### Requirement: Unarchive operation

The system SHALL expose an operation that unarchives a chat conversation, applying the same access predicate, fail-closed behaviour, and idempotency as the archive operation.

#### Scenario: Owner unarchives their conversation

- **WHEN** the owner of an archived conversation unarchives it
- **THEN** the operation succeeds and the conversation no longer counts as archived

#### Scenario: Unarchive is idempotent

- **WHEN** a conversation that is not archived is unarchived
- **THEN** the operation succeeds and the conversation remains not archived

### Requirement: Session list excludes archived by default

The chat conversation list SHALL exclude archived conversations by default. It SHALL accept an explicit include-archived option that, when set, includes archived conversations in the result. Ordering SHALL remain most-recently-updated first in both cases.

#### Scenario: Default list hides archived

- **GIVEN** a mix of archived and active conversations
- **WHEN** a client lists conversations without the include-archived option
- **THEN** only active conversations are returned

#### Scenario: Include-archived list shows both

- **GIVEN** a mix of archived and active conversations
- **WHEN** a client lists conversations with the include-archived option set
- **THEN** both archived and active conversations are returned, ordered by recency

### Requirement: Session rail exposes archive filter and per-row actions

The web chat session rail SHALL provide a filter option that includes archived sessions alongside its existing filters, and SHALL hide archived sessions when that option is not enabled. Each session row SHALL expose an action menu offering Archive for an active session, Unarchive for an archived session, and Delete for any session. Invoking archive or unarchive SHALL update the session's archive state and refresh the rail list. Invoking Delete SHALL require an explicit confirmation before the deletion is performed.

#### Scenario: Archived session hidden by default

- **WHEN** the chat rail renders with the include-archived filter off
- **THEN** archived sessions do not appear in the list

#### Scenario: Include-archived filter reveals archived sessions

- **WHEN** the user enables the include-archived filter
- **THEN** archived sessions appear in the list and are visually distinguishable from active sessions

#### Scenario: Row menu archives an active session

- **WHEN** the user chooses Archive from an active session row's action menu
- **THEN** the session is archived and disappears from the rail while the include-archived filter is off

#### Scenario: Row menu unarchives an archived session

- **WHEN** the user chooses Unarchive from an archived session row's action menu
- **THEN** the session is unarchived and appears in the rail as an active session

#### Scenario: Row menu requires confirmation before deleting

- **WHEN** the user chooses Delete from a session row's action menu
- **THEN** a confirmation is required, and the session is deleted only if the user confirms

#### Scenario: Cancelling a delete leaves the session

- **WHEN** the user dismisses the delete confirmation
- **THEN** the session is not deleted and remains in the rail

### Requirement: Delete operation

The system SHALL expose an operation that permanently deletes a chat conversation on behalf of an authenticated caller, requiring the same access predicate as archive and unarchive (owner-or-shared within the caller's project) and failing closed to not-found for a conversation the caller cannot access. The operation SHALL be reachable by an ordinary member (not gated to administrators). Deletion SHALL be permanent and irreversible.

#### Scenario: Owner deletes their conversation

- **WHEN** the owner of a conversation deletes it
- **THEN** the operation succeeds and the conversation no longer exists

#### Scenario: Member with access can delete

- **GIVEN** a non-private conversation in the caller's project owned by another user
- **WHEN** a non-owner project member deletes it
- **THEN** the operation succeeds, matching the owner-or-shared predicate

#### Scenario: Foreign conversation fails closed

- **WHEN** a caller deletes another user's private conversation
- **THEN** the operation returns not-found, indistinguishable from an unknown id

#### Scenario: Deleted conversation is gone

- **WHEN** a client requests a deleted conversation by id
- **THEN** the request returns not-found

### Requirement: Delete removes the conversation's messages

Deleting a conversation SHALL remove its messages along with it; the messages SHALL NOT be left orphaned.

#### Scenario: Messages removed with the conversation

- **GIVEN** a conversation with messages
- **WHEN** the conversation is deleted
- **THEN** its messages are no longer retrievable through any conversation read

### Requirement: Deleting the open session exits it

Deleting the conversation currently open in the chat workspace SHALL leave the workspace in a usable state with no reference to the deleted conversation, rather than rendering the deleted conversation's transcript or an error page.

#### Scenario: Open conversation deleted exits the workspace

- **GIVEN** a conversation is open in the chat workspace
- **WHEN** it is deleted
- **THEN** the workspace no longer shows the deleted conversation and is ready to start a new session

### Requirement: Archiving the open session leaves it open

Archiving the conversation currently open in the chat workspace SHALL NOT close, navigate away from, or blank it; it only removes the conversation from the default session list.

#### Scenario: Open conversation archived stays open

- **GIVEN** a conversation is open in the chat workspace
- **WHEN** it is archived
- **THEN** the conversation remains open with its messages visible and is simply absent from the default session rail list

### Requirement: Rail lifecycle actions are optimistic

Archive and unarchive SHALL apply their rail-list effect optimistically: choosing Archive SHALL remove the session from the rail (while the include-archived filter is off) before the request settles, and choosing Unarchive SHALL clear the archived presentation immediately. If the request fails, the client SHALL restore the row to its pre-request state and surface an error. The server-rendered session list remains the source of truth: once the request settles, the rail SHALL be re-fetched and reconciled with the server's archive state.

#### Scenario: Archive hides the row before the request settles

- **WHEN** the user chooses Archive on an active session while the include-archived filter is off
- **THEN** the row disappears from the rail immediately, without waiting for the archive request to complete

#### Scenario: Failed archive restores the row

- **WHEN** an optimistically hidden session's archive request fails
- **THEN** the row reappears in the rail and an error is surfaced to the user

#### Scenario: Rail reconciles with the server after settling

- **WHEN** an optimistic archive or unarchive request settles
- **THEN** the rail is re-fetched from the server and reflects the server's authoritative archive state

#### Scenario: Filtering does not reveal a pending archive

- **WHEN** the agent or origin filter changes while an optimistic archive is in flight
- **THEN** the pending row stays hidden regardless of the filter, and a failed request restores it only if the active filter would show it

#### Scenario: A rail refresh does not reveal a pending archive

- **WHEN** the session rail is rebuilt from the server (an SSE live-update, a finished turn, another session's delete, or the include-archived toggle) while an optimistic archive is in flight
- **THEN** the pending session stays hidden while the include-archived filter is off, and reappears only if its archive request fails

### Requirement: Bulk session selection

The web chat session rail SHALL provide a selection mode in which multiple conversation sessions can be checked and acted on together. Selection mode SHALL expose a checkbox per conversation row, a select-all control, a live count of the selected sessions, and a bulk archive action that applies the same lifecycle semantics as the per-row Archive action to every selected session. The controls SHALL be keyboard-operable and expose their state to assistive technology. Leaving selection mode or completing a bulk action SHALL clear the selection; the selection SHALL survive a rail refresh.

#### Scenario: Selection mode reveals row checkboxes

- **WHEN** the user turns on selection mode
- **THEN** each conversation row shows a labelled checkbox and the bulk action controls become available

#### Scenario: Select-all checks the selectable rows

- **WHEN** the user activates select-all
- **THEN** every currently selectable (visible, not already archived) conversation row is checked and the count reflects the full set

#### Scenario: Bulk archive applies to the checked set

- **WHEN** the user triggers the bulk archive action with a subset of rows checked
- **THEN** exactly the checked sessions are archived through the per-session archive operation, and the selection is cleared once the action completes

#### Scenario: Selection controls are accessible

- **WHEN** the selection controls are rendered
- **THEN** the per-row checkboxes and select-all control carry accessible names, the selected count is announced, and the controls can be operated by keyboard

#### Scenario: Selection survives a rail refresh

- **WHEN** the rail is re-fetched while selection mode is on
- **THEN** the selected sessions remain checked and the count is unchanged

#### Scenario: Selection state survives a shell swap

- **WHEN** the chat shell (#chat-root) is swapped while selection mode is active
- **THEN** the re-rendered rail shows the checkboxes, footer controls, and toggle state matching the still-active selection mode
