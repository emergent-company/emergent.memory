## 1. Server — schema + model

- [ ] 1.1 Migration: add `proposal jsonb` (nullable) to `kb.agent_questions` (new `apps/server/migrations/NNNN_add_agent_question_proposal.sql`)
- [ ] 1.2 `entity.go`: add `Proposal` field to `AgentQuestion` (Bun model, `jsonb`) + extend `AgentQuestionDTO`
- [ ] 1.3 `repository.go`: persist/read `proposal` in `CreateQuestion` and the read/mapping paths

## 2. Server — ask_user tool

- [ ] 2.1 `ask_user_tool.go`: accept optional `proposal` arg; coerce/validate against the envelope (`kind`/`summary`/`body`); on invalid, drop the payload and proceed as plain-text (no hard error)
- [ ] 2.2 Thread `proposal` through `CreateAndEmitQuestion` → `AgentQuestion` → `emitQuestionSSEEventDirect`
- [ ] 2.3 Unit tests: valid `proposal` persisted; invalid `proposal` → plain question, no error; absent `proposal` unchanged

## 3. Gateway — render proposal card

- [ ] 3.1 `sse_markdown.go`: parse `proposal` from the question event; if present, convert `body` → `ObjectTypeDetail`/`RelationshipTypeDetail` via `objectTypesFromMaps`/`propertiesFromMap`, diff vs current compiled types (`diffObjectTypes`/`diffRelationshipTypes`), and render a card → `proposalHtml`
- [ ] 3.2 Reuse `blueprints.templ`/`schema.templ` preview components (`objectTypeRow`/`relationshipTypeRow`/diff badges) in a new `proposal` templ partial
- [ ] 3.3 Include `proposalHtml` in the `question` SSE event + history rehydration path
- [ ] 3.4 `chat-stream.js`: inject `proposalHtml` above the options; fall back to `questionHtml` when absent
- [ ] 3.5 Unit tests: blueprint proposal → card HTML with object/relationship rows + summary; no-proposal question → markdown unchanged

## 4. Operator prompt

- [ ] 4.1 `operator.yaml` "Propose" step: emit a concise markdown summary in `question` + the manifest in `proposal` (drop the raw JSON fence)
- [ ] 4.2 Verify `TestOperatorBlueprintDefinesOperatorAgent` / blueprint install tests still pass (prompt contract unchanged: still mandates propose→accept)

## 5. Verify

- [ ] 5.1 `go build ./...` + `go test ./...` in `apps/server` and `apps/web-ui/gateway`
- [ ] 5.2 `task lint` (server + gateway)
- [ ] 5.3 Manual/browser smoke: operator proposal renders as a card (not raw JSON); Accept/Reject/Edit still respond via `/respond`
