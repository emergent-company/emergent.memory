## ADDED Requirements

### Requirement: Gated all-grant posture is observable

When the legacy all-or-nothing userinfo grant is active — that is, when `ZITADEL_USERINFO_GRANT_ALL_SCOPES` is enabled AND introspection is not configured (no `ZITADEL_CLIENT_JWT`/`ZITADEL_CLIENT_JWT_PATH`, or introspection disabled) — the system SHALL make that posture visible without changing it. It SHALL emit a startup warning at a level an operator sees in normal startup logs, naming the effect (a userinfo-authenticated OIDC user receives the full scope catalogue) and both remediations (configure `ZITADEL_CLIENT_JWT`/`ZITADEL_CLIENT_JWT_PATH`, or set `ZITADEL_USERINFO_GRANT_ALL_SCOPES=false`). The system SHALL also expose the posture in the health response as an `oidc_all_grant` check entry that reports a non-failing configuration-warning status when active and a healthy status when not; that entry SHALL NOT affect the overall health status or the HTTP status code.

#### Scenario: Ship-default posture is announced at startup
- **GIVEN** `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` (the shipped default) and no introspection client credentials configured
- **WHEN** the server starts
- **THEN** a warning is logged that names the all-grant effect and both remediations
- **AND** the health response's `oidc_all_grant` check reports a warning status without making the service unhealthy

#### Scenario: No warning once introspection is configured
- **GIVEN** introspection is configured (`ZITADEL_CLIENT_JWT` or `ZITADEL_CLIENT_JWT_PATH` set)
- **AND** `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true`
- **WHEN** the server starts
- **THEN** no all-grant warning is logged
- **AND** the health response's `oidc_all_grant` check reports a healthy status

#### Scenario: No warning when the flag is disabled
- **GIVEN** `ZITADEL_USERINFO_GRANT_ALL_SCOPES=false` and introspection unconfigured
- **WHEN** the server starts
- **THEN** no all-grant warning is logged
- **AND** the health response's `oidc_all_grant` check reports a healthy status
