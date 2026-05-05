## ADDED Requirements

### Requirement: FeatureSet config struct in internal/config
`apps/server/internal/config` SHALL provide a `FeatureSet` struct parsed from environment variables. Each field SHALL default to the value that preserves current production behavior. The `Config` struct SHALL include a `Features FeatureSet` field.

The following feature flags SHALL be defined:

| Flag | Env Var | Default |
|---|---|---|
| Agents | `FEATURE_AGENTS` | `true` |
| MCP | `FEATURE_MCP` | `true` |
| Sandbox | `FEATURE_SANDBOX` | `true` |
| Backups | `FEATURE_BACKUPS` | `true` |
| Monitoring | `FEATURE_MONITORING` | `true` |
| Tracing | `FEATURE_TRACING` | `true` |
| Superadmin | `FEATURE_SUPERADMIN` | `true` |
| Devtools | `FEATURE_DEVTOOLS` | `false` |
| Chat | `FEATURE_CHAT` | `false` |

#### Scenario: Server starts with all defaults unchanged
- **WHEN** the server starts with no `FEATURE_*` env vars set
- **THEN** all domains that were previously always-on continue to start normally

#### Scenario: Feature disabled via env var
- **WHEN** `FEATURE_AGENTS=false` is set before server start
- **THEN** the agents domain module is not registered with fx and the server starts without it

### Requirement: Conditional fx.Options in main.go
`cmd/server/main.go` SHALL build the fx options slice conditionally based on `cfg.Features`. Domains controlled by feature flags SHALL only be added to the fx options slice when their corresponding flag is `true`.

#### Scenario: Disabled domain has no routes registered
- **WHEN** a feature flag is set to `false`
- **THEN** the corresponding domain's HTTP routes are not registered and return 404

#### Scenario: Disabled domain has no fx module loaded
- **WHEN** a feature flag is set to `false`
- **THEN** the corresponding domain's `fx.Module` is not passed to `fx.New()` and its services are not instantiated

### Requirement: Core domains are never feature-flagged
Domains essential to all deployments (graph, documents, chunks, search, branches, schemas, schemaregistry, orgs, projects, users, userprofile, apitoken, useraccess, invites, health, auth) SHALL NOT be gated behind feature flags. They SHALL always be included in the fx options.

#### Scenario: Core domain always starts regardless of feature flags
- **WHEN** all `FEATURE_*` env vars are set to `false`
- **THEN** the server still starts successfully with core graph, documents, search, and user management routes available

### Requirement: apperror Style B standardization
All call sites constructing `apperror` values SHALL use the constructor-style functions (`apperror.NewBadRequest`, `apperror.NewInternal`, `apperror.NewNotFound`, etc.). The builder-chaining style (`apperror.ErrBadRequest.WithMessage(...)`) SHALL NOT be used in new or existing code.

#### Scenario: No Style A apperror usage in codebase
- **WHEN** the codebase is compiled after migration
- **THEN** no call site uses the `.WithMessage()`, `.WithInternal()` chaining pattern on sentinel `apperror` values
