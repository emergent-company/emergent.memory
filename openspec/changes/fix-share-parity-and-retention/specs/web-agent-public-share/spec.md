## MODIFIED Requirements

### Requirement: Session list and approval surface
The public page SHALL render the end user's own sessions (respecting `show_session_list` and the `filter` parameter) and SHALL render an approve/deny surface for pending approvals and `ask_user` questions. The surface SHALL support approve/deny only; no richer approval UI is in scope. The session rail SHALL be the server-rendered partial (including each row's archive/unarchive/delete menu); when that partial cannot be loaded the page SHALL render an explicit failure state with a retry action instead of a client-rendered list that omits the per-row actions. Question and approval cards SHALL render read-only — no option rows and no decision controls — when the bound link has `allow_end_user_approvals` false, matching the server's 403, while keeping the question body and tool call visible.

#### Scenario: Session list shows only own sessions
- **WHEN** an end user loads the public page and `show_session_list` is true
- **THEN** the page lists only that end user's own sessions for the link, filtered by `filter` (default active)

#### Scenario: Rail always offers the per-row session actions
- **WHEN** an end user's session rail is displayed
- **THEN** every row carries the server-rendered archive/unarchive/delete menu, and a failed partial load shows an explicit error with a retry that re-requests the server-rendered rail rather than substituting a list without those actions

#### Scenario: Approve/deny surface
- **WHEN** a pending approval or question exists for the end user's session
- **THEN** the page renders approve and deny actions; no richer approval controls are offered

#### Scenario: Decision controls are hidden when approvals are disabled
- **WHEN** the bound link has `allow_end_user_approvals` false and a question, tool approval, or decision-bearing structured-UI (`ui`) surface arrives
- **THEN** the card renders without decision controls (no option rows, Submit, Approve, Reject, or Cancel) and states that approvals are disabled, so the client never offers an action the server would reject with 403

## ADDED Requirements

### Requirement: Share BFF exposes only consumed endpoints
The gateway's public share surface SHALL expose only the endpoints the share page consumes. A route that no client and no documented flow consumes SHALL be removed rather than kept as an unversioned internal API, so the share surface cannot drift into an undocumented public interface.

#### Scenario: Unconsumed share route is absent
- **WHEN** the share page's client scripts and the share capability specs are enumerated
- **THEN** every `/share/api/*` route has at least one consumer, and routes with none (such as the former `GET /share/api/questions` and `GET /share/api/sessions`) are not registered
