# Tasks — object-comments

## 1. Schema + store

- [x] 1.1 `00241_object_comments.sql`: create `kb.object_comments` (id, project_id, canonical_id, author, body, actor_type, run_id, created_at) + (project_id, canonical_id, created_at) index.
- [x] 1.2 Register `(*agents.WorkItemComment)(nil)` in `internal/schemadrift/models.go`.
- [x] 1.3 `work_item_comments.go`: `WorkItemComment` entity + `AppendWorkComment` + `ListWorkComments` (ascending created_at).

## 2. Server write path

- [x] 2.1 `handler_work.go`: `WorkItemCommentDTO` + `AddWorkItemCommentDTO`; add `Comments` to `WorkItemDetailDTO`; populate in `GetWorkItem`.
- [x] 2.2 `work_actions.go`: `AddComment` (resolve HEAD → 404, 400 on empty body, append with actor_type='user' + author) + `WorkItemDetail.Comments`/`GetWorkItemDetail`.
- [x] 2.3 `routes.go`: `workItemsWrite.POST("/:canonicalId/comments", h.AddWorkItemComment)`.
- [x] 2.4 Swagger `@Router` annotation on `AddWorkItemComment`.

## 3. Agent tool

- [x] 3.1 `toolpool.go`: `ToolNameWorkComment = "comment"`.
- [x] 3.2 `work_tools.go`: `BuildWorkCommentTool` + extracted `commentWork` (actor_type='agent', author=ActorName, run id; non-finalizing; empty body rejected).
- [x] 3.3 `executor.go`: `buildWorkTools` sets deps ActorName/RunID and builds the comment tool when whitelisted.

## 4. Web UI

- [x] 4.1 `memory_work.go` + `backend.go`: `WorkItemComment`, `Comments`, `AddWorkComment`.
- [x] 4.2 `board.go`: `boardComment` + `boardComments` merge helper (comments + feedback, ascending).
- [x] 4.3 `board.templ`: merged "Activity" thread (`boardCommentsView` → `boardCommentsThread`) + inline `boardCommentForm` (empty state + `data-testid`).
- [x] 4.4 `main.go`: `POST /board/items/:canonicalId/comments` → `uiBoardComment`.
- [x] 4.5 `object_preview.go` + `board.templ`: `?actions=board` renders `boardPreviewBoardSlot` — the status-gated actions followed by the merged thread as an `objectPreviewSection("Activity")` — so the thread is discoverable from the card-click preview, not only the drag→review dialog.
- [x] 4.6 Surface-scoped test ids: `board-drawer-comment-*` (native drawer) vs `board-preview-comment-*` (preview slot), since both surfaces can coexist in one DOM.
- [x] 4.7 Copy: heading `Activity`, feedback badge `rework round N`, form placeholder `Add a comment…`, empty state `No activity yet`.
- [x] 4.8 `board.go`: deterministic same-second tie-break in `boardComments` (kind, round, author, body) instead of a stable sort on second-precision `created_at`.

## 5. Tests

- [x] 5.1 Server: store append/list ordering + scoping; `AddComment` author/actor_type + empty-rejected + missing-404; detail projection carries comments; `commentWork` agent attribution + guards; `BuildWorkCommentTool` constructor.
- [x] 5.2 Gateway: `boardComments` merge order; drawer thread render (badges, ascending order, form route); empty state; comment form POST calls `AddWorkComment`.
- [x] 5.3 Gateway: same-second tie-break total order; drawer vs preview test-id scoping; the preview board slot renders the thread.
- [x] 5.4 Server: cross-project scoping — an object with the same canonical id in another project sees no comments.
- [x] 5.5 e2e `board-comments-ui.spec.ts`: preview thread lifecycle (empty → post → merged feedback/comment) with the order derived from the API's own `createdAt` values (no wall-clock wait), plus both-surface test-id scoping. Not executed: the `mutations` Playwright project is absent from CI's matrix.

## 6. Verify

- [x] 6.1 `go build ./...` (server + gateway).
- [x] 6.2 `go vet ./...`.
- [x] 6.3 `go test -short -count=1 ./...` (unit).
- [x] 6.4 `REQUIRE_DB=1 ... go test -count=1 ./domain/agents/...` (DB-backed).
- [x] 6.5 `templ generate` + `task css` (gateway).
- [x] 6.6 Route-authority guard `-update` + check (0 violations).
- [x] 6.7 Migration preflights (number + immutability).
- [x] 6.8 `bash scripts/lint-ratchet.sh` (server) + gateway consistency ratchet.
- [x] 6.9 `openspec validate object-comments`.
