## ADDED Requirements

### Requirement: Secret lifecycle flows have behavioral e2e coverage
Every Web UI flow that mints, mutates, or revokes a credential SHALL have a Playwright spec in the `mutations` project that drives the real UI and asserts the resulting state change.

#### Scenario: Project API token lifecycle
- **WHEN** a spec creates a token via `GET/POST /settings/tokens/new`, edits its scopes via `POST /settings/tokens/:tokenId/scopes`, regenerates it via `POST /settings/tokens/:tokenId/regenerate`, and revokes it via `POST /settings/tokens/:tokenId/revoke`
- **THEN** each step is asserted in the UI (one-time secret revealed once, updated scope set shown, prior secret invalidated, no live token remains after revoke — the revoked audit row is retained)

#### Scenario: Profile API token lifecycle
- **WHEN** a spec drives `/profile/tokens/new`, `/profile/tokens/:tokenId/edit`, `/:tokenId/scopes`, `/:tokenId/regenerate`, `/:tokenId/revoke`
- **THEN** the profile-scoped token family is covered with the same assertions as the project-scoped family

#### Scenario: MCP share lifecycle
- **WHEN** a spec drives `/settings/mcp-servers/shares`, `/shares/new`, `/:id/edit`, `/:id/update`, `/:id/rotate`, `/:id/revoke`
- **THEN** share creation shows a one-time token, rotation invalidates the previous token, and revocation removes the share from the list

#### Scenario: Per-agent MCP share lifecycle
- **WHEN** a spec creates, rotates, and revokes a share from the agent detail surface
- **THEN** the agent's share list reflects each transition

### Requirement: Authorization-changing flows have behavioral e2e coverage
Flows that change who can access a tenant or the graph SHALL have specs asserting both the permitted path and the rejected path.

#### Scenario: Member role change
- **WHEN** an admin changes another member's role via `POST /members/:userId/role`
- **THEN** the new role is reflected in the members list

#### Scenario: Self and equal-role changes are rejected
- **WHEN** a member attempts to change their own role, or to set a role equal to their current role
- **THEN** the request is rejected and the UI reports the failure without a role change

#### Scenario: Member removal
- **WHEN** an admin removes a member via `POST /members/:userId/remove`
- **THEN** the member disappears from the list

#### Scenario: Invite lifecycle
- **WHEN** a spec exercises `POST /invites/:id/revoke`, `POST /invites/:id/decline`, and `POST /invites/:id/accept`
- **THEN** revocation and decline remove the invite from the pending list, and acceptance grants access

#### Scenario: Member detail page
- **WHEN** `GET /members/:userId` is opened
- **THEN** the identity and role of that member are rendered

### Requirement: Destructive operations have behavioral e2e coverage
Every route that mutates graph, schema, backup, or project state SHALL have a spec that drives the operation and asserts its resulting persisted state. The spec SHALL assert the route's deterministic contract; where the outcome depends on a live LLM or a server defect makes it non-deterministic, the spec SHALL assert the route contract and record the deferred outcome with a tracking reference rather than fabricating an outcome assertion.

#### Scenario: Object merge session
- **WHEN** a spec requests `GET /objects/:id/merge?with=<dst>`
- **THEN** the route 303-redirects to `/chat` with a resolved agent and a prompt naming both objects by key and id, and the graph is left untouched (both objects and the edge between them still exist)
- **AND** a missing or unknown target object redirects back to the source object
- **AND** the merged-away / relationships-carried outcome is deferred to the env-gated live-LLM `scenarios` suite: `uiObjectMerge` only starts an agent chat session, and the fusion itself is non-deterministic LLM work

#### Scenario: Object relationships
- **WHEN** a spec creates an edge via `POST /objects/:id/relationships`
- **THEN** the relationship is visible from both objects

#### Scenario: Blueprint install and unapply
- **WHEN** a spec installs a bundled blueprint via the gallery form (`POST /blueprints/install`) then unapplies it via `POST /blueprints/:id/unapply`
- **THEN** the blueprint's object types appear in the compiled view and the object-create form, and are gone from both after unapply

#### Scenario: Migration apply and rollback
- **WHEN** a spec force-executes `POST /blueprints/migrate` then drives `POST /blueprints/migrate/rollback`
- **THEN** apply is asserted by the undeclared property disappearing from the stored object, and rollback is asserted by its 303 and `migrateMsg` response contract
- **AND** the rollback data-restoration assertion is deferred while the endpoint restores 0 objects because `Repository.List` omits `migration_archive` from its projection (GitHub issue #513); the spec holds a runtime `test.skip` that auto-reverts to a hard assertion once that is fixed

#### Scenario: Backup create, download and delete
- **WHEN** a spec creates a backup, waits for it to reach `ready`, opens its detail, requests `GET /backups/:id/download`, and deletes it via `POST /backups/:id/delete`
- **THEN** the Download affordance is asserted, the download route's 302 `Location` is asserted without following it off-origin to the pre-signed URL, and the backup is absent from the list afterwards
- **AND** a backup that does not reach `ready`, or a feature-gated `Backups unavailable` state, skips with a stated reason rather than failing

#### Scenario: Project restore
- **WHEN** a spec restores a project via `POST /projects/restore`
- **THEN** the project returns to the active project list

#### Scenario: Document deletion
- **WHEN** a spec deletes via `POST /documents/:id/delete`
- **THEN** the document is removed from the list and its chunk view is no longer reachable

### Requirement: Agent and skill management CRUD has behavioral e2e coverage
Agent and skill management SHALL be covered beyond creation, including update and removal.

#### Scenario: Agent update
- **WHEN** a spec submits `POST /agents/:id/update`
- **THEN** the change is persisted and reflected on the agent detail page

#### Scenario: Agent activation and removal
- **WHEN** a spec deactivates and reactivates an agent, then deletes it
- **THEN** the status transitions are visible and the deleted agent is absent from the list

#### Scenario: Agent memories and sandbox
- **WHEN** `GET /agents/:id/memories` is opened and `POST /agents/:id/sandbox/update` is submitted
- **THEN** memories render and the sandbox settings persist

#### Scenario: Skill update and delete
- **WHEN** a spec submits `POST /skills/:id/update` then `POST /skills/:id/delete`
- **THEN** the update is visible on the skill detail and the skill is absent after deletion

#### Scenario: Org rename and tool settings
- **WHEN** a spec submits `POST /orgs/:id/rename`, opens `/orgs/:id/settings/general`, and sets then deletes a tool setting via `POST /orgs/:id/tool-settings/:toolName`
- **THEN** the new org name and the tool-setting changes are persisted

### Requirement: Routes currently asserted only to render gain interaction assertions
Routes whose only existing coverage is a page-load/title assertion SHALL gain at least one interaction assertion against their primary mutating control.

#### Scenario: Project settings inline autosave
- **WHEN** a setting is changed on `/settings` (or a section page) so the `hx-post` with `hx-trigger="change delay:400ms"` and `hx-swap="none"` fires
- **THEN** the change is confirmed in the UI and survives a reload

#### Scenario: Approvals
- **WHEN** a spec responds or cancels via `POST /settings/approvals/:questionId/respond|/cancel`
- **THEN** the pending approval is cleared

#### Scenario: Device revocation
- **WHEN** a spec submits `POST /settings/devices/:key/revoke`
- **THEN** the device is removed from the list

#### Scenario: Voice settings
- **WHEN** a spec submits `POST /settings/voice`, `/settings/voice/:key`, or `/settings/voice/group/:group`
- **THEN** the value persists after reload

#### Scenario: Provider diagnostics
- **WHEN** a spec submits `POST /settings/providers/test`, `/settings/providers/check-url`, `/:provider/test`, or `/:provider/remove`
- **THEN** the result (success or explicit failure) is surfaced and removal clears the provider from the panel

#### Scenario: Project overrides
- **WHEN** a spec creates an override via `POST /settings/overrides` and deletes one via `POST /settings/overrides/:agentName/delete`
- **THEN** the override list reflects both changes

#### Scenario: Editor and remember fields
- **WHEN** a spec submits `POST /settings/editor` or `POST /settings/remember/:field`
- **THEN** the value persists after reload

#### Scenario: MCP node registry
- **WHEN** `/settings/mcp-nodes` is opened and `POST /settings/mcp-nodes/remove` is submitted
- **THEN** the node is removed from the registry list

### Requirement: Schedule and run lifecycle has behavioral e2e coverage
Schedules SHALL be covered beyond creation, and run detail SHALL be reachable and asserted.

#### Scenario: Schedule lifecycle
- **WHEN** a spec opens `/schedules/:id`, submits `/:id/update`, triggers `/:id/trigger`, toggles `/:id/toggle`, and deletes `/:id/delete`
- **THEN** each transition is asserted in the UI

#### Scenario: Run detail
- **WHEN** a run is triggered and `GET /runs/:runId` is opened
- **THEN** the run detail renders its status and steps

### Requirement: Observability surfaces have behavioral e2e coverage
The usage dashboard and session inspection pages SHALL assert rendered data, not only page load.

#### Scenario: Usage statistics and charts
- **WHEN** `/usage` is opened
- **THEN** the `stat-total-tokens`, `stat-estimated-cost`, `stat-sessions` and `stat-month-spend` values render and the `usage-token-chart` and `usage-session-chart` draw from the `#usage-timeseries` payload

#### Scenario: Session detail
- **WHEN** `GET /sessions/:id` is opened
- **THEN** the timeline, run grouping and tool-call details render

### Requirement: Chat and streaming surfaces have behavioral e2e coverage
The chat client and its assistant sidepanel SHALL have specs covering streaming, conversation events, and ask-user question cards.

#### Scenario: Streamed turn
- **WHEN** a message is sent through the chat composer
- **THEN** the reply renders incrementally and the turn settles, with the transcript DOM asserted

#### Scenario: Conversation live events
- **WHEN** a conversation is open and its events endpoint emits
- **THEN** the conversation rail refreshes accordingly via `GET /api/conversations/:id/events` and `/partial/chat-rail`

#### Scenario: Ask-user question cards
- **WHEN** a question card is answered via `POST /api/chat/questions/:questionId/respond` or dismissed via `POST /api/chat/questions/:questionId/cancel`
- **THEN** the card resolves and the conversation continues

#### Scenario: Assistant sidepanel
- **WHEN** the sidepanel is opened and a turn is sent via `POST /api/chat`
- **THEN** the reply renders in the sidepanel without navigating away from the current page

### Requirement: Render-only coverage is eliminated for mutating routes
After this change, no mutating route registered in `apps/web-ui/gateway/main.go` SHALL be covered by page-load assertions alone.

#### Scenario: Coverage audit
- **WHEN** the route list from `gateway/main.go` is compared against the spec inventory in `apps/web-ui/tests/e2e`
- **THEN** every mutating method/path pair maps to at least one spec that performs the mutation and asserts its effect

#### Scenario: Read-only routes remain load-assertable
- **WHEN** a route has no mutating behavior
- **THEN** a page-load assertion remains acceptable coverage for it
