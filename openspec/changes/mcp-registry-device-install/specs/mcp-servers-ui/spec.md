## ADDED Requirements

### Requirement: Registry listing offers a device install for package-only entries

For each registry entry with no remote transport the listing SHALL offer an
"Install to device" action that opens a dialog rather than posting an install
directly. The dialog SHALL let the user pick a connected connector device and
submit the install to that device's instance id. When no device is connected the
dialog SHALL instead show how to connect one — signing in with
`memory-connector auth login` and starting the relay with
`--accept-managed-config` — and SHALL NOT offer a target picker. When the
connected-device list cannot be loaded the dialog SHALL report the failure and
SHALL NOT claim that no device is connected. An entry with a remote transport
SHALL keep its existing outbound install action and SHALL NOT offer a device
install.

#### Scenario: Package-only entry offers a device install

- **WHEN** the registry listing renders an entry with no remote transport
- **THEN** the entry shows an "Install to device" action whose dialog posts to the device install route

#### Scenario: Dialog picks a connected device

- **WHEN** the device install dialog renders while a connector device is connected
- **THEN** it offers each connected device as a selectable install target

#### Scenario: Dialog explains connector setup when no device is connected

- **WHEN** the device install dialog renders with no connected device
- **THEN** it shows the connector sign-in and relay commands and offers no target picker

#### Scenario: Failed device list is reported, not mistaken for no devices

- **WHEN** the connected-device list cannot be loaded
- **THEN** the dialog reports the failure and does not show the connector setup steps

#### Scenario: Remote entry keeps its outbound install

- **WHEN** the registry listing renders an entry with a remote transport
- **THEN** the entry shows the outbound install action and no device install action

### Requirement: Device install reports its outcome in the same way as an outbound install

A device install SHALL follow the same post/redirect/get pattern as the outbound
install. An install that still requires configuration SHALL redirect to that
server's configure form; an install with no required configuration SHALL redirect
to the connected devices page, which reports the install; and a rejected install
SHALL redirect back to the registry listing with an error and SHALL NOT report
success. A submission missing the registry name or the target device SHALL be
rejected by the gateway without calling the backend.

#### Scenario: Device install needing credentials goes to the configure form

- **WHEN** a device install creates a server with required configuration
- **THEN** the response redirects to that server's configure form

#### Scenario: Successful device install confirms on the devices page

- **WHEN** a device install creates a server with no required configuration
- **THEN** the response redirects to the connected devices page with an install confirmation

#### Scenario: Rejected device install reports on the registry listing

- **WHEN** the backend rejects a device install
- **THEN** the response redirects to the registry listing with an error and no install is reported

#### Scenario: Missing target device is rejected by the gateway

- **WHEN** a device install submission carries no target instance id
- **THEN** the gateway redirects with an error without calling the backend
