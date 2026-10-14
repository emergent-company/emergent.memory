## Why

The CLI blueprint preflight rejects any relationship whose `sourceType`/
`targetType` does not name an objectType declared by the **same pack** (#1853).
Blueprints are applied as a unit and the server accepts a relationship type whose
endpoint is owned by another pack, so this same-pack rule blocks legitimate
multi-pack blueprints and pushes authors to duplicate object types across packs
(which then collide on apply).

## What Changes

- A relationship's `sourceType`/`targetType` endpoints are validated against the
  **union** of object types declared by every pack in the blueprint, not the
  single pack that declares the relationship. An endpoint owned by a sibling
  pack in the same blueprint is accepted.
- The install preflight additionally accepts an endpoint that already exists in
  the **project** (an installed pack's type or a built-in type). It resolves
  those types with a single best-effort read-only call
  (`GET .../compiled-types`), issued **only** when an endpoint is not defined by
  any pack in the blueprint — so a blueprint whose endpoints all resolve locally
  still aborts offline with zero requests, and the #1586 zero-mutation guarantee
  holds regardless.
- An endpoint that matches no object type in the blueprint **and** none in the
  project still fails with an error naming the relationship and the unmatched
  type.
- `memory blueprints validate` stays offline and checks endpoints against the
  blueprint's own packs only; its help text says so and points at `install` for
  the project-aware check.
- The `memory blueprints validate` help text and the embedded CLI reference
  describe the union rule.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `cli-blueprints`: the offline blueprint validation accepts cross-pack
  relationship endpoints; the install preflight additionally accepts endpoints
  owned by the project.

## Impact

- `apps/cli/internal/blueprints/validator.go` — the endpoint check resolves
  against the union of the loaded packs' object types plus, at install, the
  project's types; `HasUnknownRelationshipEndpoints` gates the project read.
- `apps/cli/internal/cmd/blueprints.go` — the install preflight passes the
  project's object type names (compiled types) when needed; `validate` help text
  updated.
- Embedded `memory-cli-reference` SKILL.md (and its committed `.agents/skills`
  copy) — help text updated.
- No server change: `validateSchemaDefinitions` already accepts any non-empty
  `sourceType`/`targetType`; the server resolves types across the project.

## Known caveat

Install is additive: without `--upgrade` an already-installed pack is skipped, so
if the blueprint's new version of that pack would have added the referenced type,
the endpoint can be declared but unresolved. The declaration is metadata the
server accepts regardless, and `--upgrade` closes the gap. Fixing the additive
install semantics is out of scope.
