## 1. Gateway — fence fallback renderer

- [x] 1.1 `proposal.go`: add `proposalFromQuestionText(question string) *ProposalCard` — extract the first `json`/`yaml`/`yml` fence, parse to `map[string]any`, recognize a `packs` array or bare pack, and build the card through `buildBlueprintCard` (zero types → nil; parse error → nil; unrecognized → nil)
- [x] 1.2 Add `renderProposalCardHTML(*ProposalCard) string` ("" for nil) and route `renderProposalHTML` through it; add `fallbackProposalHTML(proposal, question)` (structured wins, else fence, else "")
- [x] 1.3 `sse_markdown.go`: use `fallbackProposalHTML` at the live `question` event; in `renderHistoryHTML`, derive `proposal_html` from the question fence only when the input has no `proposal` field
- [x] 1.4 Unit tests: real-incident JSON fence, YAML fence, bare pack, multi-pack summary, non-manifest/malformed/prose/unknown-lang/bare/unterminated/empty-packs → nil, exactly-once rendering

## 2. Gateway — stream/history wiring tests

- [x] 2.1 Live stream: ask_user with a manifest fence and no proposal emits `proposalHtml`; plain text emits none; a structured proposal wins (no fence override, no double render)
- [x] 2.2 History: ask_user tool_call with a fence and no proposal gains `proposal_html`; plain text does not; structured proposal wins

## 3. Server — discoverability

- [x] 3.1 `ask_user_tool.go`: extend the tool `Description` to advertise the optional `proposal` argument (envelope `{kind,summary,body}`, kinds, blueprint body) — no runtime/validation change
- [x] 3.2 Unit test: tool description mentions `proposal` / `objectTypes` / `blueprint`

## 4. Blueprint-architect prompt

- [x] 4.1 `blueprint-architect.yaml`: ambiguity (step 6) and emit (step 11) checkpoints instruct the structured `proposal` (kind `blueprint`, body `{objectTypes, relationshipTypes}`) with a short markdown question — never a raw JSON/YAML fence

## 5. Spec + docs

- [x] 5.1 OpenSpec change dir `proposal-card-fence-fallback/` with delta spec (ADDED requirements) for the fence fallback and the `proposal` argument advertisement
- [x] 5.2 Update `apps/web-ui/docs/spec/14-assistant-agent.md` §"Proposal / accept flow" to reflect the shipped structured `proposal` + fence fallback (drop the stale fenced-block A/C wording)

## 6. Verify

- [x] 6.1 `go build ./...` + `go test ./...` in `apps/web-ui/gateway`
- [x] 6.2 `go build ./...` + `go test ./domain/agents/...` in `apps/server`
- [x] 6.3 `openspec validate proposal-card-fence-fallback --strict`
