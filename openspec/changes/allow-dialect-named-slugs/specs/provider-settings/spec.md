## ADDED Requirements

### Requirement: Provider instance slugs are unique per project

A project's provider instance slug SHALL be unique within that project, enforced by the `(project_id, slug)` database constraint. A slug MAY equal a registered provider dialect name; the system SHALL NOT reject a slug merely because it names a dialect (including another dialect's name). Model reference resolution SHALL be slug-first: a reference whose provider segment matches a project instance slug resolves to that instance, and only when no instance slug matches does the system fall back to a dialect's default instance.

#### Scenario: Dialect-name slug for another dialect succeeds

- **WHEN** a user creates or saves a provider instance whose slug equals another vendor's dialect name (for example an OpenAI-dialect instance named `deepseek`)
- **THEN** the slug is accepted, provided it is non-empty, within the length limit, matches `[a-z0-9][a-z0-9-]*`, and is not already used by another instance in the same project

#### Scenario: Reference matching an instance slug resolves to that instance

- **WHEN** a model reference's provider segment matches a project's instance slug
- **THEN** resolution returns that instance, even when the segment is also a registered dialect name

#### Scenario: Reference naming only a dialect resolves to its default instance

- **WHEN** a model reference's provider segment is a dialect name and no project instance has that slug
- **THEN** resolution falls back to that dialect's default instance

#### Scenario: Duplicate slug within a project is rejected

- **WHEN** a user saves a provider instance whose slug is already used by another instance in the same project
- **THEN** the `(project_id, slug)` constraint rejects the save
