## ADDED Requirements

### Requirement: Stream rich chat events over LiveKit

The bridge worker SHALL stream rich chat events to the client over the LiveKit text stream so a text turn can render more than a final transcript: tool-call start/end (with arguments and result), and reasoning (thinking) deltas. The client SHALL render each event it understands and ignore events it does not.

#### Scenario: Tool call event streamed

- **WHEN** the agent invokes a tool during a text turn
- **THEN** the worker streams a tool-call event naming the tool to the client, and the client surfaces it in the chat

#### Scenario: Tool result event streamed

- **WHEN** a tool call completes
- **THEN** the worker streams the tool result (or error) to the client, and the client updates the tool chip

#### Scenario: Thinking event streamed

- **WHEN** the agent emits reasoning during a text turn
- **THEN** the worker streams the reasoning deltas to the client, and the client renders them in a thinking block

#### Scenario: Unknown event ignored

- **WHEN** the worker streams an event the client does not recognize
- **THEN** the client ignores it without crashing or corrupting the conversation

### Requirement: Stream A2UI surfaces and forward surface actions

The bridge worker SHALL forward the server's A2UI `ui` event (catalog-validated structured cards) to the client over LiveKit, preserving the `surfaceId` and its message list. The worker SHALL accept a client surface action over LiveKit and return it to the agent as a new turn in the existing conversation context, distinct from answering an `ask_user` question.

#### Scenario: Surface streamed

- **WHEN** the agent emits a validated A2UI surface during a text turn
- **THEN** the worker forwards the `surfaceId` and its messages to the client, and the client renders the cards

#### Scenario: Surface action returns

- **WHEN** the client submits an action on a surface over LiveKit
- **THEN** the worker starts a new turn carrying the surface id and action, and the agent's reply streams back as usual

### Requirement: Stream approval and question events

The bridge worker SHALL stream approval requests and agent questions to the client over LiveKit, and SHALL accept the client's decision (approve/reject/answer) sent back over LiveKit, forwarding it so the paused run resumes.

#### Scenario: Approval request streamed

- **WHEN** the agent attempts a tool that requires approval during a text turn
- **THEN** the worker streams an approval request to the client, and the client renders an approval card

#### Scenario: Client decision forwarded

- **WHEN** the client sends an approval or question decision over LiveKit
- **THEN** the worker forwards that decision to the agent and the run resumes

#### Scenario: Question streamed

- **WHEN** the agent asks the user a question during a text turn
- **THEN** the worker streams the question to the client, and the client renders an interactive question card
