## Purpose

Defines the iOS text-chat experience so it matches the web chat: rich markdown rendering, live tool and reasoning activity, interactive approvals and questions, and the composer affordances that make text turns feel complete.

## ADDED Requirements

### Requirement: Render rich markdown

The iOS chat SHALL render the agent reply as full markdown, not inline-only text: fenced code blocks, syntax highlighting within code blocks, tables, inline images from remote URLs, task lists, strikethrough, and tappable links.

#### Scenario: Code block renders with highlighting

- **WHEN** an agent reply contains a fenced code block
- **THEN** the chat shows the code in a monospaced block with language syntax highlighting

#### Scenario: Table renders

- **WHEN** an agent reply contains a markdown table
- **THEN** the chat renders the table with visible rows and columns

#### Scenario: Inline image renders

- **WHEN** an agent reply references a remote image with markdown image syntax
- **THEN** the chat loads and displays that image inline

#### Scenario: Link is tappable

- **WHEN** an agent reply contains a URL link
- **THEN** the user can tap the link to open it

#### Scenario: Malformed markdown degrades gracefully

- **WHEN** an agent reply contains malformed markdown
- **THEN** the chat falls back to rendering the text without crashing

### Requirement: Show a typing indicator

The chat SHALL show a typing indicator while the agent is generating a reply and before the first reply text arrives, and SHALL dismiss it once text streams in.

#### Scenario: Typing indicator while waiting

- **WHEN** the user sends a message and the agent has not yet produced reply text
- **THEN** the chat shows a typing indicator in the agent position

#### Scenario: Typing indicator dismissed

- **WHEN** the first reply text arrives
- **THEN** the typing indicator is replaced by the streaming reply

### Requirement: Stop an in-flight reply

The user SHALL be able to stop a reply while it is being generated, and stopping SHALL interrupt generation and return the chat to a ready state without ending the voice session.

#### Scenario: Stop button appears while generating

- **WHEN** the agent is generating a reply
- **THEN** the composer shows a stop affordance in place of the send button

#### Scenario: Stop interrupts generation

- **WHEN** the user taps stop while the agent is generating
- **THEN** generation stops and the chat returns to an idle, ready state

### Requirement: Suggested prompts in the empty state

The empty chat state SHALL offer suggested prompts that, when tapped, populate the composer without auto-sending.

#### Scenario: Suggested prompt fills the composer

- **WHEN** the chat has no messages and the user taps a suggested prompt
- **THEN** the composer is filled with that prompt and the user can edit before sending

### Requirement: Show live tool calls

The chat SHALL surface agent tool calls inline as expandable chips showing the tool name, and SHALL let the user expand a chip to see the call's arguments, result, and error status.

#### Scenario: Tool chip appears inline

- **WHEN** the agent invokes a tool during a text turn
- **THEN** the chat shows an expandable chip with the tool name at that point in the conversation

#### Scenario: Tool chip expands to details

- **WHEN** the user expands a tool chip
- **THEN** the chip reveals the tool arguments and, once available, the result

#### Scenario: Errored tool call is flagged

- **WHEN** a tool call fails
- **THEN** its chip indicates the error state

### Requirement: Show reasoning blocks

The chat SHALL surface agent reasoning (thinking) as a collapsible block separate from the reply, and SHALL append live reasoning deltas to that block as they stream.

#### Scenario: Thinking block appears above the reply

- **WHEN** the agent emits reasoning before its reply
- **THEN** the chat shows a collapsible thinking block above the reply

#### Scenario: Thinking block accumulates live

- **WHEN** reasoning deltas stream in
- **THEN** the thinking block grows with the new content

### Requirement: Surface approval requests

When a tool requires approval, the chat SHALL show an approval card identifying the tool and a summary of its arguments, with actions to approve or reject (optionally with a reason).

#### Scenario: Approval card appears

- **WHEN** the agent attempts a tool that requires approval
- **THEN** the chat shows an approval card with the tool name and an argument summary

#### Scenario: Approve

- **WHEN** the user approves the request
- **THEN** the chat sends the approval decision and the tool proceeds

#### Scenario: Reject with reason

- **WHEN** the user rejects the request, optionally with a reason
- **THEN** the chat sends the rejection decision (with the reason if provided) and the tool does not proceed

### Requirement: Surface questions from the agent

When the agent asks the user a question, the chat SHALL render it as an interactive card whose options (buttons, multiple choice, or free text) the user can answer, and SHALL show an answered state once submitted.

#### Scenario: Question card appears

- **WHEN** the agent asks the user a question
- **THEN** the chat shows an interactive question card with the available answer controls

#### Scenario: Answer submits

- **WHEN** the user selects an answer and submits
- **THEN** the chat sends the answer and shows the card in an answered, non-interactive state

### Requirement: Decisions resume the run

Submitting an approval or question decision SHALL resume the paused run and the resulting reply SHALL appear in the chat.

#### Scenario: Run resumes after a decision

- **WHEN** the user submits a decision on a pending approval or question
- **THEN** the agent resumes and its subsequent reply renders in the chat
