## Context

The gateway is an HTTP client to an external Memory REST service; it holds no project/org data itself (see `gateway/memory.go`, `MemoryBackend` in `gateway/backend.go:11-155`). Project ownership is a single `orgId` field on the project record (`Project.OrgID`, `gateway/settings.go`), mirrored as `OrgID` on the org-landing `ProjectRef` DTO (`gateway/memory.go:2433-2445`). There is currently no backend operation or gateway method that reparents a project; `UpdateProject` covers name/info fields only. The org view lives in `OrgLandingPage` (`gateway/org_context.templ:15-166`) with a per-row popover menu holding Open and Delete (pattern at `org_context.templ:125-155`), served by `uiOrg` and mutated by `uiDeleteProjects` (`gateway/org_context.go`), registered in `gateway/main.go`. Role enforcement is delegated to the Memory service; the UI only reflects role (see `org-context-and-project-picker/design.md`). See `proposal.md - Why` for motivation.

## Goals / Non-Goals

**Goals:**
- Add a Transfer row action + destination-picker dialog to the organization view.
- Add a gateway transfer route, handler, and `MemoryClient`/`MemoryBackend` method that follow the existing delete-project flow shapes exactly (form+query id handling, HTMX/PRG dual path, flash redirect back to source org).
- Keep UI role-aware (org_admin of source org only) while the backend stays authoritative.
- Make everything testable at the gateway layer with a fake `MemoryBackend` (TDD; deterministic, no timing/environment dependence).

**Non-Goals:**
- Backend service implementation itself (external repo/service); we define the REST contract it must satisfy.
- Bulk transfer, org rename/merge, transferring org ownership, project recreation/merge of data.
- Deciding backend membership/role consequences after reparent — the service owns that policy.

## Decisions

1. **Interaction: extend the row popover menu + a modal destination picker.**
   Add "Transfer" as a third item in the per-project `popover` menu (`org_context.templ:125-155`). Choosing it opens a `modalShell` dialog (reuse `gateway/ui.templ:637-665`) containing a destination-org `<select>` built from the acting user's organizations minus the source org, plus a Confirm/Cancel footer. Rationale: matches all existing surfaces (row menu, modal shell, `<select>` org picker in `newProjectModal`, `auth_ui.templ:345-356`); native `hx-confirm` is insufficient because the destination choice requires a form. Alternative (inline row form) rejected: inconsistent with the org view's menu-triggered actions and clutters the table.

2. **Route shape mirrors project delete.**
   Register `POST /projects/transfer` in `main.go` next to `/projects/delete` (`main.go:160-162`). Form fields: `projectId`, `orgId` (source org, used for redirect back, read from form or query like `uiDeleteProjects`), `destinationOrgId`. Handler `uiTransferProject` mirrors `uiDeleteProjects` (`org_context.go:183-215`): HTMX single-row submit via `render.RedirectAfterMutation` with `?moved=N` flash, non-HTMX PRG fallback redirect. Rationale: identical conventions reduce review cost. Alternative `POST /orgs/:id/transfer` rejected: project delete already encodes org context as a param, not a path segment.

3. **Client contract: dedicated `TransferProject` operation.**
   Add `TransferProject(ctx, projectID, destinationOrgID) error` to `MemoryBackend` and implement on `MemoryClient` as `POST /api/projects/{id}/transfer` with a JSON body `{"orgId": destinationOrgID}` (mirroring `DeleteProject`, `memory.go:2533-2536`, and `do` error mapping). Do not fold this into `UpdateProject`: reparenting is an org-scope authorization decision, not a settings patch, and a distinct endpoint keeps the service's permission checks unambiguous. The transfer endpoint must exist on the Memory service (see Risks and Migration Plan).

4. **Candidate destinations and visibility come from the orgs-and-projects access tree.**
   The org-landing page data gains the acting user's organizations with their roles (the same access-tree fetch that backs the orgs/members pages, `OrgWithProjectsDto`, `memory.go:2498-2503`). A user sees Transfer only when their role in the current org is `org_admin` and ≥1 candidate destination exists (their orgs minus the source org). Destination `<option>`s are that same filtered set. Rationale: no new backend call and one consistent source of membership truth. Alternative (server-side transfer-capability field on `ProjectRef`) rejected: duplicates role knowledge the client already has and lags real authorization.

5. **Local guard before the backend call.**
   Handler rejects `destinationOrgId == orgId` (and missing/unknown destination) with an error flash and no request, per spec. Everything else is delegated: backend errors map to the standard error flash flow (`redirectWithError`/`flashError` in `ui.go`) with a friendly message, since the service is authoritative on permission and project-not-found.

6. **Testability: fake `MemoryBackend` in unit tests.**
   Handler tests assert: success path redirect target + flash + the exact `TransferProject` call args; local guard short-circuits (backend spy records no call for source-as-destination); backend error paths map to error flashes. UI-level gating (menu item + dialog options) is tested where the view data is assembled. All deterministic, no real service required.

## Risks / Trade-offs

- **[Transfer endpoint missing on the Memory service]** → The gateway ships a contract-defined client call; until the service implements it, transfers fail with the surfaced backend error. Mitigation: the REST contract is fixed here (Decision 3) so service-side work is unambiguous; fake-backend tests keep the gateway correct regardless.
- **[UI role data lags the backend's real authorization]** → UI hides Transfer for non-`org_admin`; if the service's policy is stricter (e.g., destination requires `org_admin`, not mere membership), rejections surface as clear errors. Mitigation: dialog lists only orgs the user belongs to; error copy tells the user the transfer was refused.
- **[Reparenting disrupts active sessions/context]** → A project moved out of the org a user is currently operating in changes what `X-Org-ID`-scoped requests resolve to. Mitigation: transfer is org-admin initiated and confirm-modal described; scope consequences are backend-owned and out of scope here.
- **[Naming/policy collisions in destination org]** → If the service requires unique project names per org or blocks certain destinations, that validation lives server-side. Mitigation: surface the backend error verbatim in the flash; no client-side guessing.

## Migration Plan

1. Land gateway change: interface + client method, route, handler, view wiring, unit tests. Independently deployable: without a service endpoint, transfer attempts fail loudly but nothing else regresses.
2. Memory service adds `POST /api/projects/{id}/transfer` implementing the contract (separate change; authorize: requester org_admin of the project's current org and member of destination; reparent `orgId`; apply membership consequences).
3. No rollback concern for existing data: feature is additive and no-op until the endpoint exists.

## Open Questions

None that change specs, approach, or task breakdown. The only external dependency (the service transfer endpoint) is a deployment sequencing concern covered under Risks and Migration Plan, not a design unknown.
