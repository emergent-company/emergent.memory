## Why

Issue #1425: a pending agent question can carry a structured `proposal` payload (a schema/object change the
user is asked to approve). The inline chat transcript renders it as a sanitized proposal card, but the
conversation-less fallback surface `/settings/approvals` — the notification target for a run with no chat
conversation (#1375, #1417) — renders only the question text and answer controls. The user is therefore
asked to approve a change they cannot see.

This is a security-relevant UX gap: the proposal preview is the review step before an approve/reject
decision. It is also an inconsistency between the two question surfaces, which both read the same
`AgentQuestionItem` DTO that already carries `proposal`.

## What Changes

- The approvals question row renders a non-empty `proposal` through the existing sanitized
  `renderProposalHTML` path — the same `proposalCard` renderer the chat transcript uses — placed above the
  answer controls.
- A question without a renderable proposal keeps its current plain-text rendering (unchanged).
- One gateway render test asserts the preview appears and that a hostile proposal string is escaped
  (agent/LLM output is untrusted; `gateway/AGENTS.md`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-proposals` — adds the requirement that the conversation-less fallback surface renders the proposal
  card, so proposal rendering is consistent across every question surface.

## Scope

- **Gateway web UI only.** `approvals.templ` (question row), one render test, and this OpenSpec delta.
- **Out of scope:** the chat dock proposal preview (issue #1425 notes `chat_dock.templ` also omits it, as a
  consistent-with-analog gap). This change fixes the `/settings/approvals` fallback named in the issue.

## Impact

- **Gateway files:** `approvals.templ` (render `q.Proposal` via `renderProposalHTML`), `handlers_test.go`
  (two render tests).
- **No Go logic, API, schema, or migration change.** The render path already exists and is used by the SSE
  transcript; this is additive UI.
- **Verification:** `templ generate`, `go build ./...`, `go test ./...`, `task lint`, the hermetic js-dom
  Playwright gate, and `lint-ratchet.sh` from `apps/web-ui/gateway`.
