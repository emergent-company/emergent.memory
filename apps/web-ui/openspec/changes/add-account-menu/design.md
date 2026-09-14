## Context

Sessions are stateless and single-account: `sessionClaims` (gateway/session.go:17) is an HMAC-signed cookie holding one set of `{access_token, refresh_token, active_project_id, org_id, name, email, picture, exp}`. `ensureFreshSession` (gateway/auth.go:206) silently refreshes that one token, `setSessionCookie` writes it, and `authLogout` deletes it. The gateway has no local database — it is a thin layer over the Memory service. Identity currently renders in two places: the sidebar "Account → Profile" group (ui.go:67) and the pinned `userProfileFooter` (sidebar_user.templ:126). The topbar (`appShell` → `layout.Navbar`, ui.templ:84) shows no identity. The `currentUser` struct (ui.go:166) already carries `Name/Email/Picture`, and `appShell` already receives it. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Move identity to a topbar account menu (avatar dropdown), removing the sidebar "Account" group and footer.
- Support multiple concurrently signed-in Memory accounts with switch-without-reauth.
- Preserve the existing "stay signed in across gateway restart" property for the active account.
- Keep token handling consistent with the current stateless-cookie pattern.

**Non-Goals:**
- No account-chooser page on logout (a dedicated chooser is deferred).
- No per-account "forget/remove" affordance in v1 — accounts leave the list by signing out while active, or on gateway restart.
- No change to the iOS client or the supervisor/bridge workers (they stay single-tenant).
- No persistence of the account registry to disk or to the Memory service.

## Decisions

### D1 — Active account in the signed cookie; inactive accounts in an in-memory registry

The active account stays in the existing signed `alfred_session` cookie (unchanged mechanics). Additional (inactive) account sessions live in a gateway in-memory registry keyed by a new install-id cookie. The account menu composes "active (cookie) + inactive (registry)".

- **Why:** preserves session survival across `air` restarts (the cookie is self-contained), while the registry is a convenience cache whose loss degrades gracefully to "re-add your other account". It also keeps inactive `refresh_token`s server-side, out of the browser.
- **Alternative rejected:** moving *all* sessions into an in-memory registry — logs everyone out on every gateway restart, a regression against today's stateless cookie.

### D2 — Account identity key is the Zitadel `sub` claim

Accounts are keyed by the OIDC `subject`. `decodeIDTokenClaims` (oidc.go:211) currently drops `sub`; this change extracts it and carries it through `sessionClaims` and the registry entry, so an account can be de-duplicated and targeted for switch regardless of token rotation.

- **Why:** `sub` is the IdP's stable, non-revocable account identifier; name/email can change, tokens rotate.
- **Alternative rejected:** email or preferred_username as the key — mutable, and not guaranteed unique.

### D3 — "Add another account" forces `prompt=select_account`

The add-account flow reuses `/auth/start` with an extra parameter that adds `prompt=select_account` to the Zitadel authorization request, so Zitadel shows an account picker instead of silently returning the already-active SSO account.

- **Why:** without a `prompt`, Zitadel reuses the existing SSO session and "add another" would just re-authenticate the same account.
- **Alternative rejected:** `prompt=login` — forces full credential entry even when a session exists; `select_account` is the least-friction way to pick a different account.

### D4 — Switch rotates the active cookie through the registry

Switching moves the current active claims into the registry and pops the target out, lazily refreshing the target's access token first, then re-issuing the cookie with the target's claims (restoring its remembered `ActiveProjectID`/`OrgID`).

- **Why:** keeps the active session in the cookie (D1) and reuses the existing `refreshSessionTokens` path for stale inactive tokens.
- **Alternative rejected:** keeping every account in its own cookie and rotating a pointer — cookie-count/size limits and multi-cookie rotation complexity for no benefit at this account count.

### D5 — Log out signs out the current account only

`/auth/logout` clears the active cookie and drops the active account; other accounts remain in the registry (still signed in). The user lands on `/auth/login`.

- **Why:** matches the common multi-account model (log out of this account, others remain); a global "log out everywhere" is easy to add later if wanted.
- **Alternative rejected:** clearing the whole registry on every logout — too aggressive; signing out of one account should not silently kill others.

### D6 — Menu navigation matches existing shell patterns

"My Profile" uses the same htmx `#main-content` partial swap as the sidebar; "Add another account", "Switch", and "Log out" are POST forms (full reload), consistent with `projectSwitcher`'s session-mutating POSTs. Nexus is visual inspiration only, not a technical template.

- **Why:** keeps one navigation style in the shell; session-mutating actions need a full reload anyway to swap the cookie and re-render the shell.
- **Alternative rejected:** adopting nexus's plain `<a href>` full-page navigation wholesale — breaks the app's established htmx partial-swap pattern.

### D7 — Install-id cookie is a signed random identifier

A new `alfred_install` cookie carries a signed random install id that keys the registry, issued on first sign-in and stable per browser. It is HttpOnly + SameSite=Lax, signed with the same `SessionSecret`.

- **Why:** a stable, unforgeable key so two browsers (or an incognito window) don't share accounts; signing prevents an attacker from injecting another install's id.
- **Alternative rejected:** a plain (unsigned) random cookie — the id is trust-significant (it selects which accounts you can switch to), so it must be integrity-checked.

## Risks / Trade-offs

- **[Registry is volatile]** inactive accounts are lost on gateway restart. → Mitigation: active account survives (cookie), so the user is never fully logged out; re-adding an account is one click.
- **[Concurrent write safety]** the registry is shared mutable state. → Mitigation: guard with a `sync.Mutex` (single gateway instance); no external store to race with.
- **[Unbounded inactive accounts]** users could accumulate many signed-in accounts until restart. → Mitigation: acceptable for v1 given the in-memory lifetime; a per-install cap or expiry sweep can be added later.
- **[`sub` is new to the cookie]** older cookies lack it. → Mitigation: treat a missing `sub` as "legacy single-account session" — it still works, just can't be de-duplicated/switched until the next full sign-in.
- **[`prompt=select_account` support]** depends on Zitadel honoring the prompt. → Mitigation: fall back gracefully to a plain re-auth if the IdP ignores it; the add flow still completes (worst case re-issues the same account, which D2 de-dups into a no-op switch).
