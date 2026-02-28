## ADDED Requirements

### Requirement: Script Execution
The agent SHALL be able to execute debugging and maintenance scripts found in the `scripts/` directory.

#### Scenario: Running Debug Scripts
- **WHEN** the user asks to debug a specific sub-system using a script
- **THEN** the agent identifies the relevant script in `scripts/` and executes it, reporting the output
