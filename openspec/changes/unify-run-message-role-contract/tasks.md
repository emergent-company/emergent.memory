# Tasks

## 1. Server — promote and route the contract (`apps/server`)

- [x] 1.1 Export `agents.IsAgentReplyRole` (identical behaviour to `isAgentReplyRole`); document the persisted-author contract on `AgentRunMessage.Role` and `ConversationHistoryItem.Role` (`runrole-guard` enforces "do not compare to `assistant`")
- [x] 1.2 Route the remaining consumer through it: extract `lastAgentReplyText` in `domain/chat/handler.go` (chat streaming fallback) and classify with `agents.IsAgentReplyRole`
- [x] 1.3 Unit test `TestIsAgentReplyRole` pinning the predicate (agent names and literal `assistant` accepted; `""`/`user`/`tool`/`tool_result`/`system` rejected)
- [x] 1.4 Regression test `lastAgentReplyText` — a reply authored under `coder` is recovered; tool/system sides never surface; empty when no agent turn exists

## 2. Guard (`apps/server/cmd/runrole-guard`, `scripts/preflight`)

- [x] 2.1 AST-based guard fails on `role == "assistant"` / `role != "assistant"` over run-message data under `apps/`, with a documented `runrole:allow` marker window
- [x] 2.2 Exempt provider-protocol literals (`apps/server/pkg/adk`, `apps/server/domain/agentcompat`) and ignore switch-case clauses; skip generated `*_templ.go`, swagger, vendor
- [x] 2.3 Allowlist the one pre-existing tolerant display-layer comparison (`sessions.templ` author label) with a justification
- [x] 2.4 Wire `scripts/preflight/runrole-guard.sh` into `scripts/preflight/all.sh` (the required `ci` job); red-before/green-after verified
- [x] 2.5 Guard self-tests (`cmd/runrole-guard/main_test.go`) pin the detection: role/author/selector/index/type-assert forms caught, `.templ` `//` comments skipped, `runrole:allow` honored, case-sensitive literal, unrelated identifiers not flagged

## 3. Verification

- [x] 3.1 `cd apps/server && go build ./... && go vet ./... && go test ./domain/agents/... ./domain/chat/...`
- [x] 3.2 `cd apps/web-ui/gateway && templ generate ./... && go build ./... && go test ./...`
- [x] 3.3 `gofmt -l` clean on changed `.go` files; `bash scripts/preflight/runrole-guard.sh` green
- [x] 3.4 `openspec validate unify-run-message-role-contract`
