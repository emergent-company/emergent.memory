## Purpose

Tracks the devices that have registered with the backend and records what each device reports about itself — platform, name, model, OS, and app version — so the admin can identify and manage every connected device.

## ADDED Requirements

### Requirement: Device self-introduction on registration

The gateway SHALL accept an optional device manifest when it issues a per-device key and SHALL store that manifest with the registration record.

#### Scenario: Device supplies a manifest

- **WHEN** a client exchanges a valid setup token and includes a device manifest in the request
- **THEN** the gateway stores the manifest fields alongside the issued key

#### Scenario: Legacy client omits the manifest

- **WHEN** a client exchanges a valid setup token without a device manifest
- **THEN** the gateway still issues a key and records the registration without device metadata

### Requirement: Device manifest fields

The device manifest SHALL carry the platform (system type), device name, hardware model, OS name, OS version, and app version. The gateway SHALL persist each provided field and treat all fields as optional.

#### Scenario: Full manifest

- **WHEN** a client reports platform, name, model, OS name, OS version, and app version
- **THEN** all reported fields are available in the registry record

#### Scenario: Partial manifest

- **WHEN** a client reports only a subset of manifest fields
- **THEN** the gateway stores the provided fields and leaves the rest unset

### Requirement: Server-derived registration timestamps

The gateway SHALL record the registration time itself and SHALL NOT trust client-supplied timestamps.

#### Scenario: Registration records server time

- **WHEN** a device registers
- **THEN** the registration record carries the gateway's own clock time at issuance

### Requirement: Platform is stored, not assumed

The registry SHALL store a platform identifier for each device and MUST NOT assume every device is an iOS device.

#### Scenario: Non-iOS platform

- **WHEN** a device registers reporting a platform other than iOS
- **THEN** the registry records that platform and the settings UI shows it rather than labeling it iOS

### Requirement: Devices are distinguishable in the settings UI

The Devices settings page SHALL display, for each registered device, the device name (when available) or a model/OS fallback, its platform, and OS version, and SHALL fall back to the masked key when no metadata is present.

#### Scenario: Device with metadata

- **WHEN** a registered device has manifest metadata
- **THEN** its row shows the device name, platform, and OS version without exposing the full key

#### Scenario: Device without metadata

- **WHEN** a registered device has no manifest metadata
- **THEN** its row shows the masked key and registration time

### Requirement: Revoking a device cuts its access

The settings UI SHALL let the admin revoke a device, after which that device's key is no longer accepted.

#### Scenario: Revoke

- **WHEN** the admin revokes a registered device
- **THEN** the device's key no longer authenticates and the device is removed from the list
