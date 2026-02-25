## ADDED Requirements

### Requirement: Admin frontend lives in standalone repository
The React admin frontend SHALL exist as a standalone project in `/root/emergent.memory.ui` (remote: `emergent-company/emergent.memory.ui`), independent of the Nx monorepo.

#### Scenario: Admin repo builds standalone
- **WHEN** a developer runs `npm install && npm run build` in `/root/emergent.memory.ui`
- **THEN** the build SHALL succeed without requiring the monorepo to be present
- **AND** no scripts SHALL reference `../../` paths

#### Scenario: Admin repo no longer in monorepo
- **WHEN** a developer clones `/root/emergent`
- **THEN** `apps/admin/` SHALL NOT exist
- **AND** `package.json` workspaces SHALL NOT include `apps/admin`
