## 1. Backend — memory proxy in agent/admin.py

- [x] 1.1 Add a `_agent_has_memory(name)` helper that reads the agent from `Store` and returns whether its `tools.mcp` references the MCP server named `memory`
- [x] 1.2 Add `GET /api/memories/capability?agent=<name>` returning `{"agent": name, "hasMemory": bool}` (404 for unknown agent), guarded by the same `X-API-Key` as `/api/token`
- [x] 1.3 Add `GET /api/memories?agent=<name>&query=<text>` that proxies a read-only search to the memory service REST search endpoint using `MEMORY_URL` + `Bearer MEMORY_TOKEN` + `MEMORY_PROJECT_ID`, guarded by `X-API-Key`
- [x] 1.4 Implement only GET routes (no write path); return `{"memories": []}` for a memory-less agent and a structured error for memory-service reachability failures
- [x] 1.5 Verify with curl on home2: capability `true` for `diane`, `false` for `alfred-google-rt`; search returns a known saved note; restart `alfred-admin.service`

## 2. iOS — agent info client + capability discovery

- [x] 2.1 Add an `AgentInfoClient` that derives the memory API base URL from the token endpoint host (same host, port 8080) and sends the configured `X-API-Key`
- [x] 2.2 Add a `Memory` `Codable` model matching the API response (id, content, category, confidence)
- [x] 2.3 Fetch memory capability for the selected agent and expose it to the UI (re-fetch when the picker selection changes)

## 3. iOS — memory browser UI

- [x] 3.1 Add a `MemoriesView` using `NavigationStack` + `List` with `.searchable`, fetching and listing the selected agent's memories on open
- [x] 3.2 Add a `MemoryDetailView` showing a memory's full content, pushed from the list
- [x] 3.3 Add empty-state ("no memories yet") and error-state (memory service unreachable) presentations
- [x] 3.4 Add a memories entry point to the UI, shown only when the selected agent is memory-capable
- [x] 3.5 Add new localization keys to `Localizable.xcstrings`

## 4. Verification

- [ ] 4.1 Rebuild the iOS app in Xcode and confirm: Diane shows the memories entry point, Alfred does not; browse, search, and detail all work
- [x] 4.2 Confirm no regression to `/api/token` or the prompt editor on `admin.py`
