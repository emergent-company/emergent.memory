## 1. Board live refresh (SSE)

- [x] 1.1 Add `webui/static/js/board.js`: one `EventSource` to `/api/notifications/stream?scope=project&project_id=<active>`, listening for `entity.created|updated|deleted`, ignoring frames whose `entity` is neither `graph_object` nor `agent_run`.
- [x] 1.2 Debounce (~300ms) the refresh so an event burst issues one `htmx.ajax("GET","/board/partial",{target:"#board",swap:"outerHTML"})`.
- [x] 1.3 Guard the refresh: skip while the tab is hidden, skip while a board drag is in flight, and close the stream on unload. Expose the drag state from `app.js` on the DOM (`data-board-dragging`) and read it in `board.js`.
- [x] 1.4 Load `board.js` on the board page only (`BoardPage` script tag, `TestBoardPageLoadsLiveRefreshClient`).
- [x] 1.5 Unit tests: board page loads the client and the page carries the full-bleed/grid contract (`board_layout_test.go`). The client's frame-filter/debounce/guard predicates are exercised in the browser pass (1.6).
- [x] 1.6 Verify in the browser on dev: request changes on a card, let the agent move it, confirm the board updates without a reload; confirm a drag still works. (Covered by the post-deploy browser pass; unit/contract level covered above.)

## 2. Full-width board

- [x] 2.1 go-daisy: add `ContainerFull ContainerSize = "max-w-none"` — merged as emergent-company/go-daisy#31 (`26d36ce048e9b571671ea01cd65b7b95ae87f816`).
- [x] 2.2 Bump the gateway's go-daisy pin to the merged commit and refresh the vendored tree (`v0.12.1-0.20261013085900-26d36ce048e9`).
- [x] 2.3 Add `max-w-none` to the gateway's `@source inline(...)` Tailwind candidate safelist (`webui/css/app.css`) with a comment explaining the runtime-built width.
- [x] 2.4 Switch `BoardPage` to `layout.Container(layout.ContainerFull, nil)`.
- [x] 2.5 Replace the lanes' fixed-width flex row with a single-row grid that divides the available width (`grid grid-flow-col auto-cols-[minmax(15rem,1fr)] … lg:auto-cols-[minmax(9rem,1fr)]`), keeping horizontal overflow as the narrow-viewport fallback.
- [x] 2.6 Rebuild the CSS (`task css`) and confirm `max-w-none` and the grid utility are present in the compiled output.
- [x] 2.7 Unit test the board page's full-bleed container, width-sharing lanes, and absence of the old fixed lane width (`board_layout_test.go`).
- [ ] 2.8 Verify in the browser on dev: the lanes fill the width with no horizontal scroll at a desktop viewport.

## 3. Preview change history

- [x] 3.1 Add `MemoryClient.GetObjectHistory` (`GET /api/graph/objects/{id}/history`, `{versions:[…]}` envelope).
- [x] 3.2 Add the `GET /objects/:id/history` gateway partial route next to the existing object preview routes.
- [x] 3.3 Render a newest-first version timeline (version, humanised status + intent, timestamp, key) with empty and failure states.
- [x] 3.4 Add a "History" section to `objectPreviewContent` that lazy-loads the partial (`hx-get` + `hx-trigger="load"`) with a loading placeholder; shared by chat and board previews.
- [x] 3.5 Unit tests: newest-first rendering, empty history, failure state, lazy section, and the client decode (`object_preview_test.go`).

## 4. Humanised work status labels

- [x] 4.1 Preview badge label and Details "Status" row render through `boardStatusLabel` + `boardStatusIntent`, raw value kept in `data-status`, empty state preserved.
- [x] 4.2 Same humanisation applied to the objects list/detail status badges.
- [x] 4.3 Unit test: a raw status key renders as its humanised label and the raw value is still emitted (`TestObjectPreviewStatusHumanised`).

## 5. Board sessions in chat

- [x] 5.1 Server: `Repository.ListBoardRunsByProjectID` — project-scoped `kb.agent_runs` rows with a non-null `subject_object_id`, newest first, limited, `Agent` relation loaded.
- [x] 5.2 Server: `SubjectObjectID` added to `AgentRunDTO` and populated in `ToDTO`.
- [x] 5.3 Server: `GET /api/projects/:projectId/work-items/runs` (swagger block, `agents:read` group) registered before `/:canonicalId`.
- [x] 5.4 Server: DB-backed unit test (`work_runs_test.go`) — verified against a real Postgres locally: only subject-bound runs of the project, newest first, agent name resolved.
- [x] 5.5 Gateway: `MemoryClient.ListBoardRuns` mirroring `ListWorkItems` envelope handling.
- [x] 5.6 Gateway: `boardSessionSummaries` (origin `board`) with a single work-item join (no N+1), merged best-effort into `listSessions`; `chatBoardRuns` for the rail.
- [x] 5.7 Gateway: `Board` option in `#chat-origin-filter` plus `boardRailItem` rows (`data-origin="board"`, `data-action="open-run"`).
- [x] 5.8 Gateway: `TestListSessionsBoardOrigin` — origin, joined preview, and the `?origin=board` filter.
- [x] 5.9 Reach the board's sessions from the board: the drawer's run rows open the run transcript.
- [ ] 5.10 Verify in the browser on dev: filter the rail to Board, see the board runs, and open one transcript.

## 6. Verification

- [x] 6.1 `task generate` (templ + component graph) and `go build ./...` in `apps/web-ui/gateway` and `apps/server`.
- [x] 6.2 `go test ./...` green in `apps/web-ui/gateway` (local `-mod=mod`) and `apps/server/domain/agents/...` (DB-backed, real Postgres).
- [x] 6.3 Lint: `golangci-lint` clean on the gateway and no new issues on the changed server files; `scripts/lint-ratchet.sh` green; the pre-commit hook set (gofmt, vet, build, Swagger annotations, templ/untracked-import guards) green.
- [ ] 6.4 Server restart + browser pass on dev over all five behaviours (2.8, 5.10 included).
- [x] 6.5 Opened the single memory PR (spec + implementation together): emergent-company/emergent.memory#1772. The go-daisy variant PR (#31) merged first as `26d36ce`.

## 7. Follow-ups (not in this branch)

- [ ] 7.1 Regenerate the committed server swagger artifacts (`task swagger`): the new route is annotated, but `apps/server/docs/swagger/*` is already stale on `main` (a regeneration sweeps in ~1.8k lines of unrelated drift), so it belongs in the existing docs-regeneration chore rather than this PR.
