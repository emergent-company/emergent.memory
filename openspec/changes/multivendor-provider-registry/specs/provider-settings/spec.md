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

The surface SHALL render the vendor's declared extra configuration fields (label, type, required, secret, placeholder) as form inputs, and SHALL round-trip their values through the provider configuration API without a per-vendor UI branch.

#### Scenario: Azure api-version field

- **WHEN** the Azure OpenAI vendor is selected
- **THEN** an `api-version` input is rendered from its registry metadata

#### Scenario: Secret extra field masked

- **WHEN** an extra field is marked secret
- **THEN** its stored value is never echoed back to the client
