# Tasks

## 1. entity-query field_strategy

- [x] 1.1 Add `field_strategy` to the entity-query input schema (values `compact`/`minimal`/`full`).
- [x] 1.2 Default the type/pagination path to `compact`; select `'{}'::jsonb` instead of the full `properties` column.
- [x] 1.3 Keep the `ids[]` fast-path defaulted to `full` (full properties), honouring an explicit override.
- [x] 1.4 Apply the existing `fields` projection only under `full`.
- [x] 1.5 Unit-test the projection builder (`entityPropertiesProjection`).

## 2. Spec

- [x] 2.1 Document the `field_strategy` projection contract in `mcp-tool-results`.
