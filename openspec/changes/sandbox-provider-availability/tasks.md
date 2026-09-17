## 1. Health monitor lifetime (R1)

- [x] 1.1 Refactor `StartHealthMonitoring` to delegate to unexported `startHealthMonitoring(ctx, interval)` with a 30s default
- [x] 1.2 Detach the monitor goroutine from the caller ctx via `context.WithoutCancel` so it runs for the process lifetime
- [x] 1.3 Make `StopHealthMonitoring` idempotent with `sync.Once` (fix double-close panic)
- [x] 1.4 Add `TestHealthMonitoringSurvivesParentCancellation` regression test (short interval, cancellable parent, idempotent stop)

## 2. Report every known provider (R2)

- [x] 2.1 Add `Orchestrator.MarkUnavailable(pt, displayName, reason)` (health map only, never the providers map)
- [x] 2.2 Add `displayNames` map and `knownProviderTypes` stable ordering
- [x] 2.3 Rewrite `ListProviders` to return all known types plus leftovers, in deterministic order, with `Name` always populated
- [x] 2.4 Add `registered` field to `ProviderStatusResponse`; omit `capabilities` when nil
- [x] 2.5 Call `MarkUnavailable` in `registerProviders` for every skip path with precise reasons
- [x] 2.6 Add `TestListProvidersReportsUnavailableProviders` and `TestMarkUnavailableDoesNotRegister`

## 3. Actionable selection errors (R3)

- [x] 3.1 Add `rejectionReasons()` helper (deterministic, in `ListProviders` order)
- [x] 3.2 Append reasons to `SelectProvider` and `SelectProviderWithFallback` errors (preserve existing prefixes)
- [x] 3.3 Add `TestSelectionErrorEnumeratesReasons`

## 4. Spec and docs

- [x] 4.1 Write OpenSpec change artifacts (proposal, tasks, delta spec)
- [x] 4.2 Update `docs/agent-sandbox/DEPLOYMENT.md` health section
- [x] 4.3 Update `docs/agent-sandbox/PROVIDER_SELECTION.md` fallback section

## 5. Availability-driven provider picker (web-ui)

- [x] 5.1 Add `registered` to the gateway `SandboxProvider` client struct
- [x] 5.2 Render the provider `<select>` from `data.Providers` (`Auto` first; unavailable options `disabled` + labelled; reason via tooltip)
- [x] 5.3 Preserve a stored provider that the endpoint does not report (synthetic selected option)
- [x] 5.4 Add the per-provider availability status list (semantic daisyUI colors) and drop the old `Available:` note
- [x] 5.5 Surface a provider-fetch failure as an inline warning instead of a silent empty list
- [x] 5.6 Update/extend `agent_ui_test.go` + fake harness for healthy, unavailable, stored-but-unreported, and failed-list cases
