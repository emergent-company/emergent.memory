## Purpose

The usage dashboard breaks token usage down by model. Once a project can run more than one provider instance of the same dialect, usage must also distinguish the instance, otherwise two endpoints are silently merged.

## MODIFIED Requirements

### Requirement: Show per-model usage

The usage dashboard SHALL display token usage broken down by model when the backend provides model-level data. When a project uses more than one provider instance, the breakdown SHALL distinguish the provider instance (slug) and the dialect so usage is not merged across instances.

#### Scenario: Model breakdown present

- **WHEN** the usage dashboard loads and model-level usage is available
- **THEN** token usage is shown grouped by model, and each group identifies the provider instance and dialect that served it

#### Scenario: Two instances, same model name

- **WHEN** two provider instances of the same dialect both served a model with the same name
- **THEN** their usage appears as separate groups distinguished by provider instance

#### Scenario: Model breakdown absent

- **WHEN** the backend returns no model-level breakdown
- **THEN** the per-model section is omitted, and the rest of the dashboard remains usable
