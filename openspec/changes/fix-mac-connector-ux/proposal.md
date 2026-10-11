## Why

A pass over the mac connector app surfaced several signed-out / informational
surfaces that mislead the user:

- The window **footer** derives its text from engine + hub internals, so it
  reads "Connected · Connected: a project · connected — hub shows 3 tool(s),
  matches local set" even when the user is not authenticated (a persisted
  connected-project id outlives a sign-out). None of that is meaningful to the
  user, who only needs to know whether a project is connected.
- An account whose session has expired shows **"Needs sign-in again"** but the
  only action is destructive **"Sign out"**. Worse, once the session is invalid
  the Project & Account page hides all stored accounts behind a single generic
  Production sign-in prompt, so a **Dev** account can never be re-authenticated.
- Expected auth/session failures leak the connector CLI's internals as a raw
  error banner, e.g. `Unexpected identity provider response: memory-connector
  auth access-token: not signed in - run 'memory-connector auth login' first`.
- **About** is listed at the end of the Settings cluster although it is not
  project/settings context; its **Details** card shows an **Engine version**
  (an internal, separately-versioned number that does not match the app) and a
  single raw **Server** URL (misleading now that Prod and Dev are both
  supported).
- The bundled engine reports a hard-coded dev default (`0.1.0`) instead of the
  app's release version, because the embedded-engine build script does not
  stamp it.

## What Changes

- **Footer** shows a single project-connection status — a green dot with
  "1 project connected", or an orange dot with "no projects connected" — driven
  by the effective signed-in state together with the connected project. Engine
  and hub health move to the footer's help tooltip (the menu-bar icon is
  unchanged).
- **Accounts card** on the Project & Account page lists stored accounts even
  when the connector session is invalid, and each account that needs
  re-authentication offers a **"Sign in again"** action (primary) alongside
  "Sign out" (secondary). Re-authentication targets that account's **own**
  environment, so a Dev account stays Dev.
- Expected auth/session failures are classified by one shared helper and mapped
  to a signed-out state; they are **never** surfaced as raw error text. Only
  genuine, non-auth failures show an error.
- **Sidebar**: About is pinned to the bottom, visually separated from the
  Information and Settings sections.
- **About**: the Details card drops the Engine version and Server rows (it keeps
  the instance identifier; the app version/build already appear in the header).
- **Engine version**: the embedded-engine build stamps the engine with the
  app's release version (`-X main.version=$MARKETING_VERSION`), so the bundled
  engine reports the same version as the app.
- Housekeeping: `.gitignore` ignores the XcodeGen-generated `.xcodeproj` /
  `.xcworkspace` / `DerivedData` so they can never be committed (`project.yml`
  is the source of truth).

## Capabilities

### Modified Capabilities

- `mac-connector-app`: signed-out management surface lists stored accounts and
  offers per-account re-authentication against the account's own environment;
  expected auth/session failures never render as raw errors; About is separated
  from the settings clusters; the bundled engine reports the app's release
  version.

### New Capabilities

- none.

## Impact

- **Views**: `MainWindowView.swift`, `AppState.swift`, `AboutPage.swift`,
  `ProjectAccountPage.swift`, `ProjectAccountSections.swift`.
- **Stores/models**: `AccountStore.swift`, `ProjectStore.swift`, new
  `AuthFailure.swift`, new `ProjectConnectionSummary.swift`.
- **Build**: `MemoryConnector/Scripts/build-engine.sh` (engine version stamp),
  root `.gitignore`.
- **Tests**: new `AuthFailureTests`, `ProjectConnectionSummaryTests`; extended
  `AccountStoreTests`.
- **No server, API, or CLI changes.** The connector engine binary is unchanged
  apart from the version stamp injected at app-build time.
