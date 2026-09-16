## Purpose
Author object types and blueprint drafts directly from the UI so users can define a project schema without installing a bundled or registry pack.

## ADDED Requirements

### Requirement: Define an object type via UI
The UI SHALL let a user define a custom object type with a name and typed properties, which becomes a project-scoped schema and is selectable in the object-create form.

#### Scenario: Create an object type
- **WHEN** a user creates an object type with a name and at least one property
- **THEN** the type appears in the schema and can be chosen when creating an object

### Requirement: Author and install a blueprint draft via UI
The UI SHALL let a user author a private blueprint draft containing object types and install it as an applied schema.

#### Scenario: Install an authored draft
- **WHEN** a user authors a draft with object types and installs it
- **THEN** the object types become available for object creation and the blueprint appears in the applied list
