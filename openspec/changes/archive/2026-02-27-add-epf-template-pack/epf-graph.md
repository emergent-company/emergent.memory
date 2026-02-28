# EPF Template Pack - Object & Relationship Graph

## Visual Overview

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    READY PHASE                                          │
│                              (Strategic Foundation)                                     │
└─────────────────────────────────────────────────────────────────────────────────────────┘

                                    ┌─────────────┐
                                    │  NorthStar  │
                                    │   (root)    │
                                    └──────┬──────┘
                                           │ GUIDES
                          ┌────────────────┼────────────────┐
                          ▼                ▼                ▼
                 ┌─────────────────┐ ┌───────────┐ ┌─────────────┐
                 │StrategyFoundation│ │RoadmapRecipe│ │ ValueModel  │
                 └────────┬────────┘ └─────┬─────┘ └──────┬──────┘
                          │                │              │
                          │ SUPERSEDES     │              │ BELONGS_TO_TRACK
                          ▼ (versioning)   │              ▼
                 ┌─────────────────┐       │        ┌─────────┐
                 │StrategyFoundation│      │        │  Track  │
                 │   (older ver)   │       │        └─────────┘
                 └─────────────────┘       │
                                           │
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                  INSIGHT FLOW                                           │
└─────────────────────────────────────────────────────────────────────────────────────────┘

    ┌─────────────────┐                    ┌─────────────────┐
    │ InsightAnalysis │───INCLUDES_TREND──▶│      Trend      │
    └────────┬────────┘                    └────────┬────────┘
             │                                      │
             │ INFORMS                              │ INFLUENCES_OPPORTUNITY
             │ SYNTHESIZES_INTO                     ▼
             ▼                              ┌─────────────────┐
    ┌─────────────────┐◀───────────────────│InsightOpportunity│
    │InsightOpportunity│                    └─────────────────┘
    └────────┬────────┘
             │ ADDRESSED_BY
             ▼
    ┌─────────────────┐
    │ StrategyFormula │
    └────────┬────────┘
             │
             │ IMPLEMENTS (reverse)
             ▼
    ┌─────────────────┐
    │  RoadmapRecipe  │
    └─────────────────┘


┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    FIRE PHASE                                           │
│                                   (Execution)                                           │
└─────────────────────────────────────────────────────────────────────────────────────────┘

                              ┌─────────────────┐
                              │  RoadmapRecipe  │
                              └────────┬────────┘
                                       │
                    ┌──────────────────┼──────────────────┐
                    │ CONTAINS_OBJECTIVE                  │ HAS_MILESTONE
                    ▼                                     ▼
             ┌─────────────┐                       ┌─────────────┐
             │  Objective  │                       │  Milestone  │
             └──────┬──────┘                       └──────┬──────┘
                    │ HAS_KEY_RESULT                      │ REQUIRES_KR
                    ▼                                     ▼
             ┌─────────────┐◀──────────────────────────────
             │  KeyResult  │
             └──────┬──────┘
                    │
          ┌────────┴────────┐
          │ TESTS_ASSUMPTION│ DEPENDS_ON_KR
          ▼                 ▼
    ┌─────────────┐  ┌─────────────┐
    │ Assumption  │  │  KeyResult  │
    └─────────────┘  │   (other)   │
                     └─────────────┘


                    ┌───────────────────┐
                    │ FeatureDefinition │
                    └─────────┬─────────┘
                              │
       ┌──────────────────────┼──────────────────────┐
       │                      │                      │
       │ HAS_CAPABILITY       │ HAS_SCENARIO         │ TARGETS_PERSONA
       ▼                      ▼                      ▼
┌─────────────┐        ┌─────────────┐        ┌─────────────┐
│ Capability  │◀───────│  Scenario   │───────▶│   Persona   │
└─────────────┘        └─────────────┘        └─────────────┘
       ▲               EXERCISES_CAPABILITY
       │               TARGETS_PERSONA
       │
       │ CONTRIBUTES_TO (reverse)
       │
┌─────────────┐
│ ValueModel  │
└─────────────┘


┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                     AIM PHASE                                           │
│                                  (Calibration)                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘

    ┌─────────────────┐
    │  RoadmapRecipe  │
    └────────┬────────┘
             │ ASSESSES (reverse)
             ▼
    ┌─────────────────┐                    ┌─────────────────┐
    │AssessmentReport │───VALIDATES_TREND─▶│      Trend      │
    └────────┬────────┘                    └─────────────────┘
             │
             │ VALIDATES          RESPONDS_TO (reverse)
             │                           │
             ▼                           │
    ┌─────────────────┐          ┌───────┴───────┐
    │   Assumption    │          │CalibrationMemo│
    └─────────────────┘          └───────┬───────┘
                                         │
                                         │ RECOMMENDS_UPDATE_TO
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
           ┌─────────────┐      ┌─────────────────┐  ┌─────────────┐
           │  NorthStar  │      │StrategyFoundation│  │ ValueModel  │
           └─────────────┘      └─────────────────┘  └─────────────┘
                                         │
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
           ┌─────────────────┐  ┌─────────────────┐
           │  RoadmapRecipe  │  │FeatureDefinition│
           └─────────────────┘  └─────────────────┘


┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              MEETING INTEGRATION                                        │
└─────────────────────────────────────────────────────────────────────────────────────────┘

    ┌─────────────────┐
    │     Meeting     │ (from Meeting & Decision pack)
    └────────┬────────┘
             │
             ├── GENERATES ────────▶ InsightAnalysis, Assumption
             │
             └── DISCUSSES ────────▶ FeatureDefinition, RoadmapRecipe, AssessmentReport

```

## Complete Relationship Matrix

| Relationship           | Source Type(s)                                           | Target Type(s)                                                              | Cardinality  |
| ---------------------- | -------------------------------------------------------- | --------------------------------------------------------------------------- | ------------ |
| **Strategic**          |
| GUIDES                 | NorthStar                                                | StrategyFoundation, RoadmapRecipe, ValueModel                               | one-to-many  |
| IMPLEMENTS             | RoadmapRecipe                                            | StrategyFoundation, StrategyFormula                                         | many-to-one  |
| SUPERSEDES             | NorthStar, StrategyFoundation, ValueModel, RoadmapRecipe | (same types)                                                                | many-to-one  |
| **Insights**           |
| INFORMS                | InsightAnalysis                                          | InsightOpportunity, StrategyFormula, FeatureDefinition                      | many-to-many |
| SYNTHESIZES_INTO       | InsightAnalysis                                          | InsightOpportunity                                                          | many-to-one  |
| ADDRESSED_BY           | InsightOpportunity                                       | StrategyFormula                                                             | one-to-many  |
| INCLUDES_TREND         | InsightAnalysis                                          | Trend                                                                       | one-to-many  |
| INFLUENCES_OPPORTUNITY | Trend                                                    | InsightOpportunity                                                          | many-to-many |
| **OKRs**               |
| HAS_KEY_RESULT         | Objective                                                | KeyResult                                                                   | one-to-many  |
| BELONGS_TO_OBJECTIVE   | KeyResult                                                | Objective                                                                   | many-to-one  |
| TESTS_ASSUMPTION       | KeyResult                                                | Assumption                                                                  | many-to-many |
| DEPENDS_ON_KR          | KeyResult                                                | KeyResult                                                                   | many-to-many |
| **Roadmap**            |
| CONTAINS_OBJECTIVE     | RoadmapRecipe                                            | Objective                                                                   | one-to-many  |
| HAS_MILESTONE          | RoadmapRecipe                                            | Milestone                                                                   | one-to-many  |
| REQUIRES_KR            | Milestone                                                | KeyResult                                                                   | many-to-many |
| **Track**              |
| BELONGS_TO_TRACK       | Objective, ValueModel, FeatureDefinition                 | Track                                                                       | many-to-one  |
| **Value/Features**     |
| CONTRIBUTES_TO         | FeatureDefinition                                        | ValueModel                                                                  | many-to-many |
| TARGETS_PERSONA        | FeatureDefinition, Scenario                              | Persona                                                                     | many-to-many |
| REQUIRES_FEATURE       | FeatureDefinition                                        | FeatureDefinition                                                           | many-to-many |
| HAS_ASSUMPTION         | FeatureDefinition, StrategyFormula                       | Assumption                                                                  | one-to-many  |
| HAS_CAPABILITY         | FeatureDefinition                                        | Capability                                                                  | one-to-many  |
| HAS_SCENARIO           | FeatureDefinition                                        | Scenario                                                                    | one-to-many  |
| EXERCISES_CAPABILITY   | Scenario                                                 | Capability                                                                  | many-to-many |
| **Assessment**         |
| ASSESSES               | AssessmentReport                                         | RoadmapRecipe                                                               | one-to-one   |
| VALIDATES              | AssessmentReport                                         | Assumption                                                                  | one-to-many  |
| VALIDATES_TREND        | AssessmentReport                                         | Trend                                                                       | one-to-many  |
| RESPONDS_TO            | CalibrationMemo                                          | AssessmentReport                                                            | many-to-one  |
| RECOMMENDS_UPDATE_TO   | CalibrationMemo                                          | NorthStar, StrategyFoundation, RoadmapRecipe, ValueModel, FeatureDefinition | one-to-many  |
| **Integration**        |
| GENERATES              | Meeting                                                  | InsightAnalysis, Assumption                                                 | one-to-many  |
| DISCUSSES              | Meeting                                                  | FeatureDefinition, RoadmapRecipe, AssessmentReport                          | many-to-many |
| RELATES_TO             | \*                                                       | \*                                                                          | many-to-many |
| CONTAINS_LAYER         | ValueModel                                               | \*                                                                          | one-to-many  |

## Object Types by Phase

### READY Phase (Strategic Foundation) - 6 types

```
┌──────────────────────┬────────────┬─────────────────────────────────────────────────┐
│ Object Type          │ Icon       │ Purpose                                         │
├──────────────────────┼────────────┼─────────────────────────────────────────────────┤
│ NorthStar            │ compass    │ Purpose, vision, mission, values, core beliefs  │
│ InsightAnalysis      │ lightbulb  │ Market/customer/technical insights + evidence   │
│ StrategyFoundation   │ milestone  │ SWOT, strategic bets, success criteria          │
│ InsightOpportunity   │ target     │ Synthesized opportunities from insights         │
│ StrategyFormula      │ route      │ Concrete approaches with risk/reward            │
│ RoadmapRecipe        │ map        │ Time-boxed OKRs by track                        │
└──────────────────────┴────────────┴─────────────────────────────────────────────────┘
```

### FIRE Phase (Execution) - 2 types

```
┌──────────────────────┬────────────┬─────────────────────────────────────────────────┐
│ Object Type          │ Icon       │ Purpose                                         │
├──────────────────────┼────────────┼─────────────────────────────────────────────────┤
│ ValueModel           │ layers     │ L1/L2/L3 value hierarchy per track              │
│ FeatureDefinition    │ puzzle     │ Features with personas, scenarios, requirements │
└──────────────────────┴────────────┴─────────────────────────────────────────────────┘
```

### AIM Phase (Calibration) - 2 types

```
┌──────────────────────┬────────────┬─────────────────────────────────────────────────┐
│ Object Type          │ Icon       │ Purpose                                         │
├──────────────────────┼────────────┼─────────────────────────────────────────────────┤
│ AssessmentReport     │ clipboard  │ Outcomes vs. intentions evaluation              │
│ CalibrationMemo      │ settings   │ Course correction recommendations               │
└──────────────────────┴────────────┴─────────────────────────────────────────────────┘
```

### Supporting Artifacts - 9 types

```
┌──────────────────────┬────────────┬─────────────────────────────────────────────────┐
│ Object Type          │ Icon       │ Purpose                                         │
├──────────────────────┼────────────┼─────────────────────────────────────────────────┤
│ Objective            │ flag       │ Aspirational goal (qualitative)                 │
│ KeyResult            │ bar-chart  │ Measurable outcome (quantitative)               │
│ Assumption           │ help-circle│ Hypothesis requiring validation                 │
│ Track                │ git-branch │ Product/Strategy/OrgOps/Commercial              │
│ Persona              │ user       │ User archetype for feature development          │
│ Milestone            │ calendar   │ Key decision point/launch/event                 │
│ Capability           │ box        │ Discrete shippable unit of value                │
│ Scenario             │ play-circle│ End-to-end user flow with acceptance criteria   │
│ Trend                │ trending-up│ Directional shift (tech/market/regulatory/etc)  │
└──────────────────────┴────────────┴─────────────────────────────────────────────────┘
```

## Four-Track Model

```
                    ┌─────────────────────────────────────────────────────────────┐
                    │                     TRACK STRUCTURE                         │
                    └─────────────────────────────────────────────────────────────┘

     ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
     │   PRODUCT   │    │  STRATEGY   │    │   ORGOPS    │    │ COMMERCIAL  │
     │             │    │             │    │             │    │             │
     │ ValueModel  │    │ ValueModel  │    │ ValueModel  │    │ ValueModel  │
     │     +       │    │     +       │    │     +       │    │     +       │
     │ Objectives  │    │ Objectives  │    │ Objectives  │    │ Objectives  │
     │     +       │    │     +       │    │     +       │    │     +       │
     │ KeyResults  │    │ KeyResults  │    │ KeyResults  │    │ KeyResults  │
     └──────┬──────┘    └──────┬──────┘    └──────┬──────┘    └──────┬──────┘
            │                  │                  │                  │
            └──────────────────┴──────────────────┴──────────────────┘
                                       │
                            DEPENDS_ON_KR (cross-track)
                                       │
                    ┌──────────────────┴──────────────────┐
                    │         CROSS-TRACK DEPENDENCIES    │
                    │                                     │
                    │  KR-P-001 ──requires──▶ KR-S-002   │
                    │  KR-C-003 ──enables───▶ KR-O-001   │
                    │  KR-S-004 ──informs───▶ KR-P-005   │
                    └─────────────────────────────────────┘
```

## Summary Statistics

| Category               | Count |
| ---------------------- | ----- |
| **Object Types**       | 19    |
| - READY Phase          | 6     |
| - FIRE Phase           | 2     |
| - AIM Phase            | 2     |
| - Supporting           | 9     |
| **Relationship Types** | 29    |
| **UI Configurations**  | 19    |
| **Extraction Prompts** | 17    |
