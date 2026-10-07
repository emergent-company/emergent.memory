## ADDED Requirements

### Requirement: Connection-test egress honors an operator allowlist

The gateway's provider connection test (`POST /settings/providers/test`) and the inline
base-url check (`POST /settings/providers/check-url`) SHALL dial only publicly-routable
addresses by default, and SHALL additionally permit any hostname or CIDR listed in the
`PROVIDER_PROBE_ALLOW` environment variable (comma-separated). This lets a self-hosted
deployment test an internal gateway (for example `litellm`) without disabling the SSRF
guard for every destination. Entries that are neither a valid hostname nor a valid CIDR
SHALL be ignored; an unset or empty variable SHALL leave the public-only policy unchanged.

#### Scenario: Allowlisted internal host is testable

- **WHEN** `PROVIDER_PROBE_ALLOW` lists `litellm`
- **AND** the user tests a base_url whose host resolves to a non-public address
- **THEN** the connection test dials it and reports the endpoint's response instead of refusing the non-public address

#### Scenario: Allowlisted CIDR is testable

- **WHEN** `PROVIDER_PROBE_ALLOW` lists `100.64.0.0/10`
- **AND** the target host resolves to `100.113.48.6`
- **THEN** the connection test dials the address

#### Scenario: Unlisted non-public host stays blocked

- **WHEN** `PROVIDER_PROBE_ALLOW` does not list the target host or an address range containing it
- **THEN** the connection test refuses to dial the non-public address

#### Scenario: Cloud-metadata address stays blocked unless explicitly listed

- **WHEN** `PROVIDER_PROBE_ALLOW` lists an unrelated host
- **AND** the target resolves to `169.254.169.254`
- **THEN** the connection test refuses to dial it

#### Scenario: Default policy is unchanged

- **WHEN** `PROVIDER_PROBE_ALLOW` is unset or empty
- **THEN** only publicly-routable addresses are dialable, exactly as before

#### Scenario: Invalid allowlist entries are ignored

- **WHEN** `PROVIDER_PROBE_ALLOW` contains an entry that is neither a valid hostname nor a valid CIDR
- **THEN** that entry is ignored and the remaining valid entries still apply
