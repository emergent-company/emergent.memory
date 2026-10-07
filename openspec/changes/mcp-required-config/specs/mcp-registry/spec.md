## ADDED Requirements

### Requirement: Registry install resolves required configuration from the selected transport

When installing a catalog entry, memory SHALL collect the required configuration from
the **selected transport's** declaration: every remote header and environment variable
marked `isRequired` or `isSecret`. A key marked `isSecret` SHALL count as required even
when `isRequired` is absent, because a secret with no value is unusable. Memory SHALL
persist the collected list on the created server and SHALL return it in the install
result so the client can name the keys. The remote-only transport policy and the
registry remote-URL SSRF guard are unchanged.

#### Scenario: Secret header is surfaced

- **WHEN** a user installs `io.github.github/github-mcp-server`, whose selected remote declares an `Authorization` header with `isSecret: true`
- **THEN** the install result names `Authorization` and the created server records it as required configuration

#### Scenario: Install is created disabled when configuration is missing

- **WHEN** the selected transport declares a required-or-secret key with no value
- **THEN** the created server is created disabled and no tool discovery is attempted for it

#### Scenario: Fully-specified transport installs enabled

- **WHEN** the selected transport declares no required-or-secret keys
- **THEN** the server is created enabled and remote tool discovery is attempted as before

#### Scenario: Unrelated transport declarations are ignored

- **WHEN** a registry entry declares required-or-secret keys on a transport that is not the selected one (for example a stdio package, or an `sse` remote when `streamable-http` is selected)
- **THEN** only the selected transport's keys are recorded as required
