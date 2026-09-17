## 1. Gateway — kind registry refactor

- [ ] 1.1 `proposal.go`: replace the single `buildProposalCard` switch with a `proposalKindBuilders` registry (`kind → build func`); unknown kind → summary-only; dedicated kind with empty/malformed body → nil (markdown)
- [ ] 1.2 Keep `blueprint` behavior byte-identical through the registry (object/relationship previews + additions-only summary)
- [ ] 1.3 Unit tests: registry resolves `blueprint`; unknown kind → summary-only card; malformed body → nil; empty blueprint → nil

## 2. Gateway — first-class kinds (skill / agent / mcp_server / provider)

- [ ] 2.1 Add body parsers + render models for `skill`, `agent`, `mcp_server`, `provider` (body shapes per design.md); provider body ignores any API-key field (never echoed)
- [ ] 2.2 `proposal.templ`: add per-kind card sections; provider shows slug + base URL + models, no secret
- [ ] 2.3 Unit tests: each kind renders its fields; provider proposal masks the secret; each kind's empty body → nil

## 3. Gateway — `object` kind (renderer-only)

- [ ] 3.1 Add `object` kind: body `{entities[], relationships[]}` → entity/relationship preview section (renderer only; prompt stays gated on the graph-write grant)
- [ ] 3.2 Unit tests: `object` body renders entities + relationships; empty body → nil

## 4. Operator prompt

- [ ] 4.1 `operator.yaml` "Propose" step: emit the structured `proposal` argument for `skill`/`agent`/`mcp_server`/`provider`/`blueprint`; fenced block only for low-complexity resources (`token`, `document`, embedding changes); `object` not yet listed (gated on the graph-write grant)
- [ ] 4.2 Keep the propose→accept contract and the `ask_user` checkpoint unchanged

## 5. Verify

- [ ] 5.1 `templ generate` + `go build ./...` + `go test ./...` in `apps/web-ui/gateway`
- [ ] 5.2 Gateway `task lint` clean
- [ ] 5.3 Manual/browser smoke: one proposal of each first-class kind renders its card (not a fence); Accept/Reject still respond via `/respond`
