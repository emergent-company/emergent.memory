## 1. Server — schema + model

- [x] 1.1 Migration: add `proposal jsonb` (nullable) to `kb.agent_questions` (`apps/server/migrations/00154_add_agent_question_proposal.sql`)
- [x] 1.2 `entity.go`: add `Proposal` field to `AgentQuestion` (Bun model, `jsonb`) + extend `AgentQuestionDTO`
- [x] 1.3 Persist/read `proposal` via the existing `CreateQuestion` `Insert().Model(q)` path (Bun reflects the new field — no repository change needed); DTO mapping carries it

## 2. Server — ask_user tool

- [x] 2.1 `ask_user_tool.go`: accept optional `proposal` arg; `parseProposal` validates the envelope (`kind`/`summary`/`body`); on invalid, drop the payload and proceed as plain-text (no hard error)
- [x] 2.2 Thread `proposal` through `CreateAndEmitQuestion` → `AgentQuestion` → `emitQuestionSSEEventDirect` (field `proposal`, only when non-nil)
- [x] 2.3 Unit tests: valid `proposal` persisted (INSERT includes `proposal` column); invalid `proposal` → nil, no error; absent `proposal` unchanged; SSE includes/omits `proposal`

## 3. Gateway — render proposal card

- [x] 3.1 `proposal.go` + `sse_markdown.go`: parse `proposal` from the ask_user input; `buildProposalCard` → `ObjectTypeDetail`/`RelationshipTypeDetail` via `objectTypesFromMaps`/`relationshipTypesFromMaps`, diff vs current compiled types (`diffObjectTypes`/`diffRelationshipTypes`); render → `proposalHtml`
- [x] 3.2 `proposal.templ` `proposalCard` reuses `objectTypeRow`/`relationshipTypeRow` + `ui.Badge`/`ui.Eyebrow` (no new design system)
- [x] 3.3 Include `proposalHtml` in the `question` SSE event + history rehydration (`proposal_html`)
- [x] 3.4 `chat-stream.js`: inject `proposalHtml` above the options; fall back to `questionHtml` when absent
- [x] 3.5 Unit tests: blueprint proposal → card HTML with object/relationship rows + summary; no-proposal question → markdown unchanged

## 4. Operator prompt

- [x] 4.1 `operator.yaml` "Propose" step: concise markdown summary in `question` (no raw JSON fence) + manifest in structured `proposal` arg for blueprint/schema changes
- [x] 4.2 Full gateway test suite passes (includes `TestOperatorBlueprintDefinesOperatorAgent` and blueprint install tests; prompt still mandates propose→accept)

## 5. Verify

- [x] 5.1 `go build ./...` + `go test ./...` in `apps/server` (domain/agents) and `apps/web-ui/gateway`
- [x] 5.2 Lint/vet/gofmt clean for changed code (gateway `golangci-lint` 0 issues; server changed files clean; remaining server lint findings are pre-existing in untouched `workspace_tools.go`/`handler_test.go`)
- [ ] 5.3 Manual/browser smoke: operator proposal renders as a card (not raw JSON); Accept/Reject/Edit still respond via `/respond`
