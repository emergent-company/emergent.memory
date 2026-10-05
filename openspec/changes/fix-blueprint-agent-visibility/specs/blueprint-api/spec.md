## ADDED Requirements

### Requirement: Blueprint agent visibility uses the server enum

A blueprint agent manifest's `visibility` field SHALL use the server's canonical agent visibility levels — `project`, `external`, or `internal` — matching the `AgentVisibility` constants and `NormalizeVisibility` in `apps/server/domain/agents/entity.go`. An absent or empty value SHALL be treated as `project` (the server default). The offline CLI validator SHALL accept exactly these values and reject any other value (in particular the legacy `public`, `private`, and `system`), and its error message SHALL name the canonical set so a blueprint that passes offline validation also installs successfully.

#### Scenario: Canonical visibility accepted offline

- **WHEN** a blueprint agent manifest declares `visibility: project`, `visibility: external`, `visibility: internal`, or omits `visibility`
- **THEN** `memory blueprints validate` reports no visibility error for that agent

#### Scenario: Legacy visibility rejected offline

- **WHEN** a blueprint agent manifest declares `visibility: public`, `visibility: private`, or `visibility: system`
- **THEN** `memory blueprints validate` reports a visibility error whose message lists `project, external, internal`
