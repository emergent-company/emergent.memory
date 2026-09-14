# Registry-sourced blueprint upgrades need a CTA

**Status:** done
**Created:** 2026-09-09
**Source:** [2026-09-09-schema-migration-resync](../sessions/2026-09-09-schema-migration-resync.md)

## What

Give installed blueprints a real update path when the newer version is **registry-sourced** (project-scoped schema registry), and consider an update affordance on the Installed rows themselves.

Current `/blueprints` behavior (gateway):
- `buildAvailableList` suppresses an applied **registry** name entirely — the name check at `gateway/blueprints.go:278` runs before any version comparison, so a newer registry version of an installed pack can never surface.
- The **Upgrades** card only fires when a strictly newer *bundled* version exists (`blueprints.go:294-298`, `Upgrade=true`).
- Installed rows (`gateway/blueprints.templ` `appliedRow`) offer only **Remove**; the detail page has no actions.
- `ListBlueprintVersions` (`gateway/blueprint_client.go:107-113`) can list per-name versions but is only used inside `resolveOrCreateBlueprint`, never surfaced as a diff.

Add: a latest-version-per-name comparison over registry rows + an "update available" indicator and CTA (re-apply newer version via the existing install path) for both sources.

## Why

Surfaced while fixing the schema-migrations dead end: the migrations page no longer points at `/blueprints` for staleness (correct — staleness is same-version lag), but genuine version upgrades for registry packs remain impossible to trigger from the UI, and the Upgrades card's coverage is bundled-only. A user who applies a new pack version through a registry blueprint has no visible way to do so today.

## Depends on

None. Gateway-only.

## Notes

- Bundled upgrade path is the model to mirror: `ui.go:882-891` splits `Upgrade` rows into an Upgrades card; the button re-runs seed + apply (`installBundledBlueprint`, `blueprints_handlers.go:113-119`). Registry path would use `installRegistryBlueprint` / `installBlueprintById`.
- Re-applying a newer registry version flows through the same install → drift-scan redirect logic (redirect only when stale count increases) added in the resync session.

## Done (2026-09-10)

- `buildAvailableList` (`gateway/blueprints.go`) now collapses project-scoped registry schemas to the highest version per name (`versionGreater`). An applied name surfaces (`Upgrade=true`, `Source="registry"`, latest ID/Version) only when the registry version is strictly newer than the applied one; same-or-older applied versions stay hidden (no regression); non-applied names keep the plain available behavior. Global (`ProjectID == ""`) schemas still skipped. Bundled path unchanged.
- Added `upgradeByName` helper to index upgrade rows by name.
- `gateway/blueprints.templ`: `installedSection` receives `upgradeByName(upgrades)` and passes the per-name upgrade to `appliedRow`. `appliedRow` renders an "Update available" badge and an Update CTA when a newer version exists — bundled posts hidden `name`, registry posts hidden `schemaId`, both through the existing `POST /blueprints/install` path. Remove stays.
- Generic `BlueprintsPage(upgrades)` card and `availableRow` unchanged; no new visual language.
- Tests (`gateway/blueprints_test.go`): `TestBuildAvailableListRegistryUpgrade` covers registry-older upgrade, applied same/newer hidden, not-applied available, global skipped, and bundled unchanged; `TestRenderAppliedRowUpdateCTA` covers bundled/registry CTA wiring and the no-upgrade row.
- Verified: `templ generate`, `go build ./...`, `go test ./...`, `golangci-lint run ./...` (0 issues).
