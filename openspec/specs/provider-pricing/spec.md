# provider-pricing Specification

## Purpose
`kb.provider_pricing` holds the global retail rate per `(provider, model)` used by usage/cost views. It is seeded from code and refreshed by a daily sync. This capability defines how those rates are sourced for every registered vendor, and how vendors/models with no sourced rate are handled.

## Requirements

### Requirement: Rates cover registered vendors from a sourced catalog

The pricing sync SHALL source retail rates from the models.dev catalog feed for every registered vendor that the feed prices, in addition to the embedded baseline rows. Synchronized rates SHALL be per 1M tokens in USD and SHALL NOT be invented: a rate is written only when the feed publishes one for a mapped `(vendor, model)`.

#### Scenario: Newly registered vendor resolves a sourced rate

- **WHEN** a vendor added to the registry has models.dev entries with published rates and a usage event records a listed model
- **THEN** cost resolution returns a non-zero cost derived from that sourced rate

#### Scenario: Registry dialect maps to the feed's provider slug

- **WHEN** the sync runs
- **THEN** each registered vendor is looked up by its declared models.dev provider slug(s), and models are written under the registry dialect as the pricing `provider` key

### Requirement: Unpriced vendors and models fall through transparently

A vendor or model for which no rate is sourced SHALL resolve to the existing $0 default. The system SHALL NOT store or apply a placeholder rate that would present an unknown cost as a known one.

#### Scenario: Unknown model resolves to zero

- **WHEN** a usage event records a model with no sourced rate
- **THEN** cost resolution returns $0 rather than borrowing an unrelated rate

#### Scenario: Unmapped vendor is documented

- **WHEN** a registered vendor has no models.dev catalog entry or only unpriced entries
- **THEN** its usage resolves to $0 and the vendor is recorded in the sync's mapping documentation as intentionally unpriced

### Requirement: Cost-resolution precedence is unchanged

Source priority SHALL remain: project override → global exact `(provider, model)` → global model-only → normalized model name → $0.

#### Scenario: Project override still wins

- **WHEN** a project has a manual rate for a `(provider, model)` that also has a sourced global rate
- **THEN** the project override is used

### Requirement: Feed failure falls back to the embedded baseline

The embedded static pricing list SHALL always be upserted, and the models.dev rates SHALL overlay it. A feed fetch or parse failure SHALL NOT fail the sync, mutate resolution semantics, or remove already-known baseline rates.

#### Scenario: Feed unavailable

- **WHEN** the models.dev fetch fails or returns no usable rates
- **THEN** the sync writes the embedded baseline, logs the failure, and returns without error

### Requirement: Feed overlay is presence-aware per rate dimension

The models.dev overlay SHALL overwrite only the rate dimensions a feed entry actually publishes (text input, output, audio input). Dimensions the feed omits SHALL keep the embedded baseline (or previously stored) value; the overlay SHALL NOT zero a dimension merely because the feed does not publish it.

#### Scenario: Feed publishes text rates only

- **WHEN** a baseline model with image/audio rates is overlaid by a feed entry that publishes only `input` and `output`
- **THEN** the text-input and output rates are updated from the feed and the image/audio rates keep their baseline values

#### Scenario: Published dimension is updated

- **WHEN** the feed publishes a new value for a dimension the model already has
- **THEN** that dimension is overwritten with the feed value

#### Scenario: Explicit zero versus unpriced entry

- **WHEN** a feed entry publishes `0` for a rate dimension
- **THEN** that dimension is overwritten with `0` (an explicit zero is a sourced rate, not an omission), except when every published dimension is `0`, in which case the entry is treated as unpriced and dropped so it neither overwrites a baseline rate nor stores a placeholder

### Requirement: Vendor slug prefers the default endpoint's region

A registry vendor whose default endpoint is mainland China SHALL prefer the feed's `-cn` region slug over the international slug, and an international default SHALL prefer the global slug. The sync SHALL document that overriding a vendor's base URL to another region still resolves the default-region rate (per-instance regional rates are out of scope).

#### Scenario: Mainland-default vendor resolves the mainland rate

- **WHEN** both the international and `-cn` feed slugs list the same model for a vendor whose default base URL is mainland
- **THEN** the resolved rate is the `-cn` (mainland) rate

### Requirement: Stale synced rows are pruned only after a successful feed parse

After a models.dev fetch and parse succeed, the sync SHALL remove managed-vendor rows whose `(vendor, model)` the feed no longer publishes (removed or now $0). A fetch or parse failure SHALL NOT prune any row, so an outage never wipes already-known rates. The always-upserted embedded baseline SHALL be exempt from pruning.

#### Scenario: Feed drops a model

- **WHEN** a previously published managed-vendor model is absent from a successfully parsed feed
- **THEN** its row is removed from `kb.provider_pricing`

#### Scenario: Fetch fails

- **WHEN** the feed fetch fails
- **THEN** no rows are pruned and the existing rates are retained
