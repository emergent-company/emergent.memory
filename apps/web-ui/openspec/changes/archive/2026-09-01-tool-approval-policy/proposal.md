## Why

The memory backend already ships a tool-permission engine (`AgentDefinition.ToolPolicies`, with `Confirm`/`Disabled` booleans gating tool dispatch), but the human-in-the-loop UX is broken in ways that make it effectively unusable for the assistant "write into a field" flow: a rejected tool call is injected back to the agent as a raw *error* (so the model treats "no" as a retryable failure rather than a decision), and the approval prompt is emitted out-of-band via the events service — it never reaches the gateway's chat/side-panel stream, so the user has no in-conversation place to approve or reject. There is also no audit trail of who approved what, no per-agent default policy (any newly-granted tool auto-executes under default-allow), and no way to revoke a pending approval.

## What Changes

Extend the existing engine rather than build a parallel one:

- **Policy model**: widen `ToolPolicy` semantics to `allow | deny | ask` and add a per-agent `defaultPolicy` so absent entries no longer silently mean "allow".
- **Structured rejection**: on reject, inject a synthetic tool result `{policy_decision: "rejected", reason: <message>}` instead of an error, so the agent can adapt in-turn.
- **In-stream approval event**: emit the approval prompt into the chat stream (not just out-of-band), and pass it through the gateway so it renders as an approval card in the chat / side panel.
- **Approval card UI (gateway)**: Approve / Reject (with optional feedback message), visually distinct from question cards.
- **Approval respond path (gateway)**: reuse the existing question-respond route, extended to carry `{approve, message}`.
- **Audit log**: record every decision — agent, tool, args summary (redacted), decision, message, user, timestamp — with retention and a gateway audit view.
- **Revocation**: allow cancelling a pending approval; cancellation propagates to the paused run.

## Capabilities

### New Capabilities
- `tool-approval-policy`: human-in-the-loop tool gating — per-tool policy semantics (`allow`/`deny`/`ask` + per-agent default), in-conversation approval cards, structured rejection results, approval respond, audit trail, and pending-approval revocation.

### Modified Capabilities
<!-- none -->

## Impact

- **Memory backend** (`emergent.memory`, external repo — not this checkout): `ToolPolicy` model + `defaultPolicy`, executor confirm gate (`beforeToolCb`/`injectToolResponse`), chat `streamCallback` approval event, new `approvals`/`approval_decisions` tables. This repo's work depends on that landing first.
- **Gateway** (`/root/alfred/gateway`): `sse_markdown.go` approval-event passthrough, `handlers.go` respond route, `sidepanel.js`/`chat.js` approval card, new audit view (templ + handler), authz on approve/audit endpoints.
