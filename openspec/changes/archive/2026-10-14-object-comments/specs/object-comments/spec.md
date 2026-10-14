# object-comments Specification

## ADDED Requirements

### Requirement: Work items carry append-only durable comments

A work item SHALL carry append-only durable comments, stored independently of its
rework feedback rounds. Each comment SHALL record its project, the work item's
canonical id, a non-empty `body`, an optional `author` (the human user's id or the
agent's display name), an optional `actor_type` (`user` or `agent`), an optional
`run_id` (the work run that produced an agent comment), and a `created_at`
timestamp. A comment SHALL NOT change the item's status and SHALL NOT be edited or
deleted. Comments SHALL be listed in ascending `created_at` order.

#### Scenario: Human comment is attributed and does not transition the item

- **WHEN** a human records a comment on a work item
- **THEN** the comment is stored with `actor_type='user'` and the authoring user's id, and the item's status is unchanged

#### Scenario: Agent comment is attributed to the run

- **WHEN** a work agent records a comment through its comment tool
- **THEN** the comment is stored with `actor_type='agent'`, the agent's display name as author, and the producing run's id

#### Scenario: Comments are ordered oldest-first

- **WHEN** multiple comments exist on a work item
- **THEN** they are returned in ascending `created_at` order

#### Scenario: Comments are scoped to their item

- **WHEN** comments exist on one work item
- **THEN** they are not returned for any other item or project

### Requirement: A human can comment on a work item through the work path

An authorized human SHALL record a durable comment on a work item via `POST
/api/projects/:projectId/work-items/:canonicalId/comments` (agents:write). An
empty `body` SHALL be rejected with a 400, and a missing item SHALL be rejected
with a 404. The comment SHALL be attributed to the calling user with
`actor_type='user'` and no `run_id`.

#### Scenario: Comment added

- **WHEN** an authorized human posts a non-empty comment for an existing work item
- **THEN** the comment is stored and the item's work-item HEAD is returned

#### Scenario: Empty comment rejected

- **WHEN** a comment is submitted with an empty body
- **THEN** the request is rejected with a 400 and nothing is stored

#### Scenario: Missing item rejected

- **WHEN** a comment is submitted for a canonical id that does not resolve to a work item
- **THEN** the request is rejected with a 404

### Requirement: A work agent can comment through a per-run tool

A work agent SHALL be able to record a durable comment on the work item it is
working on through a `comment` run tool, built only when the agent's tools
whitelist includes `comment` (consistent with `work_complete`/`work_block`). The
tool SHALL require a non-empty `body`, SHALL store the comment with
`actor_type='agent'`, the agent's display name as author, and the run id, and SHALL
NOT end the run or change the item's status.

#### Scenario: Whitelist gates the comment tool

- **WHEN** an agent's tools whitelist includes `comment`
- **THEN** the comment tool is built for its object-driven run

#### Scenario: Whitelist absence hides the comment tool

- **WHEN** an agent's tools whitelist omits `comment`
- **THEN** no comment tool is built

#### Scenario: Empty comment rejected

- **WHEN** the comment tool is invoked with an empty `body`
- **THEN** the call returns an error result and stores nothing

#### Scenario: Comment does not finalize the run

- **WHEN** the comment tool records a comment
- **THEN** the run terminator is not finalized and the item's status is unchanged

### Requirement: The board surfaces comments in one merged thread

Both board surfaces SHALL render a work item's durable comments and its rework
feedback rounds as one "Activity" thread ordered by `created_at` ascending: the
shared object preview drawer's board slot (`?actions=board` — the card-click
path) and the native drawer dialog (the drag→review path). Entries sharing a
`created_at` second SHALL be ordered deterministically by the entry's own content
(kind, then round, then author, then body), never by which source slice they came
from. Feedback rounds SHALL be marked with their rework round; agent-authored
comments SHALL be marked as agent-authored. An empty thread SHALL render a muted
empty state, and an inline form SHALL let a human add a comment that re-renders
the board. Because both surfaces can be present in one document, each SHALL tag
its thread's elements with a surface-scoped `data-testid` (preview vs drawer).

#### Scenario: Merged thread renders in both surfaces

- **WHEN** a work item has both durable comments and feedback rounds
- **THEN** both the preview's board slot and the native drawer render them as one ascending thread, with feedback rounds marked by rework round and agent comments marked as agent-authored

#### Scenario: Equal-second entries order deterministically

- **WHEN** a comment and a feedback round share a `created_at` second
- **THEN** they render in the deterministic content order rather than by insertion order

#### Scenario: Empty thread renders an empty state

- **WHEN** a work item has no comments and no feedback rounds
- **THEN** each surface renders an empty "Activity" state

#### Scenario: Inline comment posts and re-renders

- **WHEN** a human submits a surface's comment form
- **THEN** the comment is posted to the work-item comment route and the board columns re-render

#### Scenario: Both surfaces coexist with distinct test ids

- **WHEN** the preview board slot and the native drawer are both present in one document
- **THEN** each thread element carries a surface-scoped test id, so a locator resolves to a single surface
