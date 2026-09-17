## MODIFIED Requirements

### Requirement: Navigate agent sections

The agent pages SHALL show a vertical sub-menu with Dashboard, Settings, Sandbox, and Sessions sections scoped to that agent. On any agent Settings subpage, the Settings entry SHALL expand into a group of six indented subpage links — General, Model, Tools, Skills, Delegation, and MCP sharing — with the active subpage highlighted.

#### Scenario: Sub-menu present

- **WHEN** an agent page loads
- **THEN** the sub-menu shows Dashboard, Settings, Sandbox, and Sessions links for that agent only

#### Scenario: Settings group lists the six subpages

- **WHEN** an agent Settings subpage loads
- **THEN** the Settings entry shows six indented children — General, Model, Tools, Skills, Delegation, and MCP sharing — and the current subpage is highlighted

#### Scenario: Subpages are distinct routes

- **WHEN** the owner opens any Settings subpage
- **THEN** each subpage renders only its own panel at `/agents/:id/settings[/<section>]`, and an unknown section returns 404

### Requirement: Edit an agent in-page

The Settings surface SHALL be split into one form per concern — General (name, system prompt, language), Model (model, temperature, max tokens), Tools (default approval + tool picker), Skills, and Delegation — each with its own Save action that persists ONLY that concern's fields. It SHALL NOT use a modal.

#### Scenario: Edit form shows current values

- **WHEN** a Settings subpage loads
- **THEN** the form shows the agent's current values for that concern's fields

#### Scenario: Save changes

- **WHEN** the owner submits a section's form with valid values
- **THEN** only that section's fields change, every other section's fields are preserved, and the page shows a success message

#### Scenario: Save rejected

- **WHEN** the name is empty or delegation is enabled with no targets
- **THEN** the page shows an error and does not persist the change
