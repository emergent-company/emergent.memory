## Why

`kb.agent_run_messages.role` persists the message **author**, and for an assistant
turn that author is the ADK event Author — the sanitized agent name (`coder`,
`Norwegian_Law_Assistant`, …), **never** the literal `assistant` for a real run
(only an empty Author falls back to `assistant`; see `persistEventContent`). The
rule lived only in the package-private
`isAgentReplyRole` helper and in scattered comments, so consumers kept
re-deriving it: a `role == "assistant"` equality check silently matches no real
run and drops every agent reply with no error.

That bug shipped twice — `share_service.go` `sessionTranscript` (#1824) and
`apps/web-ui/gateway/share.go` (fixed indirectly when #1871 unified the share
chat with the admin engine). Nothing stopped a third consumer from re-introducing
it.

## What Changes

- **Promote the contract to a shared API.** Export `agents.IsAgentReplyRole`
  (identical behaviour to the former `isAgentReplyRole`) and document the
  persisted-author contract on `AgentRunMessage.Role` and
  `ConversationHistoryItem.Role`.
- **Route consumers through it.** The chat streaming fallback
  (`domain/chat/handler.go`) no longer inline-compares the role set; it extracts
  the last agent reply via a new `lastAgentReplyText` helper built on
  `agents.IsAgentReplyRole`. The `call_agent` reply extractor, the A2A mapper, the
  share transcript, and the run-history reader already route through the helper.
- **Regression tests.** `TestIsAgentReplyRole` pins the predicate; the chat
  helper test proves a reply authored under a sanitized agent name (`coder`) is
  recovered end-to-end at the fallback consumer, and that tool/system sides are
  never surfaced.
- **Guard.** A new AST-based `runrole-guard` (wired into
  `scripts/preflight/all.sh`) fails on any new `role == "assistant"` /
  `role != "assistant"` comparison over run-message data, with a documented
  `runrole:allow` marker for the one pre-existing tolerant display-layer site
  (`sessions.templ`). Provider-protocol literals (`apps/server/pkg/adk`,
  `apps/server/domain/agentcompat`) are exempt — a different contract. The guard
  has its own unit tests (`cmd/runrole-guard/main_test.go`).

## Impact

- No DB migration, no persisted-role rename, no new dependency.
- `apps/server`: `domain/agents`, `domain/chat`.
- `apps/web-ui/gateway`: `sessions.templ` (allow marker only).
- New preflight gate + `apps/server/cmd/runrole-guard`.
