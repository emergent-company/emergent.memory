## Context

All app pages render through `(*Server).page(c, title, content)` (gateway/ui.go:110), which passes `title` straight to `appShell(title, …)` (gateway/ui.templ:25) → `<title>{ title }</title>`. There is no shared title helper: ~40 handler call sites in ~10 files build titles as string literals `"<label> — Memory"` (e.g. `s.page(c, "Agents — Memory", …)`). The login page (gateway/auth_ui.templ:28) hardcodes `<title>Sign in — Memory</title>` independently. The brand appears as the literal `"Memory"` in the title, `apple-mobile-web-app-title`, the web manifest, and `layout.Navbar("Memory", …)`. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- One helper owns title composition; no handler concatenates the brand or separator.
- Segment ordering (entity → section → brand) is uniform.
- Blank/empty entity segments degrade to a stable label, never a bare separator.
- Brand is a single constant reused across title + PWA meta + navbar.

**Non-Goals:**
- No change to in-page headings (`pageHeader`, `detailHeader`, `chatTitle`, `sessionTitle`) — those are display labels, not the `<title>`.
- No change to `apple-mobile-web-app-title` value or manifest contents, only their source of truth.
- No renaming of the product; brand stays "Memory".

## Decisions

**D1 — Helper shape: variadic `pageTitle(segments ...string) string`.**
Each segment is an independent, ordered piece; the brand is appended inside the function. Rationale: variadic keeps call sites terse and expresses order naturally, vs. alternatives:
- `pageTitle(a, b)` fixed-arity → awkward for 1-vs-2-segment pages.
- `pageTitle(joined string)` (caller pre-joins) → pushes separator/brand logic back to callers, defeating the goal. Rejected.

**D2 — Separator and brand: em dash + trailing brand, hardcoded in the helper.**
`parts = append(filter(segments), appBrand); strings.Join(parts, " — ")`. Rationale: matches the existing `"X — Memory"` convention already used across ~40 sites, so the change is behavior-preserving (no visible title churn). Alternatives: `" · "` (middle-dot) or `" | "` were considered but would silently change every tab title; em dash is the established format.

**D3 — Blank filtering + fallback label live in the helper.**
The helper trims and drops empty segments; if all page segments are empty it still appends the brand, but each call site supplies a concrete generic fallback (e.g. `"Agent"`, `"Object"`, `"Document"`) for load-error paths where the entity name is unavailable. Rationale: keeps the "what is the generic noun for this page" knowledge at the call site (it knows the resource type), while the helper guarantees the empty case never emits `" — Memory"` or `" —  — Memory"`.

**D4 — Section becomes a segment, not a concatenated string.**
`"Ada settings"` → `pageTitle(agent.Name, "Settings")`. Rationale: fixes today's inconsistent `"Ada settings"` vs `"Ada memories"` (no separator) into the uniform `"Ada — Settings — Memory"` shape described in the spec.

**D5 — Brand constant `appBrand = "Memory"`.**
A single package-level const reused by `pageTitle`, the `apple-mobile-web-app-title` content, and the navbar brand label. Rationale: a future rename is a one-line change and cannot drift between title and PWA name.

## Risks / Trade-offs

- [Missed call site → title silently diverges] → Mitigation: grep for the literal `" — Memory"` after migration and assert zero remaining; `go build` fails on any leftover referencing removed literals only if they were replaced, so rely on a grep + unit-test sweep.
- [Em-dash string in Go source vs. HTML entity] → Mitigation: store the raw `"—"` rune in a const and set `<title>` via templ string interpolation (already done today), avoiding `&mdash;` double-encoding.
- [Fallback label subjectivity] → Mitigation: fallback is always the resource noun ("Agent", "Object", …), matching the existing error-state titles (`"Agent — Memory"`, `"Object — Memory"`), so no new wording is invented.
- [Login page drift] → Mitigation: login page routes the same `pageTitle` helper rather than duplicating the format.

## Migration Plan

1. Add `appBrand` const + `pageTitle` helper + unit tests (gateway/ui.go, ui_test.go).
2. Replace each `s.page(c, "<label> — Memory", …)` call with `s.page(c, pageTitle(…), …)`; for detail pages pass entity + optional section as segments.
3. Wire login title through `pageTitle`; point `apple-mobile-web-app-title` and navbar brand at `appBrand`.
4. `templ generate` + `go build ./...` + `go test ./...`; grep to confirm no `" — Memory"` literals remain outside the helper.
5. Rollback: revert the single commit; no data or API surface is affected.
