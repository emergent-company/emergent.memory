## Why

The session trace viewer and the live chat surface render tool calls without two pieces of identifying detail — how long the call took and its call id — so a slow tool or a specific invocation is hard to pin down. Separately, the composed system instruction (the agent's base prompt plus the skills block, appendix, and policy guidance actually sent to the model) is never recorded, so neither surface can show what the agent was instructed to do; the raw `kb.agent_definitions.system_prompt` is not the prompt the model saw.

## What Changes

- Record the composed system instruction as the run's first `system` run-message at execution time, so it becomes part of the recorded transcript (and history/API payloads) instead of being lost after the LLM call.
- Carry the tool-call id and execution duration on timeline tool-call records (the duration already exists server-side; the id is added; both are plumbed through the gateway).
- Session trace viewer: show an "Agent prompt" card at the very top of the conversation, rendered like a tool-call card (collapsible body); tool-call cards gain the execution duration and tool-call id.
- Live chat: render the same "Agent prompt" card once at the start of the conversation; the tool-call chip's expanded detail gains the execution duration and tool-call id.

## Capabilities

### New Capabilities

- `web-chat-transcript`: how the live web chat surface renders the conversation transcript — the agent's composed prompt card at the start of a conversation and the execution duration / call id shown with each tool call.

### Modified Capabilities

- `session-log-api`: timeline tool-call records gain the call id and execution duration; the timeline includes the run's composed agent instruction as a `system` message record.
- `session-trace-viewer`: the viewer shows the agent prompt like a tool-call card at the top of the conversation, and tool-call cards show the call id and execution duration.

## Impact

- Server (`apps/server/domain/agents`): `runPipeline` persists the resolved (post-workspace-augmentation) system instruction as a `system` `kb.agent_run_messages` row; `ConversationHistoryItem` gains the tool-call id (duration already present) and populates both for tool-call records. No migration — reuses the existing run-message table and jsonb content.
- Gateway (`apps/web-ui/gateway`): `TimelineItem` and the run DTO mirror gain `id` and `duration_ms`; history rendering skips markdown-html for `system` records; session viewer templ gains an agent-prompt card; live chat JS renders the prompt card and the tool detail gains duration/id.
- No new endpoints; the existing conversation-history and run-history payloads carry the new fields.
