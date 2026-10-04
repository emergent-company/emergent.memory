# provider-extra-config Specification

## Purpose
Persist, round-trip, and apply vendor-specific provider configuration fields declared by the provider registry (`ExtraField`), so Azure OpenAI's dated endpoint, Volcengine Ark's region, and similar vendor inputs actually take effect.

## Requirements

### Requirement: Persist vendor extra config

The provider config store SHALL persist non-secret extra field values in a queryable `jsonb` map and secret extra field values (`ExtraField.Secret`) encrypted at rest, never in plaintext.

#### Scenario: Secret field encrypted at rest

- **WHEN** a provider config is saved with a declared secret extra field
- **THEN** the secret value is stored only as ciphertext in a dedicated encrypted column and appears in neither the plaintext extra-config column nor the ciphertext bytes

#### Scenario: Secret field decrypted on resolution

- **WHEN** a stored provider config with a secret extra field is resolved
- **THEN** the resolved credential's extra map contains the decrypted secret value

#### Scenario: Non-secret field stored plainly

- **WHEN** a provider config is saved with a non-secret extra field
- **THEN** the value is stored in the plaintext extra-config map and returned in the public-safe provider config response

### Requirement: Round-trip extra config through the API

The provider upsert request and public-safe responses SHALL carry an `extraConfig` map; secret fields SHALL never be serialized in responses.

#### Scenario: Omit preserves stored extras

- **WHEN** an upsert omits `extraConfig`
- **THEN** the previously stored extra config is preserved unchanged

#### Scenario: Blank secret preserves, blank non-secret clears

- **WHEN** an upsert sends an empty value for a declared secret extra field
- **THEN** the stored secret is preserved

- **WHEN** an upsert sends an empty value for a non-secret extra field
- **THEN** that key is removed from the stored non-secret config

#### Scenario: Response excludes secrets

- **WHEN** a provider config with a stored secret extra field is read via the API
- **THEN** the response's `extraConfig` omits the secret field

### Requirement: Apply extra config at request time

Resolved credentials SHALL carry extra config, and outbound provider requests SHALL apply it.

#### Scenario: Azure api_version appended to request URL

- **WHEN** an Azure OpenAI credential with `api_version` set issues a chat-completions or embeddings request
- **THEN** the request URL includes `api-version=<value>`

#### Scenario: Volcengine region substitutes the base URL host

- **WHEN** a Volcengine Ark credential has `region` set
- **THEN** the resolved base URL uses that region as the `ark.<region>.volces.com` host segment

#### Scenario: Declared header applied

- **WHEN** a vendor extra field declares an HTTP header and a value is configured
- **THEN** every outbound request for that credential carries that header

### Requirement: Edit extra config in the providers UI

The providers settings form SHALL render editable inputs for each declared extra field and submit them as `extraConfig`.

#### Scenario: Fields rendered

- **WHEN** the user opens the provider form for a vendor declaring extra fields
- **THEN** each field renders an input matching its type (string, password, number, boolean, select), with non-secret values prefilled and secrets left blank

#### Scenario: Submitted values persist

- **WHEN** the user saves the form with extra field values entered
- **THEN** the values are sent as `extraConfig` and persisted

#### Scenario: CLI round-trip

- **WHEN** `memory provider configure-project <vendor> --extra key=value` is run
- **THEN** the key/value is sent in `extraConfig` and stored on the provider config
