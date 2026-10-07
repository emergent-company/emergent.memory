## Why

Blueprints imported from GitHub are created and published through the existing
import path, but the record stores nothing about where it came from. A
`kb.blueprints` row has no notion of its upstream source — no source kind, no
URL, no ref. Update-tracking (comparing a stored blueprint against its upstream
repository to detect drift) is therefore impossible: there is nothing to
compare against. Persisting source provenance on the blueprint record is the
prerequisite for that work.

## What Changes

- Add three nullable columns to `kb.blueprints`: `source_kind`, `source_url`,
  and `source_ref`.
- Extend the `Blueprint` entity and `CreateBlueprintRequest` with matching
  camelCase fields (`sourceKind`, `sourceUrl`, `sourceRef`), exposed in API
  responses and accepted on create.
- `CreateBlueprint` persists the provenance fields when supplied.
- `CreateBlueprint` now computes and stores the canonical manifest checksum at
  create time (previously only `PublishBlueprint` set it), reusing the exact
  same helper, so drift comparison works on a draft without requiring publish.
- The GitHub import path (`ImportFromGitHub`) populates `sourceKind="github"`,
  `sourceURL="https://github.com/<org>/<repo>"` (normalized — no ref, no
  `.git`), and `sourceRef` from the parsed ref, normalizing the `HEAD` sentinel
  to the empty string (repository default branch).

Out of scope (later changes): the update-check itself, scheduled jobs, GitHub
tokens, and UI. This change only persists provenance.

## Capabilities

### Modified Capabilities

- `blueprint-api`: GitHub-imported blueprints record their source provenance,
  and created blueprints carry a canonical manifest checksum.

## Impact

- **Server** (`apps/server`): migration `00222`, `domain/blueprints` entity,
  service, and GitHub-import path.
- **Tests**: unit/integration tests for import provenance and create-time
  checksum.
- **No SDK change**: `apps/server/pkg/sdk` holds only schema definitions, no
  blueprints client.
