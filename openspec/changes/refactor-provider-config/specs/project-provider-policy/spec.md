## REMOVED Requirements

### Requirement: Project-Level Provider Policy
**Reason**: The `policy` enum (`none`, `organization`, `project`) is removed. Project-level override is now expressed by the presence or absence of a `project_provider_configs` row — no separate policy abstraction needed. The `project_provider_policies` table and `provider_policy` enum are dropped.
**Migration**: To give a project its own credentials, use `PUT /api/v1/projects/:projectId/providers/:provider`. To revert to org credentials, use `DELETE /api/v1/projects/:projectId/providers/:provider`.

### Requirement: Policy Enforcement at Instantiation Boundary
**Reason**: Replaced by the two-step structural resolution in `provider-config`. The `project_provider_policies` table is dropped; there is no policy column to check. Resolution is purely: look for project row, then org row.
**Migration**: No action needed — resolution is automatic based on row presence. See `provider-config` spec for the updated resolution hierarchy.
