## ADDED Requirements

### Requirement: Pending question links to its session

The human-input surface (`/settings/approvals`) SHALL link each pending question to the chat
conversation (session) that produced it, so an operator can jump from a question to the conversation
waiting on its answer. A question whose run has no chat conversation SHALL omit the link. The link
SHALL target `/chat?c=<conversationId>`.

#### Scenario: Question links to its conversation

- **WHEN** a pending question's run belongs to a chat conversation
- **THEN** the human-input surface shows a session link to that conversation

#### Scenario: Non-chat question omits the link

- **WHEN** a pending question's run has no chat conversation (for example a scheduled run)
- **THEN** the question renders without a session link

#### Scenario: The conversation is resolvable from the question API

- **WHEN** the question list for a project is fetched
- **THEN** each question carries the id of its originating chat conversation, or no id when its run has none
