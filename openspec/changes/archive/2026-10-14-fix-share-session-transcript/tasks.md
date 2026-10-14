## 1. Server — transcript author-role mapping (TDD)

- [x] 1.1 Extend `TestGetSessionTranscriptByID_FiltersMessages` to include an agent-name role (`coder`) with text (must map to `assistant`), `tool`/`tool_result` roles (must be dropped), a non-message `tool_call` kind, and an empty-text message, asserting ordering; confirm it fails on the current code
- [x] 1.2 Implement `sessionTranscript` mapping: keep `Kind == "message"` and non-empty `Content["text"]`; `user` → `user`; `isAgentReplyRole(role)` → `assistant`; everything else (`system`, `tool`, `tool_result`, `""`) skipped
- [x] 1.3 `go test ./domain/agents/...` green

## 2. Web UI — render parity

- [x] 2.1 `share-agent.js`: drop `whitespace-pre-wrap` from the assistant wrapper (now `memory-md break-words`); keep it on the end-user plain-text bubble
- [x] 2.2 Rename `alfred-md` → `memory-md` in `share_session_transcript.templ` (2) and `sessions.templ` (4)
- [x] 2.3 `templ generate`

## 3. Spec

- [x] 3.1 Add delta specs (`## ADDED Requirements`) for `agent-public-share`, `agent-public-share-runtime`, `web-agent-public-share`
- [x] 3.2 `openspec validate fix-share-session-transcript --strict` clean

## 4. Verification

- [x] 4.1 `cd apps/server && go build ./... && go test ./domain/agents/...`
- [x] 4.2 `cd apps/web-ui/gateway && templ generate && go build ./... && go test ./...`
- [x] 4.3 `task lint` from the repo root
- [x] 4.4 `openspec validate fix-share-session-transcript --strict`
