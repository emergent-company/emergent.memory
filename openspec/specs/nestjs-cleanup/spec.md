# nestjs-cleanup Specification

## Purpose

Documents the removal of the legacy NestJS backend from the monorepo. The project has fully migrated to a Go backend (apps/server-go).

## Requirements

### Requirement: Remove legacy NestJS backend

The system SHALL NOT include the legacy NestJS backend application.

#### Scenario: Codebase cleanup

- **WHEN** a developer inspects the workspace
- **THEN** the `apps/server` directory MUST NOT exist
- **AND** the root `package.json` MUST NOT contain `@nestjs/*` or related NestJS dependencies
- **AND** workspace configurations (`nx.json`, workspace scripts) MUST NOT contain targets for the `server` project
