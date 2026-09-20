## Context

`kb.agent_definitions.visibility` is a `notnull` enum with a `project` default and three values — `external`, `project`, `internal`. It drives two distinct behaviours that this change makes explicit: **advertisement** (whether the agent appears in the A2A agent card / public discovery) and **invocability** (whether the agent can be reached by slug through the A2A `message:send` skill selector). The admin UI previously showed visibility as a read-only badge; the server's A2A resolver accepted any visibility on the slug fallback, so `internal` agents were still invocable. This change exposes the control in the admin UI and tightens the resolver so visibility is a real guarantee.

## Goals / Non-Goals

**Goals:**

- Let a user set an agent's visibility from the admin UI's General settings form.
- Make visibility's semantics legible in the UI (per-option description, warning for `external`, note for `internal`).
- Ensure `internal` agents can no longer be invoked over A2A, matching their "hidden/system" intent.

**Non-Goals:**

- No new database column or migration — visibility already exists.
- No change to access control: A2A invocability is still governed by API-token scopes (`agents:read`/`agents:write`), independent of visibility.
- No change to agent-to-agent (delegation) invocability — `internal` agents remain callable by *other agents*; only the external A2A surface rejects them.

## Decisions

### D1 — Dropdown with per-option `Name — description`

The Visibility control is a native `<select>` (daisyUI) with three options rendered as `Name — description`, per the user's request to show what each level means inline. A server-rendered helper line under the control repeats the selected option's description, and the option list is ordered `project`, `external`, `internal` with `project` leading as the default.

*Alternatives considered:* radio group or bare select with only names — rejected, the descriptions carry the "advertised vs not" distinction the user needs.

### D2 — Default `project`

The control defaults to `project` and the applier normalises an empty/missing value to `project` (mirroring the server column default). This matches existing behaviour where a definition with no explicit visibility is `project`, and makes the control safe for legacy forms that predate it.

### D3 — Server-side validation: empty→project, unknown rejected

`applyAgentGeneralSection` validates visibility through a pure `agentVisibilityNormalize` helper: empty/missing → `project`; one of `project`/`external`/`internal` → itself; anything else → rejected (the form re-renders with an error and the backend is never called). This keeps the gateway from forwarding a value the server would not understand.

### D4 — External warning + internal note

Selecting `external` renders a warning that the agent is listed in the project's A2A agent card and that anyone with a project API token can discover (`agents:read`) and call (`agents:write`) it. Selecting `internal` renders a note that the agent is hidden from the agents list, still reachable by direct link, and still callable by other agents. `project` renders neither. The copy is factual and never claims `project`/`internal` agents are "not callable" — they are not *advertised* but (for `project`) remain resolvable by slug.

### D5 — Resolver tightening: external preferred, project fallback, internal never

`resolveA2AAgentBySkillID` resolves a skill slug with visibility precedence: `external` first, then a fallback that accepts `project` but rejects `internal`. An `internal`-only match resolves as not-found (`errA2ASkillNotFound` → HTTP 400 `SKILL_NOT_FOUND`) and never falls back to the CLI assistant. The decision is extracted into the pure `a2aPickResolvableDefinition` helper so it is unit-testable without a live DB.

*Alternatives considered:* a DB-level visibility filter on the slug fallback query — rejected, the current repo exposes no slug+visibility filter, so a post-resolve visibility check is the minimal change and keeps the external-first query untouched.

### D6 — Visibility = advertisement + invocability, access still scope-governed

Visibility's two facets are documented as such: it controls **advertisement** (agent card / discovery) and **invocability over A2A** (skill resolution). It does **not** replace authentication/authorisation — a caller still needs a valid project token with the right scope, and an `external` agent is still only reachable to callers holding that project's token.

## Risks / Trade-offs

- [Skill resolver unit coverage] → the DB-dependent `resolveA2AAgentBySkillID` path can't be exercised without a live DB, so the visibility decision is isolated in the pure `a2aPickResolvableDefinition` helper and unit-tested there; the full `internal`-slug → 400 wire path is integration coverage.
- [UI copy vs resolver semantics] → the UI `internal` note says "other agents can still call it", which refers to agent-to-agent delegation (unchanged), not A2A; the resolver rejects only the A2A surface.
- [No migration] → relies on the existing `visibility` column and its `project` default; nothing to backfill.

## Migration Plan

Additive and comment-only on the server; UI additive. No data migration. Rollback = revert the resolver change and the UI control; the field already exists and defaults to `project`.

## Open Questions

None.
