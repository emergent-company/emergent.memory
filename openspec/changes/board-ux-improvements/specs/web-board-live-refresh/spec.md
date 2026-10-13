## ADDED Requirements

### Requirement: The board reflects asynchronous work-item changes without a reload

The board page SHALL subscribe to the session's project event stream and refresh
the board's swap region when a work-item (`graph_object`) or agent-run
(`agent_run`) change frame arrives, so a status transition performed by an agent
after the triggering request completes is visible without a manual reload.
Refreshes SHALL be coalesced so a burst of frames issues a single board fetch.

#### Scenario: An agent advances a task after the operator acts

- **WHEN** the operator requests changes on a card and the agent later moves the work item to `in_progress`
- **THEN** the board re-fetches and the card is shown in the new lane without a page reload

#### Scenario: A burst of frames coalesces

- **WHEN** several change frames for the project arrive within one debounce window
- **THEN** exactly one board refresh is issued

#### Scenario: Unrelated frames do not refresh

- **WHEN** a frame arrives whose entity is neither a work item nor an agent run
- **THEN** the board is not refreshed

#### Scenario: No subscription without a board

- **WHEN** the page is not the board page
- **THEN** no project event stream is opened

### Requirement: Refresh does not disrupt an in-flight interaction

The board refresh SHALL be suppressed while the tab is hidden and while a card
drag is in progress, and the event stream SHALL be closed when the page is left,
so a swap never breaks an interaction and no connection outlives the page.

#### Scenario: Drag in progress

- **WHEN** a change frame arrives while a card is being dragged
- **THEN** no board swap happens, and the drag completes normally

#### Scenario: Hidden tab

- **WHEN** a change frame arrives while the document is hidden
- **THEN** no board swap happens

#### Scenario: Page unload

- **WHEN** the board page is unloaded
- **THEN** the project event stream is closed
