# agent-run-message-author-role Specification

## Purpose

Pin the single persisted-author contract for `kb.agent_run_messages.role`: for an assistant turn the persisted role is the ADK event Author — the sanitized agent name (e.g. `coder`, `Norwegian_Law_Assistant`), never the literal `assistant` — so every consumer must classify replies through one shared predicate (`agents.IsAgentReplyRole`) instead of re-deriving the rule with scattered `role == "assistant"` equality checks. That equality bug shipped twice and silently dropped every agent reply with no error, so this capability also adds a `runrole-guard` preflight gate that fails any new literal comparison over run-message data while exempting genuinely different role contracts and documented display-layer sites.

## Requirements

### Requirement: Persisted run-message role is the ADK event Author, classified through one shared predicate

The persisted `kb.agent_run_messages.role` — surfaced in Go as
`AgentRunMessage.Role` and `ConversationHistoryItem.Role` — SHALL hold the message
author. For an assistant turn that author is the ADK event Author, the sanitized
agent name (e.g. `coder`, `Norwegian_Law_Assistant`), NOT the literal
`assistant` (an empty Author falls back to `assistant`, which the predicate still
accepts). Every consumer that classifies, filters, or extracts an agent reply
SHALL do so through the single shared predicate `agents.IsAgentReplyRole`, which
returns false only for the non-conversational sides (`""`, `user`, `tool`,
`tool_result`, `system`) and true for every other author. A consumer MUST NOT
compare a run-message role to the literal `"assistant"`. The persisted DB role
SHALL NOT be renamed.

#### Scenario: Author-named reply is an agent reply

- **WHEN** a run message carries role `coder` (a sanitized agent name)
- **THEN** `agents.IsAgentReplyRole("coder")` is true, and every consumer routing through it treats the message as an agent reply

#### Scenario: Non-conversational sides are excluded

- **WHEN** a run message carries role `""`, `user`, `tool`, `tool_result`, or `system`
- **THEN** `agents.IsAgentReplyRole` returns false and the message is never surfaced as the agent's reply

#### Scenario: Chat fallback recovers an author-named reply

- **WHEN** a succeeded run's persisted messages end with a tool result or a system row but contain an earlier message authored as `coder`
- **THEN** the chat streaming fallback returns the `coder` message's text and never the tool/system text

### Requirement: A guard rejects new literal-"assistant" role comparisons

A preflight guard SHALL fail when a run-message role is compared to the literal
`"assistant"` with `==` or `!=` anywhere under `apps/`, unless the site carries a
documented `runrole:allow` marker explaining why the comparison is not about
run-message data.

#### Scenario: A new literal comparison fails the guard

- **WHEN** a `role == "assistant"` or `role != "assistant"` comparison over run-message data is introduced under `apps/`
- **THEN** the `runrole-guard` gate fails and names the offending file and line

#### Scenario: Provider-protocol literals are not flagged

- **WHEN** the OpenAI/Anthropic adapters (`apps/server/pkg/adk`) or the OpenAI-compatible endpoint (`apps/server/domain/agentcompat`) compare a client request `messages[].role` to `"assistant"` or switch on `case "assistant":` (a different contract)
- **THEN** the guard does not flag the site

#### Scenario: Documented tolerant exceptions are allowlisted

- **WHEN** a display-layer site compares the role only to label the author and defaults every other role to the assistant side
- **THEN** the site carries a `runrole:allow` marker and the guard passes
