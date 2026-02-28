# template-packs Specification (Delta for EPF Template Pack)

This delta spec adds requirements for the Emergent Product Framework (EPF) Template Pack.

## ADDED Requirements

### Requirement: EPF Template Pack Object Types

The system SHALL provide a template pack implementing the Emergent Product Framework (EPF) methodology with 19 object types across READY, FIRE, and AIM phases.

#### Scenario: EPF READY Phase object types

- **GIVEN** the EPF template pack is installed
- **WHEN** a user views available READY phase object types
- **THEN** the system SHALL provide the following object types:
  - **NorthStar**: Strategic foundation with purpose, vision, mission, values, and core beliefs
  - **InsightAnalysis**: Market, customer, or technical insights with evidence chains
  - **StrategyFoundation**: Current reality assessment (SWOT), strategic bets, and success criteria
  - **InsightOpportunity**: Synthesized opportunities from insight patterns
  - **StrategyFormula**: Concrete strategic approaches with risk/reward analysis
  - **RoadmapRecipe**: Time-boxed OKRs organized by track with milestones and dependencies
- **AND** each object type SHALL have properties matching EPF schema requirements

#### Scenario: EPF FIRE Phase object types

- **GIVEN** the EPF template pack is installed
- **WHEN** a user views available FIRE phase object types
- **THEN** the system SHALL provide the following object types:
  - **ValueModel**: Hierarchical value decomposition with L1 layers, L2 components, and L3 sub-components
  - **FeatureDefinition**: Rich feature specs with personas, scenarios, value drivers, and requirements
- **AND** ValueModel SHALL support nested structures for layers → components → sub-components
- **AND** FeatureDefinition SHALL support embedded persona and scenario arrays

#### Scenario: EPF AIM Phase object types

- **GIVEN** the EPF template pack is installed
- **WHEN** a user views available AIM phase object types
- **THEN** the system SHALL provide the following object types:
  - **AssessmentReport**: Systematic evaluation of outcomes vs. intentions for completed cycles
  - **CalibrationMemo**: Course correction recommendations based on assessment learnings
- **AND** AssessmentReport SHALL track objectives assessment, assumptions validation, and learnings
- **AND** CalibrationMemo SHALL specify artifacts requiring updates

#### Scenario: EPF Supporting object types

- **GIVEN** the EPF template pack is installed
- **WHEN** a user views available supporting object types
- **THEN** the system SHALL provide the following object types:
  - **Objective**: Strategic objective within a roadmap (aspirational statement)
  - **KeyResult**: Measurable key result tied to an objective (quantitative)
  - **Assumption**: Explicit assumption requiring validation with criteria
  - **Track**: One of the four EPF tracks (Product, Strategy, OrgOps, Commercial)
  - **Persona**: User persona for feature development
  - **Milestone**: Key decision point, launch, or event requiring specific KR completion
  - **Capability**: Discrete, shippable unit of value within a feature
  - **Scenario**: End-to-end user flow with testable acceptance criteria
  - **Trend**: Directional shift in technology, market, user behavior, regulatory, or competitive dimensions
- **AND** Track name SHALL be constrained to enum: ['Product', 'Strategy', 'OrgOps', 'Commercial']
- **AND** KeyResult SHALL support confidence percentage (0-100)
- **AND** Milestone gate_type SHALL be constrained to enum: ['launch', 'review', 'funding', 'decision', 'demo', 'compliance']
- **AND** Trend category SHALL be constrained to enum: ['technology', 'market', 'user_behavior', 'regulatory', 'competitive']

### Requirement: EPF Template Pack Relationships

The system SHALL provide relationship types for connecting EPF artifacts across phases and tracks.

#### Scenario: EPF strategic relationships

- **GIVEN** the EPF template pack is installed
- **WHEN** users create objects and relationships
- **THEN** the system SHALL support the following strategic relationships:
  - NorthStar `GUIDES` StrategyFoundation, RoadmapRecipe, ValueModel
  - StrategyFoundation `SUPERSEDES` StrategyFoundation (versioning)
  - InsightAnalysis `INFORMS` InsightOpportunity, StrategyFormula, FeatureDefinition
  - InsightAnalysis `SYNTHESIZES_INTO` InsightOpportunity
  - InsightOpportunity `ADDRESSED_BY` StrategyFormula
  - RoadmapRecipe `IMPLEMENTS` StrategyFoundation, StrategyFormula

#### Scenario: EPF OKR relationships

- **GIVEN** the EPF template pack is installed
- **WHEN** users manage objectives and key results
- **THEN** the system SHALL support the following OKR relationships:
  - Objective `HAS_KEY_RESULT` KeyResult (one-to-many)
  - KeyResult `BELONGS_TO_OBJECTIVE` Objective (many-to-one)
  - KeyResult `TESTS_ASSUMPTION` Assumption (many-to-many)
  - KeyResult `DEPENDS_ON_KR` KeyResult with dependency_type attribute (requires/informs/enables)
  - RoadmapRecipe `CONTAINS_OBJECTIVE` Objective
  - RoadmapRecipe `HAS_MILESTONE` Milestone (one-to-many)
  - Milestone `REQUIRES_KR` KeyResult (many-to-many)

#### Scenario: EPF value and feature relationships

- **GIVEN** the EPF template pack is installed
- **WHEN** users work with value models and features
- **THEN** the system SHALL support the following relationships:
  - FeatureDefinition `CONTRIBUTES_TO` ValueModel with contribution_level attribute (primary/secondary/tertiary)
  - FeatureDefinition `TARGETS_PERSONA` Persona (many-to-many)
  - FeatureDefinition `REQUIRES_FEATURE` FeatureDefinition (dependencies)
  - FeatureDefinition `HAS_ASSUMPTION` Assumption
  - FeatureDefinition `HAS_CAPABILITY` Capability (one-to-many)
  - FeatureDefinition `HAS_SCENARIO` Scenario (one-to-many)
  - Scenario `EXERCISES_CAPABILITY` Capability (many-to-many)
  - Scenario `TARGETS_PERSONA` Persona (many-to-one)
  - Objective `BELONGS_TO_TRACK` Track
  - ValueModel `BELONGS_TO_TRACK` Track

#### Scenario: EPF insight and trend relationships

- **GIVEN** the EPF template pack is installed
- **WHEN** users work with insights and trends
- **THEN** the system SHALL support the following relationships:
  - InsightAnalysis `INCLUDES_TREND` Trend (one-to-many)
  - Trend `INFLUENCES_OPPORTUNITY` InsightOpportunity (many-to-many)
  - AssessmentReport `VALIDATES_TREND` Trend (one-to-many)

#### Scenario: EPF assessment relationships

- **GIVEN** the EPF template pack is installed
- **WHEN** users perform assessments and calibrations
- **THEN** the system SHALL support the following relationships:
  - AssessmentReport `ASSESSES` RoadmapRecipe (one-to-one)
  - AssessmentReport `VALIDATES` Assumption (one-to-many)
  - CalibrationMemo `RESPONDS_TO` AssessmentReport
  - CalibrationMemo `RECOMMENDS_UPDATE_TO` NorthStar, StrategyFoundation, RoadmapRecipe, ValueModel, FeatureDefinition

#### Scenario: EPF integration with Meeting pack

- **GIVEN** the EPF template pack is installed alongside Meeting & Decision Management pack
- **WHEN** meetings generate EPF artifacts
- **THEN** the system SHALL support the following cross-pack relationships:
  - Meeting `GENERATES` InsightAnalysis, Assumption
  - Meeting `DISCUSSES` FeatureDefinition, RoadmapRecipe, AssessmentReport

### Requirement: EPF Template Pack UI Configuration

The EPF template pack SHALL include UI configurations for all object types.

#### Scenario: EPF UI configurations

- **GIVEN** the EPF template pack is installed
- **WHEN** EPF objects are displayed in the UI
- **THEN** each object type SHALL have:
  - A distinct icon from the lucide icon set
  - A color associated with its EPF phase (indigo/purple for strategy, amber for insights, blue for roadmaps, cyan for value, green for features, orange for assessment, red for calibration)
  - A default view (card or list)
  - Configured list fields for summary display
  - Configured card fields for detailed display

### Requirement: EPF Template Pack Extraction Prompts

The EPF template pack SHALL include AI extraction prompts for all object types.

#### Scenario: EPF extraction prompts

- **GIVEN** the EPF template pack is installed
- **WHEN** a user extracts EPF artifacts from unstructured content
- **THEN** each object type SHALL have configured extraction prompts including:
  - System prompt adapted from EPF wizards (Pathfinder for insights, Synthesizer for opportunities, Product Architect for value models)
  - User prompt describing the extraction task
- **AND** extraction prompts SHALL identify EPF-specific fields and structures

### Requirement: EPF Template Pack Installation

The system SHALL enable installation of the EPF template pack through the standard template pack installation flow.

#### Scenario: EPF template pack in catalog

- **GIVEN** a user navigates to the template pack catalog
- **WHEN** they browse available template packs
- **THEN** the EPF template pack SHALL be displayed with:
  - Name: "Emergent Product Framework (EPF)"
  - Version: "1.0.0"
  - Description explaining READY-FIRE-AIM lifecycle and four-track model
  - Count of object types (19) and relationship types (29)
  - Source: "system"

#### Scenario: EPF template pack installation

- **GIVEN** a user selects the EPF template pack for installation
- **WHEN** the installation completes
- **THEN** all 19 object types SHALL be available for creating objects
- **AND** all 29 relationship types SHALL be available for connecting objects
- **AND** UI configurations SHALL be applied to display icons and colors
- **AND** extraction prompts SHALL be available for AI-assisted object creation

#### Scenario: EPF template pack idempotency

- **GIVEN** the EPF template pack has been installed
- **WHEN** the pack installation is run again (e.g., during upgrade)
- **THEN** the system SHALL update existing definitions without creating duplicates
- **AND** existing EPF objects SHALL remain intact
- **AND** new schema fields or relationship types SHALL be added

### Requirement: EPF Four-Track Model Support

The EPF template pack SHALL enforce and support the four-track braided model.

#### Scenario: Track enumeration

- **GIVEN** an EPF object type has a track field (Objective, ValueModel, etc.)
- **WHEN** a user sets the track value
- **THEN** the system SHALL constrain values to: ['Product', 'Strategy', 'OrgOps', 'Commercial']
- **AND** invalid track values SHALL be rejected with a validation error

#### Scenario: Track-based organization

- **GIVEN** a RoadmapRecipe is created
- **WHEN** objectives and key results are organized
- **THEN** the roadmap SHALL support organizing content by track
- **AND** cross-track dependencies SHALL be explicitly modeled via DEPENDS_ON_KR relationship

#### Scenario: Track health tracking

- **GIVEN** a Track object exists for each of the four tracks
- **WHEN** track health is assessed
- **THEN** the Track object SHALL support health_status field with values: ['healthy', 'attention-needed', 'at-risk']
- **AND** the Track object SHALL track owner and current focus

### Requirement: EPF Value Model Hierarchy

The EPF template pack SHALL support hierarchical value model structure.

#### Scenario: Three-level hierarchy

- **GIVEN** a ValueModel object is created
- **WHEN** the layers property is populated
- **THEN** the system SHALL support:
  - L1 Layers with id, name, description, and components array
  - L2 Components within layers with id, name, description, and sub_components array
  - L3 Sub-components within components with id, name, active flag, premium flag, and uvp (value proposition)

#### Scenario: Value model track association

- **GIVEN** a ValueModel is created
- **WHEN** the track_name property is set
- **THEN** the value model SHALL be associated with exactly one of: Product, Strategy, OrgOps, Commercial
- **AND** a complete EPF implementation SHOULD have four value models (one per track)

### Requirement: EPF OKR Structure

The EPF template pack SHALL support OKR methodology within roadmaps.

#### Scenario: Objective structure

- **GIVEN** an Objective is created
- **WHEN** the objective fields are populated
- **THEN** the objective SHALL have:
  - Unique objective_id (e.g., "obj-p-001")
  - Track assignment
  - Aspirational statement (qualitative, 20-300 chars)
  - Status tracking (not-started, in-progress, at-risk, completed, cancelled)

#### Scenario: Key result structure

- **GIVEN** a KeyResult is created
- **WHEN** the key result fields are populated
- **THEN** the key result SHALL have:
  - Unique kr_id (e.g., "kr-p-001")
  - Metric being measured
  - Baseline, target, and current values
  - Confidence percentage (0-100)
  - Status tracking (not-started, on-track, at-risk, off-track, achieved)

#### Scenario: KR dependency tracking

- **GIVEN** KeyResult A depends on KeyResult B
- **WHEN** the DEPENDS_ON_KR relationship is created
- **THEN** the relationship SHALL include dependency_type attribute
- **AND** dependency_type SHALL be one of: requires, informs, enables

### Requirement: EPF Milestone Structure

The EPF template pack SHALL support milestone tracking within roadmaps.

#### Scenario: Milestone structure

- **GIVEN** a Milestone is created
- **WHEN** the milestone fields are populated
- **THEN** the milestone SHALL have:
  - Unique milestone_id (e.g., "ms-001")
  - Milestone description (20-200 chars)
  - Target date
  - Gate type (launch, review, funding, decision, demo, compliance)
  - Status tracking (planned, at-risk, on-track, achieved, missed, deferred)
  - Optional success criteria and stakeholders

#### Scenario: Milestone-KR association

- **GIVEN** a Milestone requires certain key results to be completed
- **WHEN** the REQUIRES_KR relationship is created
- **THEN** the milestone SHALL be linked to one or more KeyResult objects
- **AND** milestone status SHOULD reflect the combined status of its required KRs

### Requirement: EPF Capability and Scenario Structure

The EPF template pack SHALL support decomposing features into capabilities and scenarios.

#### Scenario: Capability structure

- **GIVEN** a Capability is created
- **WHEN** the capability fields are populated
- **THEN** the capability SHALL have:
  - Unique capability_id (e.g., "cap-001")
  - Title in verb-noun format
  - Description (minimum 30 chars)
  - Status tracking (planned, in-development, testing, released, deprecated)
  - Priority (critical, high, medium, low)
  - Optional acceptance criteria, inputs, outputs, and constraints

#### Scenario: Scenario structure

- **GIVEN** a Scenario is created
- **WHEN** the scenario fields are populated
- **THEN** the scenario SHALL have:
  - Unique scenario_id (e.g., "scn-001")
  - Actor (matching a Persona)
  - Context, trigger, action, and outcome
  - Acceptance criteria (at least one)
  - Test status (not-tested, passing, failing, blocked)
  - Optional JTBD category and priority

#### Scenario: Capability-Scenario association

- **GIVEN** a Scenario exercises certain capabilities
- **WHEN** the EXERCISES_CAPABILITY relationship is created
- **THEN** the scenario SHALL be linked to one or more Capability objects
- **AND** users SHOULD be able to trace which scenarios test which capabilities

### Requirement: EPF Trend Structure

The EPF template pack SHALL support tracking trends that inform strategic decisions.

#### Scenario: Trend structure

- **GIVEN** a Trend is created
- **WHEN** the trend fields are populated
- **THEN** the trend SHALL have:
  - Unique trend_id (e.g., "trend-tech-001")
  - Category (technology, market, user_behavior, regulatory, competitive)
  - Trend description (20-300 chars)
  - Timeframe (happening-now, 6-12-months, 1-2-years, 2-5-years)
  - Impact assessment (30-500 chars)
  - Confidence level (high, medium, low)
  - Evidence sources (at least one)
  - Tracks affected

#### Scenario: Trend-Insight association

- **GIVEN** an InsightAnalysis identifies trends
- **WHEN** the INCLUDES_TREND relationship is created
- **THEN** the insight SHALL be linked to one or more Trend objects
- **AND** trends SHALL be queryable independently for strategic planning

#### Scenario: Trend validation

- **GIVEN** an AssessmentReport evaluates trend accuracy
- **WHEN** the VALIDATES_TREND relationship is created
- **THEN** the assessment SHALL be linked to trends being validated
- **AND** trend confidence levels MAY be updated based on assessment findings
