# Reference material

Read-only reference data used by examples and docs. Nothing here is imported at
runtime and nothing here may be edited to suit a test.

## `togaf-core-template-pack.json`

An example **blueprint template pack**: it declares a TOGAF-flavoured object and
relationship model (capabilities, business services, applications, data entities,
and the relationships between them) that a project can load as a starting schema.

It is schema data, not code. `author: "Nexus Platform"` is the pack's own
provenance field, not a dependency on a private repository.

Use it as a shape reference when writing your own pack:

```bash
# inspect the shape
jq '{name, version, minEngineVersion, objectTypes: (.objectTypes|length)}' \
  reference/togaf-core-template-pack.json
```

The blueprint format it follows is documented in
[`docs/site/developer-guide/schema.md`](../docs/site/developer-guide/schema.md)
and in the `code-memory` example pack under
[`blueprints/`](../blueprints/code-memory-blueprint/).

## Adding reference material

- One folder per upstream project, and record its origin, licence and the commit
  or version you took it from.
- Prefer a link plus the extracted pattern over a vendored copy. If you must
  vendor it, put the licence text next to it and add the component to `NOTICE`.
- Do not add private repositories, internal hosts, or credentials here — this
  file ships in a public repository.
