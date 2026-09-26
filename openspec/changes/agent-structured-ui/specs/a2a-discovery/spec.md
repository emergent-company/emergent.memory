## ADDED Requirements

### Requirement: AgentCard advertises the A2UI extension

The AgentCard SHALL advertise A2UI support via `capabilities.extensions[]` with the extension URI `https://a2ui.org/a2a-extension/a2ui/v0.9.1`, carrying the supported catalog ids. The server SHALL accept A2UI client capabilities (`a2uiClientCapabilities`, and `a2uiClientDataModel` when `sendDataModel` is enabled) delivered in `message.metadata`, and SHALL NOT leak tenant data through the extension advertisement.

#### Scenario: Extension is advertised
- **WHEN** a client fetches the AgentCard
- **THEN** `capabilities.extensions` contains an entry whose `uri` is the A2UI v0.9.1 extension URI

#### Scenario: Catalog ids are declared
- **WHEN** a client fetches the AgentCard
- **THEN** the A2UI extension entry declares the catalog ids the server's surfaces may reference

#### Scenario: Advertisement leaks no tenant data
- **WHEN** a project has agent definitions or surface data
- **THEN** the A2UI extension advertisement contains none of that project's data
