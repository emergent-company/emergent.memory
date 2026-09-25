## 1. Server — project-scoped progress

- [ ] 1.1 Confirm `GraphEmbeddingJobsService.StatsByProject` and `GraphRelationshipEmbeddingJobsService.StatsByProject` both exist and split genuine vs stale failures. (Both already exist; no change needed.)
- [ ] 1.2 Include relationship stats in `ProjectEmbeddingHandler.Progress` (populate `Relationships` in `ProjectEmbeddingProgressResponse`, typed `*GraphRelationshipEmbeddingQueueStats`). Verify: `go build ./...` compiles.
- [ ] 1.3 Drop the `requireProjectAdmin` check from `Progress` (reads require membership, enforced by route middleware); keep `requireProjectAdmin` on `Retrigger`/`Cancel`. Verify: handler still compiles.

## 2. Server — routes and membership

- [ ] 2.1 Apply `RequireProjectTokenScope` + `RequireProjectMember` to the `/api/projects/:projectId/embeddings` group and rename the route param `:id` → `:projectId` (URL unchanged); update handlers to read `c.Param("projectId")`. Verify: `go build ./...` compiles and server boots (`task status`).
- [ ] 2.2 Add/adjust a server test proving project scoping: jobs in project A and B → project A stats count only A's rows (object + relationship). Verify: `go test ./domain/extraction/...` passes (DB-backed tests run with the integration DB; skip otherwise).

## 3. Gateway — fetch project-scoped progress

- [ ] 3.1 Change `MemoryClient.GetEmbeddingProgress` to call `/api/projects/{projectID}/embeddings/progress` when `m.projectIDFor(ctx)` is non-empty, else fall back to `/api/embeddings/progress`. Verify: `go build ./...` from `apps/web-ui/gateway` compiles.
- [ ] 3.2 Update `TestGetEmbeddingProgress` to assert the project-scoped path + `X-Project-ID`, and add a fallback test for an empty project id. Verify: `go test ./...` passes.

## 4. Build, lint, and manual verification

- [ ] 4.1 Run `templ generate` (if templ changed), `go build ./...`, and `task lint` in both modules; fix until clean. Verify: all commands succeed.
- [ ] 4.2 Manual browser check: open `/embeddings` for a project with no objects; confirm counters are zero/empty (not another project's numbers), and confirm a project with jobs shows its own counts. Verify: page matches the active project.
- [ ] 4.3 Run `openspec validate` for the change. Verify: validation passes.
