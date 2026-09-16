## Context

The Settings area today has `/settings` (a single long page with seven stacked panels) and `/settings/approvals` (already separate). The agent details pages use a vertical sub-nav rail (`agentSubNav` + `agentSubNavItem` in `gateway/agent.templ`): a sticky left rail on desktop, horizontal scroll on small screens, each item an icon + label with an active highlight. Motivation for the change is in proposal.md.

## Goals / Non-Goals

**Goals:**
- Give Settings the same sub-nav pattern as agent details.
- Split the single Project Settings page into General, Voice, Devices, and Approvals pages without changing save/error behavior.

**Non-Goals:**
- No new settings or fields; only page organization and navigation change.
- No changes to the top-level app sidebar (`gateway/ui.go`).
- No changes to voice/device/approvals persistence semantics.

## Decisions

**1. URL scheme: one route per section, no nested wildcard.**

- `/settings` → General (project info, assistant agent, agent overrides, remember & dedup).
- `/settings/voice` → Voice.
- `/settings/devices` → iOS setup + Devices.
- `/settings/approvals` → Approvals (route already exists).

Rationale: flat, explicit routes mirror the agent sub-nav (`/agents/:id`, `/agents/:id/settings`, …). Keeps PRG save handlers (`POST /settings/voice`, `POST /settings/devices`, …) simple and preserves existing `POST /settings/project`, `POST /settings/remember`, `POST /settings/assistant` for the General page. Alternative considered: a `/settings/:section` wildcard with one handler — rejected because sections load different data and have different forms, so a single handler would reintroduce the monolithic loading we are removing.

**2. Reuse the agent sub-nav component rather than build a new one.**

Rename the generic `agentSubNavItem` link helper to `subNavItem` (internal, templ-generated) and add a `settingsSubNav(active string)` that renders General/Voice/Devices/Approvals with the same styling. `agentSubNav` keeps using the same helper. Rationale: identical visual pattern already exists and is tested; duplicating it risks drift. Alternative considered: a from-scratch settings nav — rejected (duplication, no visual benefit).

**3. Split handlers and page data per section.**

Replace the single `projectSettingsData`/`uiProjectSettings` with per-section handlers and data structs that load only what that page needs:
- `uiProjectSettings` (General) → project info, agents, assistant, overrides, remember/dedup.
- `uiProjectSettingsVoice` → voice settings.
- `uiProjectSettingsDevices` → iOS QR image + devices.

Rationale: mirrors the agent pages, which load per-section; avoids fetching voice/devices data when only editing General. Existing save handlers (`uiProjectSettingsUpdate`, `uiProjectSettingsRemember`, `uiProjectSettingsAssistant`, `uiProjectSettingsVoice`, `uiRevokeDevice`) stay unchanged and redirect back to their section. Alternative considered: keep one handler loading all data and pass a section flag — rejected (keeps monolithic loading, more conditional rendering in one big template).

**4. Preserve PRG + flash toast + per-section error handling.**

Each sub-page keeps `flashToasts` and its own `pageError` state. No shared form state between pages.

## Risks / Trade-offs

- [Voice/devices handlers now used from a different URL] → Existing save flows redirect back to their own section; update tests that assert the old single-page DOM.
- [Splitting `uiProjectSettings` could briefly break data loading] → Each new handler reuses the existing `resolveVoiceSettings` / project fetch helpers verbatim; keep the shared `pageHeader`/crumbs.
- [Sub-nav component rename touches agent pages] → Rename is mechanical and covered by existing `agent_ui_test.go` render tests.

## Migration Plan

- Add new routes, handlers, and the `settingsSubNav` component; move panels from `project_settings.templ` into per-section templates.
- Update render/handler tests for the new page structure; keep old save handlers as-is.
- No data migration; rollback is reverting the route/template split (save handlers are unchanged).
