## 1. Gateway — QR config endpoint

- [x] 1.1 Add `GET /api/qr-config` returning the same payload as `admin.py` `_qr_payload()` (serverURL, tokenEndpoint, apiBaseURL, apiKey, agentName, roomName)
- [x] 1.2 Guard under the same auth as the rest of the API (dev-mode pass-through when no key)

## 2. Gateway — session-log API (Memory-sourced)

- [x] 2.1 Add Memory client methods to list conversations + read conversation history (reuse `session_dump.go` timeline parsing)
- [x] 2.2 Add `GET /api/sessions` mapping Memory conversations → the iOS `SessionSummary` contract (room, started_at, ended_at, turns, tool_calls, preview), most recent first
- [x] 2.3 Add `GET /api/session?room=` mapping a conversation's ACP history → the iOS `SessionRecord` timeline (turn / tools_executed records with name, args, result, error flag)
- [x] 2.4 Unknown room → empty timeline, not an error

## 3. Gateway — memory proxy

- [x] 3.1 Add `GET /api/memories/capability?agent=<name>` deriving memory-capability from the agent-definition's MCP references (`memory` server)
- [x] 3.2 Add `GET /api/memories?agent=<name>&query=<text>` proxying search via Memory REST (`/api/search/unified`), empty result for memory-less agents
- [x] 3.3 Normalize entity results to `{id, content, category, confidence}` (Note vs typed entities), matching admin.py `_memory_item`

## 4. iOS — retarget to gateway

- [x] 4.1 Point `alfred.apiBaseURL` and the token endpoint default at the gateway (single origin), keeping QR-scan override

## 5. Retire legacy

- [x] 5.1 Delete `agent/admin.py`, `agent/api/` (FastAPI), `agent/main*.py`, `agent/factory.py`, `agent/patches.py`, `agent/ha_tools.py`, `agent/ha_catalog.py`, `agent/session_lifecycle.py`, `agent/stt_local.py`, `agent/tts_local.py`, `data/agents.db`
- [x] 5.2 Delete the orphaned `ui/` go-daisy module
- [x] 5.3 Update `deploy.sh` + systemd: drop `alfred-admin`, `alfred-api`, `alfred-worker@alfred-google-rt`; keep gateway + bridge workers

## 6. Verify

- [x] 6.1 `go build ./...` + `go vet` + `go test ./...` in `gateway/`
- [x] 6.2 `ruff check` (Python deletions leave no dangling imports)
- [ ] 6.3 Smoke-test `GET /api/qr-config`, `/api/sessions`, `/api/memories/capability` against a live gateway
- [x] 6.4 iOS rebuild in Xcode against the gateway (no FastAPI/admin dependency)
