## ADDED Requirements

### Requirement: Required configuration rows show the registry's guidance

The create/edit form SHALL show a required configuration row's registry description as
a hint beneath that row, so the user knows what kind of value is expected. When a
required header key carries a registry value template, the form SHALL use that template
as the value input's placeholder to convey the expected shape. The template SHALL be a
placeholder only and SHALL NOT be prefilled into the input, so no credential or
credential-shaped value is pre-populated.

#### Scenario: Description hint is shown on a required row

- **WHEN** the edit form renders a required key whose registry declaration includes a description
- **THEN** the description is displayed as a hint on that key's row

#### Scenario: Value template is shown as a placeholder

- **WHEN** the form renders a required header key whose registry declaration includes a value template
- **THEN** the value input shows that template as its placeholder and its submitted value is empty until the user types one

#### Scenario: Row without a template keeps the generic placeholder

- **WHEN** the form renders a required key whose registry declaration has no value template
- **THEN** the value input keeps the existing state-based placeholder (for example `Enter value` for a still-missing required key)
