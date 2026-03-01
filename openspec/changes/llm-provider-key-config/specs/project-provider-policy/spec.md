## ADDED Requirements

### Requirement: Project-Level Provider Policy
The system SHALL allow projects to define a provider policy (`none`, `organization`, or `project`) per supported LLM provider to control credential inheritance.

#### Scenario: Setting policy to none
- **WHEN** a user sets the policy for a project to `none`
- **THEN** the project SHALL NOT inherit the organization's credentials
- **AND** SHALL fall back to the server environment configuration

#### Scenario: Setting policy to organization
- **WHEN** a user sets the policy for a project to `organization`
- **THEN** the project SHALL inherit the organization's credentials and model selections

#### Scenario: Cascading to server environment
- **WHEN** a project policy is `organization` but the organization has not configured a credential for the required provider
- **THEN** the system SHALL seamlessly fall back to using the server's environment configuration (e.g. `GOOGLE_API_KEY`)

#### Scenario: Setting policy to project
- **WHEN** a user sets the policy for a project to `project`
- **THEN** the system SHALL allow the user to save independent credentials and model selections for that specific project

### Requirement: Policy Enforcement at Instantiation Boundary
The system SHALL enforce the project provider policy at the exact boundary where an LLM client (generative or embedding) is instantiated for an operation.

#### Scenario: Enforcing policy during request
- **WHEN** an operation requests an LLM client
- **THEN** the system SHALL check the `project_provider_policies` table using the `ProjectID` from the `context.Context`
- **AND** SHALL instantiate the client using the exact credentials dictated by the active policy