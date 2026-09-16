## 1. Gateway — kind registry refactor

- [ ] 1.1 `proposal.go`: replace the single `buildProposalCard` switch with a `proposalKinds` registry (`kind → {parse, render}`); unknown kind → summary-only; parse failure → summary-only (no error)
- [ ] 1.2 Keep `blueprint` behavior byte-identical through the registry (object/relationship previews + additions-only summary)
- [ ] 1.3 Unit tests: registry resolves `blueprint`; unknown kind → summary-only card; malformed body → summary-only, no error

## 2. Gateway — first-class kinds (skill / agent / mcp_server / provider)

- [ ] 2.1 Add body parsers + render models for `skill`, `agent`, `mcp_server`, `provider` (reuse apply-path structs: `BundledAgent`, skill detail row, MCP-server row, provider row)
- [ ] 2.2 `proposal.templ`: add per-kind card sections; provider body never renders the API key (masked indicator only)
- [ ] 2.3 Unit tests: each kind renders its fields; provider proposal masks the secret; each kind's parse failure → summary-only

## 3. Gateway — diff against current state

- [ ] 3.1 Introduce a `proposalContext` (current compiled types, agent definitions, skills, MCP servers); thread it into `renderProposalHTML`
- [ ] 3.2 `sse_markdown.go` + `run_history.go`: fetch/attach current state where project context exists; degrade to additions-only where absent
- [ ] 3.3 `proposalSummaryLabel` reports added/changed/removed from a non-empty `before` state (schema types + agent/skill/mcp diffs)
- [ ] 3.4 Unit tests: adding vs changing vs removing a type/agent/skill/mcp server produces the correct counts; no-state → additions-only

## 4. Gateway — `object` kind + operator scope

- [ ] 4.1 Add `object` kind: body `{entities[], relationships[]}` → entity/relationship preview section (renderer only, independent of the scope change)
- [ ] 4.2 Extend `operator.yaml` tools with `entity-create`/`relationship-create` and relax the read-only-graph guardrail to propose-then-write
- [ ] 4.3 Unit tests: `object` body renders entities + relationships; empty body → summary-only

## 5. Operator prompt

- [ ] 5.1 `operator.yaml` "Propose" step: emit the structured `proposal` argument for `skill`/`agent`/`mcp_server`/`provider`/`object`/`blueprint`; fenced block only for low-complexity resources (`token`, `document`, embedding changes)
- [ ] 5.2 Keep the propose→accept contract and the `ask_user` checkpoint unchanged

## 6. Verify

- [ ] 6.1 `go build ./...` + `go test ./...` in `apps/web-ui/gateway` (and `apps/server` if `object` scope changes the agents domain)
- [ ] 6.2 `templ generate` + gateway `task lint` clean
- [ ] 6.3 Manual/browser smoke: one proposal of each first-class kind renders its card (not a fence); Accept/Reject still respond via `/respond`
