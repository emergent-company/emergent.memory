## ADDED Requirements

### Requirement: Servers missing required configuration are flagged and cannot be enabled

The gateway SHALL render a server whose required configuration is incomplete with a
visible "Needs configuration" indication naming the missing keys, and SHALL offer a
"Configure" action that takes the user to that server's edit form. The memory API
SHALL reject a request that enables such a server (`400`), naming the missing keys. A
required key is missing when its value is blank and no secret is stored for it; a key
marked `isSecret` counts as required.

#### Scenario: List flags an unconfigured server

- **WHEN** a registry-installed server has one or more blank required keys
- **THEN** its list row shows a "Needs configuration" indication naming those keys and a "Configure" action linking to its edit page

#### Scenario: Enable is rejected until configured

- **WHEN** a client or the list toggle attempts to enable a server whose required keys are blank
- **THEN** the memory API rejects the request with `400` naming the missing keys and the toggle does not turn on

#### Scenario: Configured server enables normally

- **WHEN** every required key has a value (plaintext or stored secret)
- **THEN** enabling the server succeeds

### Requirement: Required configuration rows are marked on the edit form

The create/edit form SHALL mark env/header rows that correspond to a server's required
configuration and SHALL show a banner naming the missing keys when the page is opened
for configuration (`?configure=1`). A never-configured secret row SHALL prompt for a
value rather than describe a "leave blank to keep" contract that does not yet apply.

#### Scenario: Install guides straight to configuration

- **WHEN** a registry install creates a server with required configuration
- **THEN** the user is redirected to that server's edit page with the required rows marked and the missing keys named

#### Scenario: Never-configured secret prompts for a value

- **WHEN** the edit form renders a required secret key that has no stored value
- **THEN** its value input prompts the user to enter the value

#### Scenario: Stored secret keeps the leave-blank contract

- **WHEN** the edit form renders a required secret key that already has a stored value
- **THEN** its value input indicates that leaving it blank keeps the stored value

### Requirement: Create and edit forms offer a save-less connection test

The create and edit forms SHALL offer a "Test connection" action that probes the
currently entered configuration (including unsaved values) and reports the result
inline without persisting the server. In edit mode, a required secret left blank SHALL
be tested with the server's stored secret, if any. The probe SHALL be ephemeral and
SHALL not modify the stored server or its cached tools.

#### Scenario: Test reports success before saving

- **WHEN** a user enters a valid configuration and clicks "Test connection"
- **THEN** the form reports a successful probe (server identity and discovered tools) and no server record is created or modified

#### Scenario: Test reports failure before saving

- **WHEN** a user enters an invalid configuration and clicks "Test connection"
- **THEN** the form reports the probe failure inline and no server record is created or modified

#### Scenario: Edit-mode test uses the stored secret

- **WHEN** a user opens an existing server's edit form, leaves the stored secret blank, and clicks "Test connection"
- **THEN** the probe uses the stored secret rather than an empty value
