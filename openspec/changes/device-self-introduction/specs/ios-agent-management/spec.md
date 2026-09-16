## MODIFIED Requirements

### Requirement: Authenticate via QR code

The app SHALL let the user authenticate to the backend by scanning a QR code that carries the server URL and API key; the QR flow MUST NOT configure an agent. During the setup exchange the app SHALL include a device manifest describing the platform, device name, hardware model, OS name and version, and app version.

#### Scenario: QR authenticates the app

- **WHEN** the user scans a valid setup QR code
- **THEN** the app stores the server URL and API key and can reach the backend, without creating or changing an agent

#### Scenario: Device introduces itself on setup

- **WHEN** the app exchanges a valid setup token
- **THEN** the app sends a device manifest with its platform, device name, model, OS version, and app version

#### Scenario: Invalid QR

- **WHEN** the scanned QR is malformed or lacks the required fields
- **THEN** the app reports the failure and keeps the previous configuration
