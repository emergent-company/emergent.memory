# EPF Template Pack Tasks

## Phase 1: Core Implementation

### 1.1 Create EPF Template Pack Seed File

- [ ] Create `apps/server/src/modules/template-packs/seeds/epf-pack.seed.ts`
- [ ] Define EPF_PACK_ID constant (UUID)
- [ ] Implement all 15 object type schemas
- [ ] Implement all 22 relationship type schemas
- [ ] Implement UI configurations for all types
- [ ] Implement extraction prompts for all types
- [ ] Add seed function with upsert logic
- [ ] Add pack metadata (name, version, description, author)

### 1.2 Object Type Schemas

#### READY Phase

- [ ] Implement NorthStar schema
- [ ] Implement InsightAnalysis schema
- [ ] Implement StrategyFoundation schema
- [ ] Implement InsightOpportunity schema
- [ ] Implement StrategyFormula schema
- [ ] Implement RoadmapRecipe schema

#### FIRE Phase

- [ ] Implement ValueModel schema (with nested layers/components/sub-components)
- [ ] Implement FeatureDefinition schema (with personas, scenarios, value drivers)

#### AIM Phase

- [ ] Implement AssessmentReport schema
- [ ] Implement CalibrationMemo schema

#### Supporting Artifacts

- [ ] Implement Objective schema
- [ ] Implement KeyResult schema
- [ ] Implement Assumption schema
- [ ] Implement Track schema
- [ ] Implement Persona schema

### 1.3 Relationship Type Schemas

- [ ] Implement North Star relationships (GUIDES, SUPERSEDES)
- [ ] Implement Insight relationships (INFORMS, SYNTHESIZES_INTO, ADDRESSED_BY)
- [ ] Implement Strategy relationships (IMPLEMENTS)
- [ ] Implement OKR relationships (HAS_KEY_RESULT, BELONGS_TO_OBJECTIVE, TESTS_ASSUMPTION, DEPENDS_ON_KR)
- [ ] Implement Value/Feature relationships (CONTRIBUTES_TO, TARGETS_PERSONA, REQUIRES_FEATURE, HAS_ASSUMPTION, CONTAINS_LAYER)
- [ ] Implement Roadmap relationships (CONTAINS_OBJECTIVE, HAS_MILESTONE)
- [ ] Implement Assessment relationships (ASSESSES, VALIDATES)
- [ ] Implement Calibration relationships (RESPONDS_TO, RECOMMENDS_UPDATE_TO)
- [ ] Implement Track relationship (BELONGS_TO_TRACK)
- [ ] Implement Integration relationships (GENERATES, DISCUSSES, RELATES_TO)

### 1.4 UI Configurations

- [ ] Configure icons (lucide icon names) for all 15 types
- [ ] Configure colors (hex) for all types by phase/category
- [ ] Configure defaultView (card/list) for all types
- [ ] Configure listFields for all types
- [ ] Configure cardFields for all types

### 1.5 Extraction Prompts

- [ ] Create system prompts for all extractable types
- [ ] Create user prompts for all extractable types
- [ ] Adapt prompts from EPF wizards:
  - [ ] Pathfinder wizard → InsightAnalysis prompts
  - [ ] Synthesizer wizard → InsightOpportunity prompts
  - [ ] Product Architect wizard → ValueModel, FeatureDefinition prompts
  - [ ] Scout wizard → Assumption, Risk prompts

## Phase 2: Integration & Testing

### 2.1 Seed Integration

- [ ] Add EPF pack to seed runner (`apps/server/src/modules/template-packs/seeds/index.ts`)
- [ ] Ensure seed can be run independently
- [ ] Test seed idempotency (can run multiple times safely)

### 2.2 Schema Validation

- [ ] Validate object type schemas against EPF JSON schemas
- [ ] Ensure required fields match EPF requirements
- [ ] Verify enum values match EPF allowed values
- [ ] Test nested object structures (ValueModel layers, FeatureDefinition personas)

### 2.3 Relationship Testing

- [ ] Test relationship cardinality constraints
- [ ] Verify relationship attributes work correctly
- [ ] Test cross-type relationships

### 2.4 Extraction Testing

- [ ] Test NorthStar extraction from strategy documents
- [ ] Test InsightAnalysis extraction from research notes
- [ ] Test FeatureDefinition extraction from PRDs
- [ ] Test RoadmapRecipe extraction from planning sessions
- [ ] Test AssessmentReport extraction from retrospectives

## Phase 3: Documentation

### 3.1 User Documentation

- [ ] Document EPF methodology overview
- [ ] Document each object type with examples
- [ ] Document relationships and when to use them
- [ ] Create getting started guide for EPF template pack

### 3.2 Developer Documentation

- [ ] Document seed file structure
- [ ] Document schema extension points
- [ ] Document extraction prompt customization

## Phase 4: Future Enhancements (Out of Scope for Initial Release)

### 4.1 EPF-Specific Views

- [ ] Strategy Canvas view (North Star + Strategy Foundation)
- [ ] OKR Tree view (Roadmap → Objectives → Key Results)
- [ ] Value Model hierarchy visualization
- [ ] Four-track dashboard

### 4.2 Advanced Extraction

- [ ] Multi-artifact extraction from meeting transcripts
- [ ] Batch extraction for onboarding existing EPF content
- [ ] Relationship inference from content

### 4.3 EPF Workflow Integration

- [ ] READY→FIRE→AIM phase transitions
- [ ] Assumption validation workflows
- [ ] Assessment scheduling and reminders

## Dependencies

- Template pack system fully implemented (existing)
- Graph object/relationship types support nested schemas (existing)
- Extraction pipeline supports template pack prompts (existing)

## Risks & Mitigations

| Risk                                                        | Mitigation                                                     |
| ----------------------------------------------------------- | -------------------------------------------------------------- |
| Complex nested schemas (ValueModel) may hit database limits | Test with realistic data, consider schema flattening if needed |
| Too many object types for users to understand               | Provide onboarding wizard, start with core types               |
| Extraction prompts may need tuning                          | Include prompt iteration in testing phase                      |
| EPF updates may require pack updates                        | Version the pack, document EPF version compatibility           |

## Acceptance Criteria

1. [ ] EPF template pack installs successfully via seed
2. [ ] All 15 object types can be created with valid data
3. [ ] All 22 relationship types can be created between appropriate objects
4. [ ] UI displays correct icons and colors for all types
5. [ ] Extraction correctly identifies EPF artifacts from sample content
6. [ ] Pack passes schema validation
7. [ ] Documentation enables users to start using EPF methodology
