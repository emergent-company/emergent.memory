## ADDED Requirements

### Requirement: Remove legacy NestJS backend

The system SHALL NOT include the legacy NestJS backend application.

#### Scenario: Codebase cleanup

- **WHEN** a developer inspects the workspace
- **THEN** the `apps/server` directory MUST NOT exist
- **AND** the root `package.json` MUST NOT contain `@nestjs/*` or related NestJS dependencies
- **AND** workspace configurations (`nx.json`, workspace scripts) MUST NOT contain targets for the `server` project
