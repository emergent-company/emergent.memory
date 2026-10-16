## ADDED Requirements

### Requirement: Share session creation is atomic under the active-session cap

Creating an end-user share session SHALL NOT leave an orphaned backing
`kb.sessions` row behind when the per-(link, end user) active-session cap is
reached. The cap check and the creation of both the backing `kb.sessions` row and
the `kb.agent_share_sessions` join row SHALL happen in one transaction under the
per-(link, end user) advisory lock, with the cap count taken before any insert
and the rows created only when the count is under the cap. When the cap is
reached, no row SHALL be inserted and the request SHALL be denied with 429
`share_session_limit`. The atomic cap guarantee (advisory lock + count) SHALL be
preserved: two concurrent first-session requests can never both create a session
past the cap.

#### Scenario: Cap hit leaves no orphaned session row

- **WHEN** an end user at the active-session cap opens another session
- **THEN** the request is denied with 429 `share_session_limit` and no `kb.sessions` row (and no `kb.agent_share_sessions` row) is inserted

#### Scenario: Under the cap both rows are created

- **WHEN** an end user under the cap opens a session
- **THEN** a `kb.sessions` row and its linked `kb.agent_share_sessions` join row are created in the same transaction
