## Context

Memory derives a request's effective scopes from several planes, and two of them can each mint a fine-grained Memory grant:

| Plane | Where | Authority today |
|---|---|---|
| IdP token scopes | `filterMemoryScopes(rawScopes)` — `apps/server/pkg/auth/scope_mapping.go:79` | **Memory scope names on an OIDC token are honoured verbatim** and win over everything else (`oidc-scope-mapping` spec: "Explicit Memory scopes are authoritative") |
| App role map | `roleToScopes` — `scope_mapping.go:67`, from `kb.project_memberships` | viewer / user / admin (#803) |
| App default set | `ZITADEL_OIDC_DEFAULT_SCOPES` — `internal/config/config.go:203` | app policy, IdP-named |
| App all-grant | `ZITADEL_USERINFO_GRANT_ALL_SCOPES`, default `true` — `config.go:211`, gated by `auth_source == userinfo && oidcAllGrantEnabled()` — `middleware.go:638` | app policy, IdP-named, permissive |
| Machine grants | `core.api_tokens.scopes` + `RequireAPITokenScopes` — `middleware.go:431` | app-owned, a **separate authentication plane** (`validateAPIToken` returns before OIDC resolution, `middleware.go:567`) |
| Coarse platform flag | `core.superadmins` (`revoked_at IS NULL`) — `domain/apitoken/repository.go:274` | app-owned, but **not a scope source today** — read only by `CanGrantAdminAll`; it never feeds `AuthUser.Scopes` |
| Org membership | `kb.organization_memberships` | app-owned, but **not a scope source today** — read only by `CanGrantAdminAll` (`repository.go:276`) |
| Scope catalogue | `GetAllScopes()` — `middleware.go:1041`; `memoryScopeVocabulary` — `scope_mapping.go:34` | app-owned |

The duplication is the first row: the IdP can mint fine-grained grants. Everything below it was the app trying to compensate — a role map, a default set, and an all-grant escape hatch.

Two facts shape the target state, and both must be stated rather than implied:

1. **`core.superadmins` and `kb.organization_memberships` are not scope tiers today.** They gate exactly one decision (`admin:all` token minting). This design *introduces* app-side entitlement tiers into scope resolution — that is new authorization, not the "rounding out" of an existing tier.
2. **The permissive all-grant currently hides this gap.** While it is on, every userinfo-authenticated user gets the full catalogue, so superadmins and org admins never needed their own tier. Removing the all-grant is only safe *because* D4 defines those tiers.

The design is bounded by decisions already made elsewhere and **not reopened here**: the umbrella expansion semantics (`schema:write ⇒ schema:migrate`, `agents:write ⇒ chat:admin`, `data:write ⇒ related writes + journal:write`) and the nested role sets were decided and pinned by tests in #803; the `project_admin/project_user` scope lists are settled.

## Goals / Non-Goals

**Goals**

- State and specify one authority: **Zitadel authenticates; the app authorizes.**
- Make the IdP token-scope trust path **opt-in**, so the default posture has a single authority.
- Define the app-side entitlement tiers that replace the all-grant: superadmin and `org_admin`, with **explicit** scope grants.
- Give the app-owned knobs app-owned names, with a one-release deprecated alias for each.
- Make the load-bearing permissive default impossible to run unnoticed, then removable.
- Reconcile `org_admin` vs `owner` for `kb.organization_memberships`.
- State **exactly** who gains or loses what, when, and the sequenced, reversible grace path.

**Non-Goals**

- No implementation code, schema, or API surface in this change.
- Not per-project scope *isolation*. Scopes remain global per request; this is about who is *allowed to define* them.
- No change to umbrella expansion, to the role scope sets from #803, or to `GetAllScopes()`.

## Target principle

**Zitadel authenticates; the app authorizes. There is no third authority.**

- Zitadel's job is identity: prove the subject (`sub`), and optionally deliver one coarse, app-defined signal.
- The app is the sole source of fine-grained grants: `core.api_tokens.scopes`, `core.superadmins`, `kb.organization_memberships`, `kb.project_memberships`, and the app-owned default set.
- If an operator wants a Zitadel-side coarse signal at all — e.g. to bootstrap the **first** superadmin over SSO before any app-side row exists — it is exactly **one** claim/role mapping to **one** app-side superadmin grant. It is never a list of fine-grained scopes.
- A Memory scope name carried on an OIDC token is, by default, **not** a grant. It becomes one only when an operator explicitly opts in for a transition window.

## Decisions

### D1 — Token-carried Memory scopes are opt-in

`filterMemoryScopes` is correct as a *filter* (it keeps only names in `memoryScopeVocabulary`), but as a *grant source* it is the duplication. Put the trust path behind an app-owned flag:

- `MEMORY_OIDC_TRUST_TOKEN_SCOPES` (bool).
- When enabled, current behaviour: a vocabulary-matching token scope is used verbatim and **terminally** (first match, no union with app-derived scopes).
- When disabled, `resolveOIDCScopes` **skips the token branch entirely** and proceeds to app-derived entitlement tiers → app-owned default → empty.
- The flag is checked at the single resolution point (`resolveOIDCScopes`), so there is exactly one code path to reason about.

This is the change that removes the duplication: with the flag off, an OIDC token cannot mint a fine-grained Memory grant. Its **final** default is `false`; it is **introduced** defaulting `true` so Release N changes no behaviour (see D2 and "Rollout").

### D2 — App-owned configuration vocabulary

The knobs are Memory policy; their names should say so.

| App-owned (new canonical) | Deprecated alias (one release) | Current default | Introduced default | Target default |
|---|---|---|---|---|
| `MEMORY_OIDC_DEFAULT_SCOPES` | `ZITADEL_OIDC_DEFAULT_SCOPES` | empty | empty | empty |
| `MEMORY_USERINFO_GRANT_ALL_SCOPES` | `ZITADEL_USERINFO_GRANT_ALL_SCOPES` | `true` | `true` (unchanged) | removed |
| `MEMORY_OIDC_TRUST_TOKEN_SCOPES` | (new flag, no alias) | n/a | `true` (no behaviour change) | `false`, then removed |

Deprecation mechanics: if a deprecated name is set and the canonical name is not, the app uses the deprecated value and logs a **startup warning** naming both. If both are set, the canonical wins and the alias is ignored with a warning. The alias is removed in the release after next, following the repo's existing "deprecate the old name as an alias for one release" convention.

### D3 — The permissive default is loud, then removed

`UserinfoGrantAllScopes` defaults `true` and short-circuits to `GetAllScopes()` for the userinfo path whenever introspection is unconfigured (`middleware.go:638`). This is a pilot posture, and it should be impossible to run in production by accident.

- **While `UserinfoGrantAllScopes && !introspectionConfigured`**: emit a startup `WARN` ("scope authority: userinfo all-grant enabled; every authenticated user receives the full scope catalogue — configure introspection or disable `MEMORY_USERINFO_GRANT_ALL_SCOPES`"), and expose a health field (D7).
- **Then remove**: once introspection is the norm, delete the knob, the `authSourceUserinfo` all-grant branch, and its health field. After removal the userinfo fallback uses the standard fail-closed resolution like every other path.

Sequencing: signal → measure (`introspection_configured` in the health field tells the operator how close they are) → delete. D4 must land before D3's removal, because the all-grant is what users currently rely on for org/superadmin access.

### D4 — Entitlement tiers (introduced, not existing)

Today exactly one check reads superadmin/org membership (`apitoken.CanGrantAdminAll`), and **neither feeds session scopes**. This design defines them as app-side scope tiers, consulted at the single OIDC resolution point:

```
OIDC session scope resolution (app-owned, first match for the project tier):

  0. Token-carried Memory scopes   only while MEMORY_OIDC_TRUST_TOKEN_SCOPES is on   (terminal)
  1. superadmin                    core.superadmins, revoked_at IS NULL              (terminal: full catalogue)
  2. org_admin                     kb.organization_memberships for the request org   (org-administration set)
  3. project membership            kb.project_memberships for X-Project-ID           (role scope set, #803)
  4. app-owned default set         MEMORY_OIDC_DEFAULT_SCOPES
  5. empty                         fail closed
```

Tier semantics — specified explicitly, because the difference is a security decision:

- **superadmin (tier 1)** is terminal and grants the full scope catalogue. This is the post-all-grant replacement for platform operators; it is a deliberate grant, not a subset of any prior path.
- **org_admin (tier 2)** grants the **org-administration scope set**: `org:read`, `org:invite:create`, `org:project:create`, `org:project:delete`. It SHALL contain **no `project:*` scope** and no data, schema, or agent scope. The exact list is operator-confirmable (open question 2).
- **project membership (tier 3)** grants the nested role set from #803 for the declared project only.
- **Tier 2 and tier 3 are disjoint resource families and are combined** (a user can be org admin and project member): the session's app-derived scopes are the union of the org-administration set and the project-role set. Because the org set carries no `project:*` scope, the union cannot add a project scope a pure project member would not have. There is **no union** between tier 0 (token scopes) and any app-derived tier, nor between tier 4 and any entitlement tier.
- Tier 1 short-circuits: no union is needed.

**The request organization must be resolved.** `resolveOIDCScopes(ctx, userID, projectID, rawScopes)` (`scope_mapping.go:104`) has no organization input today, and tier 2 needs one. The request's organization is the owning organization of the project declared by `X-Project-ID`, or the standalone organization when no project is declared. This is a new parameter on the resolution seam and a new query (`kb.organization_memberships` scoped to that organization); it is called out as a task and covered by an org-isolation scenario (org admin in A is not org admin in B).

**The decision check is narrower than the scope tier list.** `apitoken.CanGrantAdminAll` today is `superadmin OR org_admin-in-any-org` — **no project tier**. The shared decision check (task 4.2) SHALL therefore be `superadmin OR org_admin` only: adding the project tier there would let a bare `project_admin` mint `admin:all`, a widening the security analysis does not sanction. It also SHALL preserve the existing **any-org** semantics of `org_admin` eligibility, so replacing the bespoke query does not silently narrow cross-organization minting. The request-org scoping applies to the *scope grant* (tier 2), not to the decision check.

A Zitadel coarse signal, if an operator configures one, may **bootstrap tier 1 only**. It must not populate tier 2 or 3.

This is specified, not implemented here. Making the broad set of org-scoped decision points *consume* the check (rather than each inventing a query) is a wider refactor, deferred to a follow-up change — see tasks §4.

### D5 — Vocabulary reconciliation: `org_admin` is authoritative

The two vocabularies disagree:

- **Writes `org_admin`**: `domain/orgs/repository.go:153` (org creation), `domain/invites/service.go:175` (accepted-role validation; `:293` is only a display label in `roleLabelFor`).
- **Checks `org_admin`**: `domain/apitoken/repository.go:276` — the only org-membership authorization check.
- **Writes `owner`**: `domain/standalone/bootstrap.go:143` (standalone bootstrap org membership).
- **Claims `owner` is valid**: `openspec/specs/project-viewer-role/spec.md:10`, migration 00165's comment, and the archived `oidc-scope-mapping` proposal.

`org_admin` is authoritative — it is what the only consumer checks and what every other writer writes. The `owner` claim is stale: it describes a role no other code path uses.

**Correct framing of the risk.** The obvious story ("standalone org creators hit a latent 403") is mostly cosmetic: standalone requests are authenticated by `checkStandaloneAPIKey` (`middleware.go:826`), which returns `Scopes: GetAllScopes()` regardless of membership, so standalone users already hold full access and do not need `CanGrantAdminAll` to succeed. The **real** D5 risk is the opposite direction and is exclusive to **non-standalone** `owner` rows (if any exist): normalising them to `org_admin` makes them newly eligible to mint `admin:all` tokens where they previously failed. That is a deliberate widening and must be reviewed against real row counts (open question 4) before the migration lands.

Reconciliation:

- `org_admin` is the canonical `kb.organization_memberships` role; `owner` is not written by any path.
- `standalone/bootstrap.go` writes `org_admin` (implementation lane).
- A Goose migration normalises existing `kb.organization_memberships` rows with `role='owner'` to `org_admin` — the pattern of migration 00165, for the organization table.
- The stale spec/comment claims are corrected.

Note: the legacy baseline RLS policies in `migrations/00001_baseline.sql:3858,3882` reference `project_memberships.role IN ('owner','admin')` — a project-scoped legacy DB default, out of scope for this app-level reconciliation, recorded so it is not mistaken for authority.

### D6 — Target resolution order (fail closed)

Composed from D1–D4; the honest statement is that there are **two mutually exclusive authentication planes**, not one linear list:

- **API-token plane**: an `emt_*` token's scopes are the effective scopes (`validateAPIToken` returns before OIDC resolution). Unchanged.
- **OIDC plane**: the ordered tiers in D4. First match for the project tier; token scopes terminal when trusted; fail closed to empty when no tier grants.

The IdP token branch is present **only** while `MEMORY_OIDC_TRUST_TOKEN_SCOPES` is on. In the target state it is absent, so the first tier a browser/OIDC caller can reach is the app's own entitlement resolution.

`org_admin` never widens a *project* role's project scopes: the org-administration set carries no `project:*` scope, so it grants org-family scopes only.

### D7 — Health and observability

The existing health `Check` type is `{Status, Message}` (`domain/health/handler.go:99-103`) and cannot carry structured booleans. Add a **dedicated** top-level `scope_authority` object to the health response (alongside `Tracing`, `handler.go:79`) with at least:

- `token_scopes_trusted` — whether `MEMORY_OIDC_TRUST_TOKEN_SCOPES` is on (should be `false` in the target state);
- `permissive_all_grant` — whether the userinfo all-grant is active (D3);
- `introspection_configured` — the precondition that lets an operator turn the all-grant off.

This makes the transitional and permissive states visible to monitoring instead of buried in config. Field names are pinned by the `scope-authority` spec.

## Rollout and breakage

**Who breaks when token scopes stop being honoured.** Anyone who configured Memory scope names in Zitadel and relies on them: IdP custom-scope grants, service clients whose OIDC tokens carry Memory scope names, and any SSO bootstrap flow that mints `data:write`/`schema:*` etc. on the token. They break **silently** in the worst case (scopes silently become empty → 403s), so the flag's default trajectory is sequenced rather than flipped:

**Release N (implementation lane):**
1. Add `MEMORY_OIDC_TRUST_TOKEN_SCOPES` **defaulting `true`** — behaviour identical to today, no break.
2. Add the app-owned names; keep `ZITADEL_*` aliases working; warn at startup when a deprecated name is used.
3. Add the D4 entitlement tiers (superadmin / org_admin) and the org-administration set.
4. Emit the loud startup warning + health field for the permissive all-grant (D3).
5. Log a one-time startup **deprecation warning** that token-carried Memory scopes will stop being honoured in N+1, naming the flag.
6. Land the vocabulary fix (D5) — safe after the row-count review, and it repairs the latent 403 for non-standalone `owner` rows.

**Release N+1:**
7. Flip `MEMORY_OIDC_TRUST_TOKEN_SCOPES` default to **`false`**. Operators who genuinely need it set it explicitly and get a warning each boot.
8. Remove the `ZITADEL_*` aliases.

**Release N+2:**
9. Remove the token-trust flag and the `filterMemoryScopes` grant path entirely; remove the all-grant knob and its branch (D3), assuming introspection is configured.

At every step the break is preventable by configuration, and the warning names the exact variable. An operator who wants a faster cutover can set the flag `false` in Release N. A per-org transitional setting was considered and rejected as over-engineered — see open question 3.

## Security analysis

**Precision over a blanket claim.** There is no blanket "nobody gains more than today" — two deliberate widenings exist. The correct statement is:

- **Scope-resolution path (token trust off): strictly narrower.** Removing the token branch can only drop a grant source; the app-derived tiers replace it. Relative to a *strict introspection* deployment (default empty), a superadmin or `org_admin` newly gains their tier — but that is the explicit entitlement model (D4), and the same principal could already obtain more via the userinfo all-grant whenever introspection was unconfigured. Relative to the **all-grant pilot posture**, every non-superadmin is strictly narrower: they lose the full catalogue.
- **No D4 tier exceeds the all-grant.** The all-grant hands the full catalogue to any userinfo-authenticated user; tier 1 is exactly `GetAllScopes()` and tier 2 is bounded strictly below it. This bound applies to the D4 *session-scope* tiers only — D5's `admin:all` **minting** entitlement is a separate grant and is not bounded by `GetAllScopes()` (the `admin:all` umbrella includes scopes outside the catalogue, `middleware.go:387-410`).
- **IdP-minting authority is removed.** With trust off, a compromised IdP client or a Zitadel misconfiguration can no longer mint a Memory grant — the security point of the change.
- **Bounded widenings, each reviewed separately:**
  - **D4 tier 1/2** — superadmin and `org_admin` gain session scopes they do not have on the strict introspection path. Required for the target state to function once the all-grant is removed; bounded by the explicit org-administration set (tier 2) and by `GetAllScopes()` (tier 1).
  - **D5** — normalising non-standalone `owner` rows to `org_admin` gives those principals `admin:all` minting rights they previously lacked. Gated on the open-question-4 row count.
- **The permissive default only narrows**: D3 moves "everyone gets everything" → fail-closed.
- **Failures fail closed, not open**: a removed alias leaves the default set empty (403), never a silent grant.

**Tightenings that could break a legitimate flow:**

- **IdP-configured scopes** — the intended break, sequenced and reversible.
- **`owner` org rows** — see D5; a real widening for that principal only.
- **Userinfo all-grant removal** — removes the pilot escape hatch. An operator still on userinfo + all-grant must configure introspection or an explicit default set before N+2; D3's warning window is the mitigation.
- **Default scope set renamed** — a deployment setting only `ZITADEL_OIDC_DEFAULT_SCOPES` keeps working through the alias for one release.

**Ordering invariant:** no step in the rollout may remove a grant path in the same release that removes its replacement. The token-trust flag stays default-on until the app-owned tiers are in place (Release N), and the all-grant is only removed after introspection is the documented norm.

## Knock-on: issue #736 item 3

#736 item 3 asks for live-Zitadel e2e coverage of real RFC 7662 introspection, the userinfo fallback with the all-grant on/off, and a role change taking effect on the next request. Once Zitadel is reduced to identity:

- the userinfo all-grant sub-case **disappears** (the knob is removed by D3);
- token-scope trust is a **flag** whose off-state is unit-tested, so only the on-state is e2e-worthy during the transition;
- introspection remains the one meaningful live path, so the fixture shrinks to "real introspection returns identity claims; Memory derives scopes itself" plus "role change takes effect next request" (already covered by the never-cached requirement).

Net: item 3 gets materially smaller and should be re-scoped against this shape rather than planned against today's two-authority model. The decision is the operator's; recorded here so it can be taken against the new shape.

## Open questions

1. **Default-off timing.** Is the Release N+1 flip of `MEMORY_OIDC_TRUST_TOKEN_SCOPES` to `false` acceptable, or should it stay default-on until a named future release? The design sequences it; the calendar is the operator's call.
2. **`org_admin` scope set.** The org-administration set is `org:read`, `org:invite:create`, `org:project:create`, `org:project:delete`. Confirm, or supply the exact list. `project:read` (singular) was considered and deliberately **excluded**: it is not implied by `projects:read`, so including it would let an `org_admin`+`project_viewer` hold a project scope a pure viewer lacks, contradicting the no-project-widening rule. If org-scoped screens need it, say so and it will be reclassified explicitly. Should `org_admin` grant `mcp:admin`?
3. **Per-org transitional setting.** A per-org opt-in for token-scope trust was considered and rejected as over-engineered. Confirm platform-wide only.
4. **`owner` migration blast radius.** How many production `kb.organization_memberships` rows are `owner`, and are any of them non-standalone? Those are the only principals who genuinely widen under D5; if the answer is "none", the migration is trivial.
5. **Health field visibility.** `scope_authority` is additive on the existing health response. Publicly readable, or gated behind the existing health auth? (Note the response's `Check` type cannot carry structured booleans — this adds a dedicated object.)
6. **Superadmin via Zitadel.** Reserve the one-claim→one-superadmin shape (as designed), or implement it now to bootstrap the first superadmin over SSO?
7. **All-grant removal gate.** Confirm "introspection configured" is the right precondition, or name a release target.
8. **Org-tier routing scope.** Confirm deferring the broad "every org-scoped decision consumes the tier" refactor to a follow-up change, keeping this one to the tier definition plus `CanGrantAdminAll` as its first consumer.

## Relationship to #803 (merged)

#803 (`fix/oidc-role-scope-sets`, PR #803) merged as `589923ff` before this change was finalised. Its role scope sets and the pinned "Bounded umbrella expansion of role scope sets" requirement are now in `openspec/specs/oidc-scope-mapping/spec.md`, and this change's deltas are authored against that post-#803 text (scenario names in the modified requirements are carried forward, not renamed). The two changes are orthogonal: #803 defines *what* project roles map to; this change defines *who is allowed to define* scopes. One sequencing note remains for implementation: landing the entitlement tiers must not alter #803's pinned expansion sets, and the exact-set tests from #803 remain the regression guard.
