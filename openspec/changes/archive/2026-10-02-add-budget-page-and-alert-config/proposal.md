## Why

Budget configuration currently sits as the fourth fieldset on the already-crowded General settings page (`/settings`). A user can set the monthly spend cap but cannot choose *when* budget alerts fire: `kb.projects.budget_alert_threshold` is a server-defaulted fraction (0.80) that the UI neither shows nor edits. The alert producer (`apps/server/domain/provider/usage_service.go`, hardened in #1347) already reads that fraction and sends a durable, once-per-project-per-month notification when spend reaches `budget * threshold` — only the configuration surface is missing.

## What Changes

- Move the **Budget (USD)** control off the General settings page onto its own Settings sub-page at `/settings/budget`, following the existing settings sub-page pattern (Assistant / Overrides / Providers / Voice / Devices).
- Add a **Budget** entry to the settings sub-navigation rail so the page is discoverable; the General page keeps its other panels unchanged.
- Expose `budget_alert_threshold` on the project read DTO so the page can show the effective value (the server previously accepted the field on write but never returned it).
- Let the user edit **when budget alerts are sent** as a percentage of the monthly budget (1–100%), stored as the existing fraction in `kb.projects.budget_alert_threshold`.
- Validate the threshold on both sides: the gateway rejects a non-numeric, empty, zero, negative, or greater-than-100 percentage; the server rejects a fraction `<= 0` or `> 1`. This closes the #1347 regression path where a threshold of 0 produced an alert at $0 spend.
- Respect the existing alert semantics: a single threshold, evaluated against current-month spend, with the durable monthly dedup key unchanged.

## Capabilities

### New Capabilities
- `web-budget-settings`: the `/settings/budget` page — the monthly spend cap control, the alert-threshold percentage control, their inline saves, and the validation contract.

### Modified Capabilities
- `settings-navigation`: the Settings sub-navigation rail gains a Budget section.
- `project-settings-ui`: the General project-settings page no longer shows or edits the budget; that control moves to the Budget page.

## Impact

- `apps/server/domain/projects/`: `ProjectDTO` gains `budget_alert_threshold` and `ToDTO` populates it; `Service.Update` validates the threshold before persisting.
- `apps/web-ui/gateway/`: `Project` read model gains the threshold field; new GET handler + route `/settings/budget`; the existing inline field-save handler gains a `budget_alert_threshold` case (percent → fraction); `project_settings.templ` moves the budget fieldset to a new page and adds the rail entry.
- No database migration: `kb.projects.budget_usd` and `kb.projects.budget_alert_threshold` already exist (migrations `00063`, `00105`, `00212`).
- Tests: server-side threshold validation + DTO round-trip, gateway handler/route tests for the new page, and render tests; the existing #1347 budget-alert tests stay green.
