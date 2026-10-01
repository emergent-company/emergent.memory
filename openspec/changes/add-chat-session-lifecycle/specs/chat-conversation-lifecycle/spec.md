## Purpose

Defines the lifecycle contract for chat sessions (conversations): how a session is archived and unarchived without being deleted, how it is permanently deleted, how archived sessions are excluded from the default session list and surfaced through an explicit include-archived filter, and the session-rail filter option and per-row action menu that expose hide, restore, and removal to users.

## ADDED Requirements

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
