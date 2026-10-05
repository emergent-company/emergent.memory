## ADDED Requirements

### Requirement: Install a hosted MCP server from the registry

The app SHALL let the user browse the official MCP registry from the hosted MCP
servers page, search it by name, and select an entry. Selecting an entry SHALL
load the connector's suggested local configuration and open the Add-server sheet
prefilled with that suggestion (transport, command/args/env for stdio, or
url/headers for a remote transport, and a derived name). The app SHALL surface
the entry's required and secret environment variables and headers, and any
warning (for example, a missing `npx`/`uvx` runner). The user SHALL be able to
edit every prefilled field before confirming, and the server SHALL be created
only on confirmation through the existing create path. The app SHALL NOT create
a server merely by selecting an entry. Registry failures and an unreachable
management API SHALL surface a clear error state with a retry, and secrets SHALL
remain masked and stay in the connector's local config.

#### Scenario: Browse and search the registry

- **WHEN** the user opens the registry browse from the hosted MCP servers page and searches
- **THEN** matching entries are listed and one can be selected

#### Scenario: Prefilled suggestion for a stdio entry

- **WHEN** the user selects an entry that maps to a local stdio server
- **THEN** the Add-server sheet opens prefilled with the suggested command, arguments, and environment placeholders, and a derived name

#### Scenario: Prefilled suggestion for a remote entry

- **WHEN** the user selects an entry that maps to a remote transport
- **THEN** the Add-server sheet opens prefilled with the suggested URL, headers, and transport

#### Scenario: Missing runner warning

- **WHEN** the chosen package requires a runner not available on the Mac
- **THEN** the sheet is shown with the suggestion and a warning naming the missing runner

#### Scenario: Nothing is created until confirmed

- **WHEN** the user selects an entry but cancels the sheet
- **THEN** no hosted server is created

#### Scenario: Registry unreachable

- **WHEN** the registry browse or lookup fails
- **THEN** the app shows a clear error state with a retry instead of a broken list

#### Scenario: Secrets stay local

- **WHEN** the user enters secret values for the suggested environment variables or headers
- **THEN** they are masked by default and stored only in the connector's local config, never sent to Memory
