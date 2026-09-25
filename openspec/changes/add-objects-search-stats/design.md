## Context

The objects browser (`GET /objects`, rendered by `uiObjects` in `objects.go` + `ObjectsPage` in `objects.templ`) previously listed objects via `ListGraphObjects` and showed a page-scoped "loaded objects" badge. The page needed search and project health, and the flat list did not scale past a single page.

The backing `domain/graph` endpoints already exist and are membership-protected (the `RequireProjectTokenScope` → `RequireProjectMember` pair from #912). This PR is gateway-only: it consumes those endpoints, so the design's job is the UI data-flow and the pagination/error contracts.

## Decisions

- **Cursor pagination reuses the existing keyset.** The gateway calls `GET /api/graph/objects/search` with `cursor`, `limit`, and `include_total=false`. The server's list path already keysets on `(created_at, id)` DESC with the `id` tiebreak and returns `next_cursor` only when `hasMore`; `include_total=false` skips the exact `COUNT(*)` that is the endpoint's latency floor on large projects (#733). The gateway adds no pagination logic of its own — it forwards the cursor verbatim and renders `HasMore = nextCursor != ""`.
- **Search has no cursor pagination.** The search form requests a single top-25 page (`SearchObjects(..., limit 25, offset 0)`); `uiObjectsPartial` short-circuits search mode to an empty fragment because search is capped at 25. Backing FTS/hybrid endpoints clamp `limit` (default 20, hard cap 200 server-side); the gateway's 25 sits within that bound.
- **Parallel best-effort fetches.** Branches, compiled types, the object page, the count, and the embedding progress are fetched with `errgroup` so a slow or failed stat never blocks the object rows. Stats errors are preserved in `objectsStats.TotalErr`/`EmbedErr` and rendered as `—` with `title="Unavailable"` rather than a misleading `0`.
- **Errors are surfaced, not swallowed.** A browse load failure sets `objectsPageData.LoadErr` and renders the page error state; a load-more failure returns HTTP 502 so HTMX keeps the "Load more" button for retry.
- **Authorization is a stated contract, not a new mechanism.** The gateway already scopes requests via `resolveDefaultProject` (picks only the user's own `ListProjects`, mirrors into the session context) and `projectIDFor`/`documentHeaders` forwards `X-Project-ID`. The spec records this plus the backing endpoints' membership enforcement so a future change cannot silently drop tenant isolation.

## Risks / mitigations

- **Reliance on pre-existing endpoint protection.** The membership enforcement on `/api/graph` lives in the server (`#912`), not this PR. Mitigation: the authorization posture is written as an explicit SHALL requirement so removing it is a visible spec change.
- **Search-mode filter drift.** The search form and filter form carry `q`/`mode`/`type`/`branch` as hidden inputs so the other context is not dropped on submit. Covered by the search/filter scenarios and the partial-URL builder carrying all context forward.
