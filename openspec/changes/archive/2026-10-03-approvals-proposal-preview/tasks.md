## 1. Render the proposal preview on the fallback surface

- [x] 1.1 In `apps/web-ui/gateway/approvals.templ`'s `questionRow`, render `q.Proposal` (when non-empty) through `renderProposalHTML` above the answer controls, tagged `data-testid="pending-question-proposal"`.
- [x] 1.2 Keep questions without a renderable proposal on the existing plain-text path (no behavior change).

## 2. Tests

- [x] 2.1 Add a render test asserting a pending question's blueprint proposal appears on `/settings/approvals` (card kind + preview content) with the answer control still present.
- [x] 2.2 Add a render test asserting a hostile proposal string is escaped, not emitted as live HTML.

## 3. OpenSpec delta

- [x] 3.1 Add the `agent-proposals` requirement delta: the conversation-less fallback surface renders the proposal card.

## 4. Verification

- [x] 4.1 `templ generate`, `go build ./...`, `go test ./...` pass from `apps/web-ui/gateway`.
- [x] 4.2 `task lint` (golangci-lint) passes.
- [x] 4.3 Hermetic js-dom gate (`npx playwright test --config=js-dom.config.ts`) passes.
- [x] 4.4 `bash apps/server/scripts/lint-ratchet.sh` passes.
- [x] 4.5 `openspec validate approvals-proposal-preview --strict` passes.
