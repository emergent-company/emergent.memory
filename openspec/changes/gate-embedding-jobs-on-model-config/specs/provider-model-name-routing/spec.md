## Purpose

Define that the routing prefix on a model reference is stripped only when the first segment names a
recognized dialect / registered routing provider AND is not a project-registered instance/gateway slug — so a
provider instance slug such as `gemini` is never mistaken for a dialect prefix and stripped away.

## ADDED Requirements

### Requirement: Routing prefix is stripped only for a dialect, never for a project slug

`modelref.StripRoutingPrefix` SHALL strip the first routing segment of a model reference only when that
segment names a recognized dialect / registered routing provider AND is **not** a project-registered
instance/gateway slug. The decision SHALL be made with project context at the credential boundary, not
globally. When the first segment is a project-registered instance/gateway slug — even one that collides with a
dialect name (for example `gemini`, an accepted alias for the `google` dialect) — the prefix SHALL be
preserved.

#### Scenario: Project slug is not stripped

- **WHEN** the model reference is `gemini/gemini-embedding-001` and `gemini` is a project-registered instance/gateway slug
- **THEN** the full reference `gemini/gemini-embedding-001` is preserved, not stripped to the bare `gemini-embedding-001`

#### Scenario: Bare dialect prefix is stripped

- **WHEN** the model reference's first segment names a recognized dialect / registered routing provider and is not a project-registered instance/gateway slug
- **THEN** the routing prefix is stripped to the bare model name

#### Scenario: Unknown first segment is left unchanged

- **WHEN** the model reference's first segment is neither a recognized dialect nor a registered provider slug
- **THEN** the reference is left unchanged (no strip)
