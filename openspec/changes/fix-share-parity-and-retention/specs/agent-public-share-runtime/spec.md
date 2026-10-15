## MODIFIED Requirements

### Requirement: Session ownership uses a join table with per-end-user visibility
Session ownership SHALL be recorded in a join table `kb.agent_share_sessions` linking a share link to an `kb.sessions` row, keyed by `end_user_ref` — not via nullable columns on `kb.sessions`. Each end user SHALL see only their own sessions. The system SHALL enforce a maximum of 5 active sessions per end user and 3 concurrent runs per link, and a maximum message length of 8,000 chars. Deleting a share session SHALL delete the join row and SHALL reclaim the backing session and its runs only when no other share link references that session, so a deleted conversation leaves no stranded session, run, or message history behind.

#### Scenario: Join table records ownership
- **WHEN** an end user starts a session on a share link
- **THEN** a `kb.agent_share_sessions` row is created linking the share link, the ACP session, and the end user's ref, with no ownership column added to `kb.sessions`

#### Scenario: End user sees only their own sessions
- **WHEN** an end user requests their session list
- **THEN** only sessions whose join-table rows belong to their ref (and link) are returned

#### Scenario: Share sessions are hidden from normal listings
- **WHEN** a project member lists the project's ACP/chat sessions
- **THEN** ACP sessions linked from `kb.agent_share_sessions` are excluded, so anonymous public-share sessions never appear in a member's session list

#### Scenario: Archived filter
- **WHEN** an end user lists sessions with `filter=active|all|archived`
- **THEN** the list is filtered accordingly, defaulting to `active`

#### Scenario: Active-session cap enforced
- **WHEN** an end user attempts to open a 6th active session on the same link
- **THEN** the attempt is denied with 429 `share_session_limit`

#### Scenario: Concurrent-run cap enforced
- **WHEN** a link already has 3 concurrent runs and a 4th run is requested
- **THEN** the 4th run is rejected with 429 `share_busy`

#### Scenario: Message length cap enforced
- **WHEN** a message exceeds the link's `max_message_chars`
- **THEN** the stream is rejected before the agent runs

#### Scenario: Deleting the last reference reclaims the session and its runs
- **WHEN** a share session is deleted and no other share link references its backing session
- **THEN** the join row, the session's runs, and the session itself are deleted in one transaction, under a per-conversation lock so two concurrent last-reference deletes cannot both skip the reclaim, leaving no stranded session, run, or message history behind

#### Scenario: Shared backing session is not destroyed
- **WHEN** a share session is deleted while another share link still references the same backing session
- **THEN** only the deleted link's join row is removed, and the backing session and its runs are left intact for the other link

#### Scenario: Delete is transactional
- **WHEN** any step of the share-session delete fails
- **THEN** the whole delete is rolled back, so no join row is removed without its associated cleanup
