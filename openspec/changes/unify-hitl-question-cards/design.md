## Context

Two surfaces must show the same question/approval card:

1. `/settings/approvals` — a server-rendered page (`approvals.templ`), rendering pending questions
   and the approval audit trail.
2. The chat transcript — cards are **client-generated** from SSE `question` / `approval` events
   (`chat-stream.js` `renderQuestion` / `renderApproval`); the server never renders them because the
   transcript is live.

There is also a third server-rendered consumer: the chat's `<noscript>` dock (`chat_dock.templ`
`DockPanel`).

So a single Go `templ` component can be rendered directly by (1) and (3), but not by (2).

## Decision 1 — templ component is canonical; the chat JS mirrors it

Chosen: extract `components.QuestionCard` / `components.ApprovalCard` as the single source of the
markup, render them from `/settings/approvals` and the no-JS dock, and align the chat's JS renderer
to the same structure and classes, guarded by a contract test that fails when the emitted
`data-testid` set or class skeleton diverges.

Rejected alternatives:

- **Server-rendered `<template>` cloned by chat JS.** Literally one markup source (there is precedent
  in `app.js` cloning `#property-row-template`), but it moves card construction into a clone-and-fill
  routine with dynamic option counts, re-keys escaping through the clone path, and rewrites a large,
  well-tested streaming renderer for no user-visible gain. The chat card is inherently dynamic; the
  cost outweighs the purity.
- **Only restyle the approvals page.** Cheapest, but leaves two definitions of the same card and
  repeats the drift this change exists to stop.

The contract test is what makes "mirrors it" enforceable rather than aspirational: it asserts the
rendered testids and class skeleton of both renderers, not colours or class order (per
`web-ui-component-conventions` "Component tests assert the contract, not class order").

## Decision 2 — resolve the question's conversation on the server

`AgentQuestionDTO` gains `conversationId`, resolved by the same LEFT JOIN the tool-approval list uses
(`agent_runs.session_id` → `chat_conversations.session_id`). Alternative: resolve per-question in the
gateway, which would add a lookup per question and a second implementation of a mapping the server
already owns. The DTO mirrors `AgentToolApprovalDTO`, so the two lists stay symmetrical.

## Decision 3 — component shape

`QuestionCardProps` carries the prompt (pre-sanitized HTML), interaction kind, options
(`label`/`value`/`description`), proposal HTML, and the conversation id; `Mode` is one enum
(`interactive` | `answered`) rather than booleans, and the answer controls are children/slots so the
integration site owns the htmx/POST contract (`web-ui-component-conventions` "The integration site
owns the htmx swap contract"). The approvals page keeps its existing form based POST flow, so
answering from the page is unchanged behaviourally — only the visual is unified.

## Decision 4 — scope

A2UI cards and the public share page keep their own renderers for now; both are separately owned
(`agent-structured-ui` is an in-flight change) and neither is the surface this request is about.
Follow-up issues track them.
