## Purpose

The Project Settings provider surface is generated from the provider registry: the set of selectable vendors, their branding, and their vendor-specific configuration inputs all come from registry metadata rather than a hardcoded whitelist.

## ADDED Requirements

### Requirement: Provider list is registry-driven

The provider configuration surface SHALL present the vendors returned by the provider registry. It SHALL NOT maintain a separate hardcoded vendor whitelist.

#### Scenario: New vendor appears without UI change

- **WHEN** a vendor is added to the registry
- **THEN** it appears in the provider configuration surface with no gateway code change

#### Scenario: Unknown vendor is rejected

- **WHEN** a configuration request names a vendor absent from the registry
- **THEN** the request is rejected

### Requirement: Vendor branding rendered

The surface SHALL display each vendor's registry-provided display name and icon.

#### Scenario: Branding present

- **WHEN** the provider list renders
- **THEN** each vendor shows its display name and, when provided, its icon

### Requirement: Dynamic vendor configuration fields

The surface SHALL surface the vendor's declared extra configuration fields (key, label, type, required, secret) as registry-driven metadata, without a per-vendor UI branch. Persisting submitted values and round-tripping them through the provider configuration API is deferred: the panel renders the fields read-only and does not submit them.

#### Scenario: Azure api-version field

- **WHEN** the Azure OpenAI vendor is selected
- **THEN** its `api-version` registry metadata is rendered read-only from the definition

#### Scenario: Secret extra field masked

- **WHEN** an extra field is marked secret
- **THEN** only its key/label metadata is rendered and no stored value is echoed back to the client
