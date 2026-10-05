## Why

Provider instance slugs are currently rejected when they equal a *different* provider's dialect name (`provider slug %q is a reserved dialect name`). Since PR #1409 the registry grew from 4 to ~24 vendors, so almost any vendor name (e.g. `litellm`) can no longer be used as an instance name of another dialect. Reference resolution is already slug-first with dialect fallback, so this restriction is unnecessary: allow any valid, project-scoped-unique slug.

## What Changes

- Remove the reserved-dialect check from `validateProviderSlug`, so a slug MAY equal a registered dialect name (including another dialect's name).
- Keep the existing slug validation: non-empty, max length, and the `[a-z0-9][a-z0-9-]*` charset.
- Uniqueness remains enforced per project by the existing `(project_id, slug)` DB constraint.
- Reference resolution stays slug-first: a reference whose provider segment matches a project instance slug resolves to that instance; only when no instance slug matches and the segment names a dialect does it fall back to that dialect's default instance.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `provider-settings`: provider instance slugs are unique per project and may reuse dialect names; resolution is slug-first with dialect fallback.

## Impact

- **Server** (`apps/server/domain/provider/service.go`): drop the reserved-dialect rejection in `validateProviderSlug`.
- **Tests** (`apps/server/domain/provider/service_slug_test.go`): update shadowing cases to expect success and add a regression test for slug-first resolution over a same-named dialect fallback.
- No UI, schema, or API-surface change.
