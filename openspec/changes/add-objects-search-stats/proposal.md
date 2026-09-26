## Why

The objects browser (`GET /objects`) could only render one flat list of objects — no way to search for a specific entity, no project-wide health signal, and no stable pagination once a project outgrew a single page. This change adds text search (full-text and hybrid), a stats row (total objects plus embedding-queue pending/failed), and cursor-paginated browse, all backed by the already-membership-protected `domain/graph` endpoints.

## What Changes

- **Search** — a query input with a full-text vs hybrid segmented control. Full-text hits `GET /api/graph/objects/fts`; hybrid hits `POST /api/graph/search`, which auto-embeds the query and fuses lexical + vector results. Semantic/vector-only is intentionally not exposed (no text-input vector endpoint exists; hybrid covers semantic). Type and branch filters ride along as hidden inputs. Results are ranked and show a muted relevance score; search renders the top 25 only (no cursor pagination in search mode).
- **Stats** — a three-tile stats row shown in browse mode: total objects (`GET /api/graph/objects/count`, branch-scoped) and embedding-queue pending/failed (`GET /api/embeddings/progress`). A failed stats fetch renders `—` with `title="Unavailable"`, never a misleading `0`.
- **Cursor-paginated browse** — browse loads the 25 most-recent objects per page via `GET /api/graph/objects/search` with `include_total=false` (the exact `COUNT(*)` is skipped as the endpoint's latency floor, #733). Keyset cursor on `(created_at, id)` DESC with the unique `id` tiebreak; a reusable HTMX "Load more" button appends the next page and swaps itself via an out-of-band replacement. Search mode has no cursor pagination.
- **Authorization posture (unchanged by this PR, but now stated as a contract)** — the gateway derives the project server-side from the signed session (`resolveDefaultProject` picks only the user's own `ListProjects`; no client input overrides it) and forwards `X-Project-ID`. The backing `domain/graph` endpoints enforce `RequireProjectTokenScope` → `RequireProjectMember` (from #912): 401 with no auth, 403 for a non-member / foreign `X-Project-ID` / token-binding mismatch, 404 for an unknown project.

## Capabilities

### New Capabilities

<!-- None: the feature extends the existing objects-browser surface; no new capability is introduced. -->

### Modified Capabilities

- `object-browser`: the existing "List objects" requirement is modified to the cursor-paginated 25-per-page browse, and new requirements are added for search, stats, the cursor contract, and the session-derived project / membership authorization posture.

## Impact

- `apps/web-ui/gateway/objects.go` — `objectsStats` / `objectsPageData`; rewrite `uiObjects` (search vs browse, parallel stats fetch); add `uiObjectsPartial` load-more fragment, `objectsPartialURL`, `searchScoreLabel`.
- `apps/web-ui/gateway/objects.templ` — single-struct `ObjectsPage`; search form + segmented control, stats section, browse/search rows with muted relevance score, three empty states, HTMX load-more.
- `apps/web-ui/gateway/memory_graph.go` — `ListGraphObjectsPage`, `CountObjects`, `SearchObjects` client methods; `ObjectSearchResult`.
- `apps/web-ui/gateway/backend.go` — extend the `MemoryBackend` interface with the three methods.
- `apps/web-ui/gateway/main.go` — register `GET /objects/partial`.
- Tests updated/added across `objects_test.go`, `memory_test.go`, `handlers_test.go`, `embeddings_test.go`, `schema_test.go`, `page_width_consistency_test.go`.
- No server code changes: this is a gateway-only PR that consumes the existing membership-protected `domain/graph` endpoints.
