# Tasks — ui-data-authoring

Gateway features B.1/B.2 (this repo, spec `schema-authoring`) plus cross-repo B.3. Each task ends with its verification gate.

## B.1 — Object-type builder

- [ ] B.1.1 Add `POST /api/schema/object-types` (create) and `PUT /api/schema/object-types/:name` (update), mapped onto the compiled-schema path. Verify: `go build ./...` + `task lint` clean; a created type appears in `GET /api/blueprints/compiled-types`.
- [ ] B.1.2 Add the "New type" form on `/schema` and the editor on `/schema/object-types/:name` (name + typed properties). Verify: `templ generate` + `go build` clean; type is selectable in `/objects/new`.
- [ ] B.1.3 e2e spec: create a minimal type via the UI, then assert it's selectable in object-create. Verify: `npx playwright test specs/schema-authoring-ui.spec.ts` green.

## B.2 — Blueprint draft author

- [ ] B.2.1 Add draft create/save endpoints (private draft: name + version + object types) and wire install to the existing `installBlueprint` path. Verify: `go build` + `task lint` clean.
- [ ] B.2.2 Add "New draft" to the blueprints gallery + a draft editor (reuse B.1's type editor). Verify: `templ generate` + `go build` clean; installing the draft applies its types.
- [ ] B.2.3 e2e spec: author a draft with a type via the UI, install it, assert the type is object-creatable. Verify: green.

## B.3 — Deterministic test LLM (memory backend, cross-repo)

- [ ] B.3.1 File an issue in `emergent-company/emergent.memory` for a project `test_llm` flag that returns a canned generate response. Verify: issue filed; implementation tracked there (not in this repo).

## Notes

- The seed step (`auth.setup.ts` installs `personal-memory`) stays for now; B.1 lets tests seed a minimal type instead, which can replace the bundled-pack seed once B.1 is stable.
- No `skip_specs` — B.1/B.2 add real behavior (spec `schema-authoring`).
