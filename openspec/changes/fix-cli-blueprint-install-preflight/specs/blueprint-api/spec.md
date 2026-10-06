## ADDED Requirements

### Requirement: CLI blueprint install validates and normalizes models before applying

`memory blueprints install <source>` SHALL run the offline blueprint validation before making any API call that mutates the project, and SHALL abort with a non-zero exit and an actionable error when validation reports any error, leaving the project unmodified. The error SHALL name the offending resource (agent), its model reference, and the expected `provider/model` form.

Before applying, install SHALL normalize every agent `model` reference through the shared `modelref` parser: a structured `model.provider` combined with a bare `model.name` SHALL be folded into the canonical `provider/model` name and the provider field cleared, so the reference sent to the server is always one the server's agent write path accepts. A model that itself contains `/` (for example a Vertex resource path) SHALL be preserved intact. A blueprint that passes offline validation SHALL install as before.

#### Scenario: Invalid bare model aborts with no mutation

- **WHEN** a blueprint agent declares a `model.name` that is not in `provider/model` form (for example `gemini-2.5-flash`)
- **THEN** `memory blueprints install` fails before any API call, reporting the agent, the offending model, and the expected `provider/model` form, and no pack, agent, skill, or seed data is created or modified

#### Scenario: Structured provider normalized before apply

- **WHEN** a blueprint agent declares `model: { name: gpt-4o, provider: openai }`
- **THEN** install sends the canonical reference `openai/gpt-4o` to the server with the provider field cleared

#### Scenario: Valid blueprint installs as before

- **WHEN** a blueprint passes offline validation
- **THEN** install applies its packs, agents, skills, and seed data exactly as before, reporting the same results
