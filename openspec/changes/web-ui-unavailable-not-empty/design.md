## Context

The core design question is whether to reuse the existing `pageError` degraded-state mechanism or introduce
a new error-surface idiom. The three broken sites already have a correct precedent in the same codebase
(`uiOrgs` sets `data.LoadErr`, `OrgsPage` renders `@pageError("Organizations unavailable", data.LoadErr)`),
so the change reuses it rather than inventing anything new. The only new piece is a full-page shell for the
landing route, which has no page to degrade to.

## D1 — The landing route gets a dedicated full-page unavailable state

**Decision.** `uiRoot` renders a new `RootUnavailablePage(err)` component through `s.page` (the app shell)
when `ListOrgs` fails, rather than redirecting anywhere. The component wraps the shared `pageError` with a
reassurance line ("Your data is safe — Memory is temporarily unavailable and will retry automatically.").

**Why.** `GET /` is a redirect-only route today — there is no existing page whose `LoadErr` it can set, and
the whole bug is that a failure *redirects* into the onboarding wizard. Rendering a page is the only way to
stop that redirect while still showing an explicit unavailable state inside the shell.

**Alternatives rejected.** (a) Redirect to `/orgs` with an `?err=` flash — still a redirect, and `/orgs`
itself re-fetches the org tree (which is down), so the flash is lost behind the same empty state. (b) Render
the branded `errorPage` (403/404/500 document) — that page is a standalone document with no shell/sidebar
and its copy is generic status copy, not the "unavailable, your data is safe" contract. (c) Reuse `OrgsPage`
with a `LoadErr` — heading would read "Organizations unavailable" and loses the "your data is safe"
reassurance that distinguishes this state from data loss.

## D2 — Failure and emptiness are split at the dispatch point, not in the template

**Decision.** Each handler distinguishes "request failed" from "request succeeded with zero results" before
choosing a render, and the templates keep their existing `LoadErr != nil → pageError` branch unchanged.

**Why.** The templates already have the correct branch (`OrgLandingPage`, `OrgsPage`); the bug is entirely
in the handlers discarding or conflating the error. Fixing it at the dispatch point keeps the template
contract intact and makes the "failure ≠ empty" property a handler-level invariant that is directly
unit-testable via the echo test server.

## D3 — The unavailable state is asserted by copy, not by class list

**Decision.** Unit tests assert the rendered body contains the explicit unavailable copy
("Service temporarily unavailable", "Your data is safe", "Organization unavailable") and does **not**
contain the empty/onboarding copy ("Create project", "Create the first project in this organization.",
"Create organization"), following the repo's existing `org_context_test.go` string-assertion style.

**Why.** The unavailable state's correctness is a copy-level contract (it must be distinguishable from data
loss), so the tests pin the user-facing strings rather than brittle class names. The shell's project
switcher always renders "No projects yet", so assertions target the empty-state CTA copy ("Create project")
that is unique to the onboarding screen.
