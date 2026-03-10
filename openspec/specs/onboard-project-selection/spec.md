## ADDED Requirements

### Requirement: Project selection before onboarding
The `emergent-onboard` skill SHALL prompt the user to select an existing Emergent project or create a new one before designing a template pack.

#### Scenario: Projects exist — user selects one
- **WHEN** the skill runs `memory projects list` and one or more projects are returned
- **THEN** the skill presents the list to the user and asks which project to use for onboarding

#### Scenario: No projects exist
- **WHEN** the skill runs `memory projects list` and no projects are returned
- **THEN** the skill skips the selection step and goes directly to creating a new project

#### Scenario: User chooses to create a new project
- **WHEN** the user indicates they want a new project (either because none exist or by choice)
- **THEN** the skill runs `memory projects create --name "<name>"` with a name derived from the repository directory or confirmed with the user

### Requirement: Persist project ID to .env.local
After a project is selected or created, the skill SHALL write `EMERGENT_PROJECT=<project-id>` to `.env.local` in the repository root so that all subsequent CLI calls in that directory use the correct project context.

#### Scenario: .env.local does not exist
- **WHEN** `.env.local` does not exist in the repository root
- **THEN** the skill creates it with a single line: `EMERGENT_PROJECT=<id>`

#### Scenario: .env.local exists without EMERGENT_PROJECT
- **WHEN** `.env.local` exists but does not contain `EMERGENT_PROJECT`
- **THEN** the skill appends `EMERGENT_PROJECT=<id>` to the file

#### Scenario: .env.local exists with a different EMERGENT_PROJECT
- **WHEN** `.env.local` contains `EMERGENT_PROJECT=<other-id>`
- **THEN** the skill replaces the existing line with the new project ID

### Requirement: Skip selection if project already configured
The skill SHALL detect when `.env.local` already contains `EMERGENT_PROJECT` and offer to confirm or change the project rather than running full selection again.

#### Scenario: Re-running onboard on an already-onboarded repo
- **WHEN** `.env.local` already contains `EMERGENT_PROJECT=<id>` at the start of onboarding
- **THEN** the skill reads the project ID, shows the project name, and asks the user: "This repo is already connected to project X. Continue with this project, or switch to a different one?"

#### Scenario: User confirms the existing project
- **WHEN** the user confirms they want to continue with the existing project
- **THEN** the skill skips project selection and proceeds to the next step

#### Scenario: User wants to switch projects
- **WHEN** the user says they want to switch to a different project
- **THEN** the skill runs the full project selection flow and overwrites `.env.local`
