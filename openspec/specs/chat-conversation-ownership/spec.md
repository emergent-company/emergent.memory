# chat-conversation-ownership

## Purpose

Defines the ownership and access model for chat conversations (`kb.chat_conversations` / `kb.chat_messages`): the owner-or-shared predicate every by-id and canonical-id accessor applies, the 404-on-foreign convention, and the project-shared semantics of canonical (object-refinement) conversations.

## Requirements

### Requirement: Owner-or-shared access predicate

A conversation SHALL be accessible to a caller only when the caller owns it (`owner_user_id` equals the caller's user id) or it is non-private (`is_private = false`), and it belongs to the caller's addressed project. Every by-id and canonical-id accessor (get, get-with-messages, history, update, delete, add-message, set-agent-definition) SHALL apply this predicate at the data-access layer (the query), not merely as a post-load check.

#### Scenario: Owner reads their private conversation

- **WHEN** a caller loads a private conversation they own
- **THEN** the conversation is returned

#### Scenario: Foreign member cannot read a private conversation

- **GIVEN** a private conversation owned by another user in the same project
- **WHEN** a non-owner project member loads it by id
- **THEN** the accessor returns not-found, not the conversation

#### Scenario: Non-private conversation is project-shared

- **GIVEN** a non-private (`is_private = false`) conversation
- **WHEN** any member of its project loads it by id
- **THEN** the conversation is returned

### Requirement: Foreign id fails closed to not-found

A conversation id (or canonical id) the caller cannot access SHALL be indistinguishable from one that does not exist: the by-id endpoints SHALL return 404 rather than 403, so existence is not leaked.

#### Scenario: Unknown conversation

- **WHEN** a caller requests a conversation id that does not exist
- **THEN** the endpoint returns 404

#### Scenario: Foreign conversation is indistinguishable from unknown

- **WHEN** a caller requests another user's private conversation id
- **THEN** the endpoint returns 404, identical to an unknown id

### Requirement: Canonical conversations are project-shared

A conversation created with a `canonicalId` (object-refinement chat) SHALL be non-private (`is_private = false`) so every project member resolves the same conversation for the same canonical id. The `canonical_id` column is globally unique, so a private per-user refinement conversation is impossible.

#### Scenario: Get-or-create by canonical id converges

- **GIVEN** a canonical (refinement) conversation exists for a canonical id
- **WHEN** a second project member creates a conversation with the same canonical id
- **THEN** the existing shared conversation is returned (dedup), not a new row

#### Scenario: Foreign private canonical id is refused

- **GIVEN** a private conversation with a canonical id, owned by another user
- **WHEN** a non-owner requests a conversation with the same canonical id
- **THEN** the request is refused with 409 conflict, and the owner's conversation is never returned

### Requirement: Message history respects ownership

Message reads SHALL never return messages from a conversation the caller cannot access. The conversation-history read SHALL apply the owner-or-shared predicate or be reachable only through a guarded conversation load.

#### Scenario: Foreign caller reads no history

- **GIVEN** a private conversation owned by another user
- **WHEN** a non-owner reads its message history
- **THEN** no messages are returned

### Requirement: Session todos inherit the conversation's ownership

Session todos (`kb.session_todos`, keyed by `session_id`) SHALL inherit the owner-or-shared predicate of the conversation the session backs. The session id SHALL be resolved to its ACP session (`kb.acp_sessions`) for the project scope and to its linked conversation (`kb.chat_conversations.acp_session_id`) for the owner/private scope; a todo's list/create/update/delete SHALL succeed only when the session is in the caller's project AND the linked conversation is owned by the caller or non-private. A session with no linked conversation SHALL be project-scoped. A foreign or unknown session id SHALL fail closed to the domain's 404 convention, indistinguishable from a non-existent session.

#### Scenario: Foreign member cannot read or write a private session's todos

- **GIVEN** a private conversation owned by another user in the same project, whose ACP session has todos
- **WHEN** a non-owner project member lists, creates, updates, or deletes those session todos by session id
- **THEN** every accessor returns 404

#### Scenario: Owner still manages their session todos

- **WHEN** the conversation owner lists, creates, updates, or deletes their session todos
- **THEN** every accessor succeeds

#### Scenario: Non-private session todos are project-shared

- **GIVEN** a non-private (`is_private = false`) conversation whose session has todos
- **WHEN** any member of its project lists those session todos
- **THEN** the todos are returned
