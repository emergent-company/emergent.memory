## Purpose

Corrects the public share chat rendering for the unarchived `agent-public-share` change: the shared chat MUST render assistant markdown through the same `.memory-md` wrapper as the admin chat, with NO whitespace-preserving wrapper on assistant markdown, while end-user plain-text bubbles keep preserving their line breaks.

## ADDED Requirements

### Requirement: Shared chat renders assistant markdown with collapsed soft line breaks
The public share chat SHALL render assistant message markdown through the `.memory-md` wrapper, matching the admin chat (`chat-stream.js`), and SHALL NOT apply a whitespace-preserving wrapper (`whitespace-pre-wrap`) to the assistant markdown node, so goldmark soft line breaks collapse instead of becoming visible breaks. End-user plain-text bubbles SHALL keep preserving their line breaks (they MAY keep `whitespace-pre-wrap`). The assistant content node MUST keep the `break-words` behavior.

#### Scenario: Assistant markdown soft line breaks collapse
- **WHEN** an assistant reply is rendered in the shared chat
- **THEN** its markdown wrapper carries `memory-md` and `break-words` but no `whitespace-pre-wrap`, so soft line breaks collapse exactly as they do in the admin chat

#### Scenario: End-user plain text keeps its line breaks
- **WHEN** an end-user message is rendered in the shared chat
- **THEN** its plain-text bubble still preserves explicit line breaks via `whitespace-pre-wrap`
