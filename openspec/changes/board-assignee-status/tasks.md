## 1. Server — exact-status work-path endpoint

- [x] 1.1 `graph.ValidateWorkObjectStatus(cfg, status)` — reject a board-enabled type whose `AllowedStatuses` is non-empty and does not include `status`; accept any non-empty status otherwise.
- [x] 1.2 `graph.Service.UpdateWorkObjectStatus(ctx, projectID, canonicalID, status, reviewStatus)` — resolve the object type, apply the agent-write guard, and `transitionWorkObject` with `FromStatus=""`, `ToStatus=status`; `needs_review=true` when target==review; leave `reviewed_by`/`reviewed_at` untouched; emit `status_changed`; do not enqueue a run.
- [x] 1.3 `WorkObjectStore.UpdateWorkObjectStatus` interface method + fakes (`fakeWorkObjectStore`, `recordingWorkObjectStore`).
- [x] 1.4 `WorkActionService.SetStatus(ctx, projectID, canonicalID, status)` — empty → 400; validate via the work config; dispatch to `UpdateWorkObjectStatus`.
- [x] 1.5 `UpdateWorkItemStatusDTO` + `UpdateWorkItemStatus` handler (route `POST /api/projects/:projectId/work-items/:canonicalId/status`), reusing `validateTypeStatus` disallowed-status semantics.
- [x] 1.6 Tests: set-status succeeds, empty rejected, disallowed rejected, agent-actor rejected, review marks needs_review, no run enqueued (`work_actions_test.go`); graph transition + agent rejection (`work_transition_test.go`).

## 2. Gateway — ticket-style assignee picker + set-status control

- [x] 2.1 `MemoryClient.SetWorkItemStatus` (`memory_work.go`) + `MemoryBackend` interface method + `fakeMemory` + `lastStatusTo` field.
- [x] 2.2 `boardAssigneeCandidate` + `buildBoardAssigneeCandidates` (agents by name; members by display name → email) + `boardAssigneeCandidates` (best-effort fetch) + `boardStatusOptions` (vocabulary minus current).
- [x] 2.3 `uiBoardSetStatus` handler + `/board/items/:canonicalId/status` route.
- [x] 2.4 Thread candidate lists through the preview action slot (`object_preview.go`) only when `?actions=board`; read-only preview stays read-only and never fetches candidates.
- [x] 2.5 `board.templ`: replace the plain reassign input with `boardAssigneePicker`; add `boardStatusControl` to the drawer metadata row.
- [x] 2.6 Tests: picker renders candidates + posts reassign; status control renders options minus current + posts `/status`; set-status handler forwards the posted status; read-only preview never calls `ListScheduledAgents`/`ListMembers`.

## 3. E2E

- [x] 3.1 `board-assignee-status-ui.spec.ts` — assignee picker reassigns to a member; status control moves the item to an exact phase.
- [x] 3.2 Update `board-lifecycle-ui.spec.ts` reassign step to drive the picker instead of the removed free-text input.

## 4. Verification

- [x] 4.1 `templ generate` + component graph; `go build ./...` / `go vet` clean in gateway and server.
- [x] 4.2 `go test ./...` green in `apps/web-ui/gateway`; DB-backed tests green in `apps/server/domain/agents/...` and `apps/server/domain/graph/...` (real Postgres).
- [x] 4.3 Pattern-consistency ratchet green (no new drift tokens in `board.templ`).
- [x] 4.4 `openspec validate board-assignee-status`.
- [ ] 4.5 Server restart + browser pass on dev (assignee picker + set-status control).

## 5. Follow-ups (not in this branch)

- [ ] 5.1 Regenerate the committed server swagger artifacts — deferred to the existing docs-regeneration chore (the new route is annotated, but the committed `apps/server/docs/swagger/*` is already stale on `main`).
