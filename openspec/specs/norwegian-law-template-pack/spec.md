## ADDED Requirements

### Requirement: Template pack YAML defines all object types
The `packs/norwegian-law.yaml` file SHALL define exactly 7 object types matching what the seeder ingests: `Law`, `Regulation`, `Ministry`, `LegalArea`, `LegalParagraph`, `EUDirective`, and `EuroVocConcept`. Each type SHALL have a `name`, `label`, `description`, and a `properties` map where each property has a `type` (`string` or `number`) and a `description`.

#### Scenario: Law object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `Law` object type exists with properties: `name`, `title`, `ref_id`, `doc_id`, `short_title`, `legacy_id`, `language`, `date_in_force`, `year_in_force`, `decade_in_force`, `last_change_in_force`, `date_of_publication`, `applies_to`, `eea_references`, `content`

#### Scenario: Regulation object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `Regulation` object type exists with the same properties as `Law` (same schema, different label/description)

#### Scenario: Ministry object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `Ministry` object type exists with property `name` (string)

#### Scenario: LegalArea object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `LegalArea` object type exists with properties: `name` (string), `parent_area` (string, for sub-areas)

#### Scenario: LegalParagraph object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `LegalParagraph` object type exists with properties: `name`, `content`, `section_id`, `paragraph_num`, `law_ref_id`, `position` (number), `chapter_id`, `title`

#### Scenario: EUDirective object type
- **WHEN** the template pack YAML is loaded
- **THEN** an `EUDirective` object type exists with properties: `name`, `celex_id`, `directive_id`, `full_title`, `form`, `date_of_document`, `date_of_effect`, `author`, `subject_matter`, `oj_reference`, `content`

#### Scenario: EuroVocConcept object type
- **WHEN** the template pack YAML is loaded
- **THEN** a `EuroVocConcept` object type exists with properties: `name`, `eurovoc_id`, `label_en`

### Requirement: Template pack YAML defines all relationship types
The `packs/norwegian-law.yaml` file SHALL define exactly 13 relationship types. Each relationship type SHALL have a `name`, `label`, `description`, `sourceType`, and `targetType`.

#### Scenario: Law administration relationship
- **WHEN** the template pack YAML is loaded
- **THEN** an `ADMINISTERED_BY` relationship type exists with sourceType `Law` or `Regulation` and targetType `Ministry`

#### Scenario: Legal area classification relationships
- **WHEN** the template pack YAML is loaded
- **THEN** an `IN_LEGAL_AREA` relationship type exists with sourceType `Law` or `Regulation` and targetType `LegalArea`

#### Scenario: Amendment relationships
- **WHEN** the template pack YAML is loaded
- **THEN** `AMENDED_BY` (Law/Regulation → Law/Regulation) and `AMENDS` (Law/Regulation → Law/Regulation) relationship types exist

#### Scenario: Cross-reference relationships
- **WHEN** the template pack YAML is loaded
- **THEN** `SEE_ALSO` and `REFERENCES` relationship types exist between Law/Regulation nodes

#### Scenario: Language variant relationship
- **WHEN** the template pack YAML is loaded
- **THEN** a `HAS_LANGUAGE_VARIANT` relationship type exists linking the same law in different languages

#### Scenario: EEA implementation relationship
- **WHEN** the template pack YAML is loaded
- **THEN** an `IMPLEMENTS_EEA` relationship type exists with sourceType `Law` or `Regulation` and targetType `EUDirective`

#### Scenario: Paragraph containment relationship
- **WHEN** the template pack YAML is loaded
- **THEN** a `HAS_PARAGRAPH` relationship type exists with sourceType `Law` or `Regulation` and targetType `LegalParagraph`

#### Scenario: EU law citation relationship
- **WHEN** the template pack YAML is loaded
- **THEN** a `CITES_EU_LAW` relationship type exists with sourceType `Law` or `Regulation` and targetType `EUDirective`

#### Scenario: EU directive chain relationships
- **WHEN** the template pack YAML is loaded
- **THEN** `EU_CITES` and `EU_MODIFIED_BY` relationship types exist between `EUDirective` nodes

#### Scenario: EuroVoc descriptor relationship
- **WHEN** the template pack YAML is loaded
- **THEN** a `HAS_EUROVOC_DESCRIPTOR` relationship type exists with sourceType `EUDirective` and targetType `EuroVocConcept`

### Requirement: Template pack metadata is complete
The YAML file SHALL include top-level metadata fields: `name`, `version`, `description`, `author`, `license`, and `repositoryUrl`.

#### Scenario: Pack metadata present
- **WHEN** the template pack YAML is parsed
- **THEN** all six metadata fields are non-empty strings
