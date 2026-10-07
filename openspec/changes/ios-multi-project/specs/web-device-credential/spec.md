## MODIFIED Requirements

### Requirement: Setup exchanges the one-time token for the device credential

`POST /api/setup` SHALL exchange the one-time setup token for the device credential. The setup token, its target project identity, and the pre-minted device credential SHALL be held in a gateway-local, in-process store keyed by the token — NOT in a memory project setting — so the exchange performs no memory call and therefore succeeds with no session. The token SHALL be minted on the session-authenticated devices page, which also mints the device credential server-side (`CreateDeviceToken`) and records it in the store for a single claim. Consuming the token SHALL be single-use and SHALL return the `emt_*` value as `apiKey` plus the stored `orgID`, `projectID`, and `projectName`. On rotation or expiry of an unclaimed token, the store SHALL revoke the orphaned credential through the session-authenticated revoke path; a claimed credential SHALL never be revoked. The retired 64-hex `ios_device_keys` registry SHALL authenticate nothing; the optional device manifest SHALL be accepted but no longer persisted.

#### Scenario: Setup returns the device credential

- **WHEN** a client exchanges a valid one-time setup token
- **THEN** the response `apiKey` is the `emt_*` device credential minted at QR time

#### Scenario: Setup returns the onboarding project identity

- **WHEN** a client exchanges a valid one-time setup token
- **THEN** the response carries the `orgID`, `projectID`, and `projectName` the QR was minted under

#### Scenario: Session-less exchange succeeds

- **WHEN** a device with no session exchanges a valid one-time setup token
- **THEN** the exchange returns 200 (the gateway makes no memory call to consume the token)

#### Scenario: Setup fails closed without a bound credential

- **WHEN** a setup token has no bound device credential (e.g. minted without a session)
- **THEN** the exchange returns 401, never an empty bearer

#### Scenario: Setup token is single-use

- **WHEN** a setup token is exchanged a second time
- **THEN** the second attempt returns 401

#### Scenario: Registry key authenticates nothing

- **WHEN** a client presents a retired 64-hex registry key
- **THEN** the gateway rejects it; only the session or a `device:api` credential authenticates

## ADDED Requirements

### Requirement: Setup QR carries the onboarding project

The setup QR payload SHALL carry the project the onboarding is bound to — `orgID`, `projectID`, and `projectName` — resolved from the project scope of the session-authenticated devices page that minted it. A QR minted under project A SHALL NOT be presented as belonging to any other project.

#### Scenario: QR identifies its project

- **WHEN** the devices page for project A renders the setup QR
- **THEN** the QR payload names project A's `orgID`, `projectID`, and `projectName`

#### Scenario: QR without project identity is not produced

- **WHEN** the devices page has no resolved project scope
- **THEN** the gateway does not mint a setup QR with a missing project identity
