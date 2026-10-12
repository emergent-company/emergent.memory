## ADDED Requirements

### Requirement: Blueprint pack-assignment ownership is stamped and guarded

Applying a blueprint SHALL record `source_blueprint_id` on the project's pack assignment so unapply
can later remove only what that blueprint owns. The stamp SHALL happen only when the assignment is
unowned (`source_blueprint_id IS NULL`) or already owned by the same blueprint name family (a newer
version of the same blueprint); it SHALL NOT overwrite an assignment owned by a different blueprint
name. Unapplying a blueprint SHALL soft-delete only the assignments it owns, and SHALL leave an
assignment in place while another applied blueprint still claims the pack.

#### Scenario: Apply claims an unowned assignment

- **WHEN** a blueprint is applied to a project and its pack assignment has no owning blueprint
- **THEN** the assignment's `source_blueprint_id` is set to that blueprint

#### Scenario: Apply does not overwrite a different blueprint's ownership

- **WHEN** a blueprint is applied but the pack assignment is already owned by a different blueprint name
- **THEN** the assignment's `source_blueprint_id` is unchanged

#### Scenario: Re-apply adopts the same family

- **WHEN** a newer version of the same blueprint (same name) is applied over an assignment owned by the
  older version
- **THEN** the assignment's `source_blueprint_id` is re-pointed to the newer blueprint

#### Scenario: Unapply removes only owned assignments

- **WHEN** a blueprint is unapplied and its pack assignment is still claimed by another applied blueprint
- **THEN** the assignment remains (the other blueprint still needs it)
