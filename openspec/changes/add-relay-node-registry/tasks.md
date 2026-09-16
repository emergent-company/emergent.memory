# Tasks — add-relay-node-registry

TDD in the gateway module (Go tests with the existing fakes). Verify with
`go build ./...`, `templ generate`, `task lint`, `go test ./...` in `gateway/`.

## 1. Registry storage (settings KV)

- [x] 1.1 Registry model + helpers (category `mcp_relay_nodes`, key `registry`,
  value `{"nodes":{...}}`): `loadRelayRegistry`, `upsertRelayNode`,
  `removeRelayNode`, tolerant of missing/partial entries. Unit tests
  (upsert creates/refreshes, remove deletes, 404 → empty, corrupt value →
  empty).
- [x] 1.2 `relayNodeRecord` fields (version, toolCount, tools snapshot,
  firstSeen, lastSeen) with RFC3339 timestamps; first-seen never overwritten on
  refresh. Tests.

## 2. Page reconciliation (uiMCPNodes)

- [x] 2.1 On page load: fetch live sessions, upsert each (lastSeen = now,
  refresh version/toolCount), then build rows = live first (connected_at desc)
  + offline registry entries (lastSeen desc). Tests with the fake backend
  (live node upserted; previously-seen node absent → offline row).
- [x] 2.2 On live-fetch failure: render the registry (offline) plus an error
  banner instead of a blank page. Test.
- [x] 2.3 Tools panel: live → `GetRelaySessionTools`; offline → stored
  snapshot (empty snapshot → "no tools recorded"). Tests.
- [x] 2.4 Cache the tool snapshot on observation (so offline nodes can show
  tools); cover in tests.

## 3. Removal route

- [x] 3.1 `POST /settings/mcp-nodes/remove` (field `instance_id`) → delete the
  registry entry → `303` redirect to `/settings/mcp-nodes?updated=1`; error via
  `redirectWithError`. Route registered in `main.go`. Tests (303 + Location,
  entry deleted, unknown id tolerated).

## 4. UI (templ)

- [x] 4.1 Rows: green dot + `Connected` for live, gray dot + `Disconnected`
  (muted) for offline, each with the connection / `last seen <relative>` line;
  offline tools panel reads the snapshot and notes the snapshot time via the
  node's last-seen; "no tools recorded" state.
- [x] 4.2 Restructure the row so the link and a compact destructive **Remove**
  icon button coexist (no nested interactive elements); confirmation via a
  shared native `modalShell` dialog whose form POSTs `/settings/mcp-nodes/remove`.
- [x] 4.3 Render tests: live row, offline row with last-seen + snapshot note,
  remove button/dialog/action present, empty state.

## 5. Gate

- [ ] 5.1 `go build ./...`, `templ generate`, `task lint`, `go test ./...`
  green in `gateway/`.
- [ ] 5.2 Manual check on dev: disconnect a node (app toggle off) → node shows
  offline with last-seen; Remove deletes it.
