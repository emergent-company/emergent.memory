# EPF Template Pack Design

## Overview

This document details how the Emergent Product Framework (EPF) maps to Emergent's template pack structure. The EPF methodology is organized around three lifecycle phases (READY, FIRE, AIM) and four parallel tracks (Product, Strategy, OrgOps, Commercial).

## Source Material

- **EPF Repository:** `/root/epf/`
- **EPF Version:** 2.4.4
- **Schemas:** `/root/epf/schemas/*.json`
- **Templates:** `/root/epf/templates/{READY,FIRE,AIM}/`
- **Wizards:** `/root/epf/wizards/*.md`

## Template Pack Metadata

```typescript
const EPF_PACK_ID = 'a1b2c3d4-e5f6-4789-abcd-1234567890ef';

const metadata = {
  name: 'Emergent Product Framework (EPF)',
  version: '1.0.0',
  description:
    'Structured product development methodology with READY-FIRE-AIM lifecycle phases and four-track braided model (Product, Strategy, OrgOps, Commercial)',
  author: 'Emergent Team',
  source: 'system',
};
```

## Object Type Schemas

### READY Phase (Strategic Foundation)

#### 1. NorthStar

The foundational document capturing organizational purpose, vision, mission, values, and core beliefs.

```typescript
NorthStar: {
  type: 'object',
  description: 'Strategic foundation capturing why the organization exists, where it is headed, and how it operates',
  required: ['title', 'purpose_statement', 'vision_statement', 'mission_statement'],
  properties: {
    title: {
      type: 'string',
      description: 'Document title (e.g., "Acme Corp North Star")',
      minLength: 5,
    },
    organization: {
      type: 'string',
      description: 'Organization name',
    },
    purpose_statement: {
      type: 'string',
      description: 'Why the organization exists - fundamental reason for being',
      minLength: 20,
      maxLength: 200,
    },
    problem_we_solve: {
      type: 'string',
      description: 'The fundamental problem being addressed',
      minLength: 50,
      maxLength: 500,
    },
    who_we_serve: {
      type: 'string',
      description: 'Broadest definition of beneficiaries',
      minLength: 20,
      maxLength: 300,
    },
    impact_we_seek: {
      type: 'string',
      description: 'Ultimate change desired in the world',
      minLength: 50,
      maxLength: 400,
    },
    vision_statement: {
      type: 'string',
      description: 'Vivid picture of the future (5-10 years)',
      minLength: 50,
      maxLength: 300,
    },
    vision_timeframe: {
      type: 'string',
      description: 'Vision timeframe (e.g., "2030" or "2025-2030")',
    },
    success_indicators: {
      type: 'array',
      items: { type: 'string' },
      description: 'Observable indicators of vision achievement',
    },
    mission_statement: {
      type: 'string',
      description: 'What we do and for whom',
      minLength: 30,
      maxLength: 250,
    },
    core_activities: {
      type: 'array',
      items: { type: 'string' },
      description: 'Essential activities that deliver the mission',
    },
    values: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          principle: { type: 'string' },
          behaviors: { type: 'array', items: { type: 'string' } },
        },
      },
      description: 'Organizational values with principles and behaviors',
    },
    core_beliefs: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          belief: { type: 'string' },
          evidence: { type: 'string' },
          implications: { type: 'array', items: { type: 'string' } },
        },
      },
      description: 'Fundamental beliefs about the world with evidence',
    },
    last_reviewed: {
      type: 'string',
      format: 'date',
      description: 'Date of last review',
    },
    next_review: {
      type: 'string',
      format: 'date',
      description: 'Scheduled next review date',
    },
  },
}
```

#### 2. InsightAnalysis

Captures market, customer, and technical insights with evidence chains.

```typescript
InsightAnalysis: {
  type: 'object',
  description: 'Analysis capturing insights from market, customer, or technical research',
  required: ['title', 'insight_type', 'headline', 'evidence_summary'],
  properties: {
    title: {
      type: 'string',
      description: 'Analysis title',
      minLength: 5,
    },
    insight_type: {
      type: 'string',
      enum: ['market', 'customer', 'technical', 'competitive', 'regulatory', 'trend'],
      description: 'Category of insight',
    },
    headline: {
      type: 'string',
      description: 'One-sentence insight summary',
      minLength: 20,
      maxLength: 200,
    },
    context: {
      type: 'string',
      description: 'Background and situational context',
    },
    evidence_summary: {
      type: 'string',
      description: 'Summary of evidence supporting the insight',
      minLength: 50,
    },
    evidence_items: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          source: { type: 'string' },
          finding: { type: 'string' },
          confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
          date: { type: 'string', format: 'date' },
        },
      },
      description: 'Individual evidence items',
    },
    implications: {
      type: 'array',
      items: { type: 'string' },
      description: 'Strategic implications of this insight',
    },
    confidence_level: {
      type: 'string',
      enum: ['validated', 'supported', 'hypothesis', 'speculative'],
      description: 'Overall confidence in the insight',
    },
    tracks_affected: {
      type: 'array',
      items: { type: 'string', enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'] },
      description: 'Which tracks this insight affects',
    },
    analysis_date: {
      type: 'string',
      format: 'date',
      description: 'When the analysis was conducted',
    },
  },
}
```

#### 3. StrategyFoundation

Documents current reality, strategic bets, and success criteria.

```typescript
StrategyFoundation: {
  type: 'object',
  description: 'Strategic foundation documenting current state and strategic direction',
  required: ['title', 'current_reality', 'strategic_bets'],
  properties: {
    title: {
      type: 'string',
      description: 'Document title',
    },
    current_reality: {
      type: 'object',
      properties: {
        market_position: { type: 'string' },
        strengths: { type: 'array', items: { type: 'string' } },
        weaknesses: { type: 'array', items: { type: 'string' } },
        opportunities: { type: 'array', items: { type: 'string' } },
        threats: { type: 'array', items: { type: 'string' } },
      },
      description: 'Current state assessment (SWOT)',
    },
    strategic_bets: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          bet: { type: 'string' },
          rationale: { type: 'string' },
          risk_level: { type: 'string', enum: ['high', 'medium', 'low'] },
          success_criteria: { type: 'string' },
        },
      },
      description: 'Strategic bets the organization is making',
    },
    success_metrics: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          metric: { type: 'string' },
          current_value: { type: 'string' },
          target_value: { type: 'string' },
          timeframe: { type: 'string' },
        },
      },
      description: 'Key success metrics',
    },
    planning_horizon: {
      type: 'string',
      description: 'Timeframe for this strategy (e.g., "2024-2026")',
    },
  },
}
```

#### 4. InsightOpportunity

Synthesized opportunities from insight patterns.

```typescript
InsightOpportunity: {
  type: 'object',
  description: 'Opportunity identified from synthesizing multiple insights',
  required: ['title', 'opportunity_statement', 'source_insights'],
  properties: {
    title: {
      type: 'string',
      description: 'Opportunity title',
    },
    opportunity_statement: {
      type: 'string',
      description: 'Clear statement of the opportunity',
      minLength: 50,
      maxLength: 500,
    },
    source_insights: {
      type: 'array',
      items: { type: 'string' },
      description: 'References to source insight analyses',
    },
    synthesis_rationale: {
      type: 'string',
      description: 'How the insights combine to reveal this opportunity',
    },
    market_size: {
      type: 'string',
      description: 'Estimated market size or impact',
    },
    timing: {
      type: 'string',
      enum: ['immediate', 'near-term', 'medium-term', 'long-term'],
      description: 'When to pursue this opportunity',
    },
    strategic_fit: {
      type: 'string',
      description: 'How this aligns with organization strategy',
    },
    risks: {
      type: 'array',
      items: { type: 'string' },
      description: 'Key risks in pursuing this opportunity',
    },
    required_capabilities: {
      type: 'array',
      items: { type: 'string' },
      description: 'Capabilities needed to capture this opportunity',
    },
    priority: {
      type: 'string',
      enum: ['critical', 'high', 'medium', 'low'],
      description: 'Priority level',
    },
  },
}
```

#### 5. StrategyFormula

Concrete strategic approaches with risk/reward analysis.

```typescript
StrategyFormula: {
  type: 'object',
  description: 'Strategic formula defining approach to capture an opportunity',
  required: ['title', 'formula_statement', 'approach'],
  properties: {
    title: {
      type: 'string',
      description: 'Formula title',
    },
    formula_statement: {
      type: 'string',
      description: 'Concise strategy statement',
      minLength: 30,
      maxLength: 300,
    },
    target_opportunity: {
      type: 'string',
      description: 'Reference to the opportunity this formula addresses',
    },
    approach: {
      type: 'object',
      properties: {
        primary_lever: { type: 'string' },
        supporting_actions: { type: 'array', items: { type: 'string' } },
        differentiators: { type: 'array', items: { type: 'string' } },
      },
      description: 'Strategic approach details',
    },
    resource_requirements: {
      type: 'object',
      properties: {
        investment: { type: 'string' },
        team: { type: 'string' },
        timeline: { type: 'string' },
      },
      description: 'Required resources',
    },
    risk_analysis: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          risk: { type: 'string' },
          probability: { type: 'string', enum: ['high', 'medium', 'low'] },
          impact: { type: 'string', enum: ['high', 'medium', 'low'] },
          mitigation: { type: 'string' },
        },
      },
      description: 'Risk analysis',
    },
    expected_outcomes: {
      type: 'array',
      items: { type: 'string' },
      description: 'Expected outcomes if strategy succeeds',
    },
    success_probability: {
      type: 'string',
      enum: ['high', 'medium', 'low'],
      description: 'Estimated probability of success',
    },
  },
}
```

#### 6. RoadmapRecipe

Time-boxed objectives with measurable key results.

```typescript
RoadmapRecipe: {
  type: 'object',
  description: 'Strategic roadmap with OKRs across four tracks',
  required: ['title', 'timeframe', 'tracks'],
  properties: {
    title: {
      type: 'string',
      description: 'Roadmap title',
    },
    strategy_reference: {
      type: 'string',
      description: 'Reference to parent strategy document',
    },
    cycle: {
      type: 'integer',
      description: 'Cycle number (1, 2, 3, etc.)',
      minimum: 1,
    },
    timeframe: {
      type: 'string',
      description: 'Human-readable timeframe (e.g., "Q1 2024")',
    },
    status: {
      type: 'string',
      enum: ['draft', 'approved', 'active', 'completed', 'cancelled'],
      description: 'Roadmap status',
    },
    tracks: {
      type: 'object',
      properties: {
        product: { type: 'object', description: 'Product track OKRs' },
        strategy: { type: 'object', description: 'Strategy track OKRs' },
        org_ops: { type: 'object', description: 'OrgOps track OKRs' },
        commercial: { type: 'object', description: 'Commercial track OKRs' },
      },
      description: 'OKRs organized by track',
    },
    cross_track_dependencies: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          from_kr: { type: 'string' },
          to_kr: { type: 'string' },
          dependency_type: { type: 'string', enum: ['requires', 'informs', 'enables'] },
          description: { type: 'string' },
        },
      },
      description: 'Dependencies between key results across tracks',
    },
    milestones: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          date: { type: 'string', format: 'date' },
          key_results: { type: 'array', items: { type: 'string' } },
          gate_type: { type: 'string', enum: ['launch', 'review', 'funding', 'decision'] },
        },
      },
      description: 'Key milestones',
    },
  },
}
```

### FIRE Phase (Execution)

#### 7. ValueModel

Hierarchical value decomposition across tracks.

```typescript
ValueModel: {
  type: 'object',
  description: 'Hierarchical value model with layers, components, and sub-components',
  required: ['title', 'track_name', 'layers'],
  properties: {
    title: {
      type: 'string',
      description: 'Value model title',
    },
    track_name: {
      type: 'string',
      enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'],
      description: 'Which track this value model represents',
    },
    version: {
      type: 'string',
      description: 'Semantic version (e.g., "2.1.0")',
    },
    status: {
      type: 'string',
      enum: ['active', 'placeholder', 'deprecated'],
      description: 'Model status',
    },
    description: {
      type: 'string',
      description: 'High-level description of value delivered',
      minLength: 30,
    },
    layers: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string' },
          name: { type: 'string' },
          description: { type: 'string' },
          components: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                id: { type: 'string' },
                name: { type: 'string' },
                description: { type: 'string' },
                sub_components: {
                  type: 'array',
                  items: {
                    type: 'object',
                    properties: {
                      id: { type: 'string' },
                      name: { type: 'string' },
                      active: { type: 'boolean' },
                      premium: { type: 'boolean' },
                      uvp: { type: 'string', description: 'Unique value proposition' },
                    },
                  },
                },
              },
            },
          },
        },
      },
      description: 'L1 layers containing L2 components and L3 sub-components',
    },
  },
}
```

#### 8. FeatureDefinition

Rich feature specs with personas, scenarios, and value mapping.

```typescript
FeatureDefinition: {
  type: 'object',
  description: 'Comprehensive feature definition with personas, scenarios, and value mapping',
  required: ['title', 'feature_id', 'summary', 'personas'],
  properties: {
    title: {
      type: 'string',
      description: 'Feature name',
    },
    feature_id: {
      type: 'string',
      description: 'Unique feature identifier (kebab-case)',
    },
    summary: {
      type: 'string',
      description: 'Brief feature summary',
      minLength: 50,
      maxLength: 500,
    },
    status: {
      type: 'string',
      enum: ['proposed', 'approved', 'in-development', 'released', 'deprecated'],
      description: 'Feature lifecycle status',
    },
    priority: {
      type: 'string',
      enum: ['critical', 'high', 'medium', 'low'],
      description: 'Feature priority',
    },
    personas: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          role: { type: 'string' },
          goals: { type: 'array', items: { type: 'string' } },
          pain_points: { type: 'array', items: { type: 'string' } },
          scenarios: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                scenario: { type: 'string' },
                context: { type: 'string' },
                outcome: { type: 'string' },
              },
            },
          },
        },
      },
      description: 'Target personas and their scenarios',
    },
    value_drivers: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          driver: { type: 'string' },
          value_model_ref: { type: 'string', description: 'Reference to value model component' },
          contribution: { type: 'string', enum: ['primary', 'secondary', 'tertiary'] },
        },
      },
      description: 'How this feature contributes to value model',
    },
    functional_requirements: {
      type: 'array',
      items: { type: 'string' },
      description: 'Functional requirements',
    },
    non_functional_requirements: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          category: { type: 'string', enum: ['performance', 'security', 'scalability', 'usability', 'accessibility'] },
          requirement: { type: 'string' },
        },
      },
      description: 'Non-functional requirements',
    },
    dependencies: {
      type: 'array',
      items: { type: 'string' },
      description: 'Dependencies on other features or components',
    },
    assumptions: {
      type: 'array',
      items: { type: 'string' },
      description: 'Key assumptions',
    },
    risks: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          risk: { type: 'string' },
          mitigation: { type: 'string' },
        },
      },
      description: 'Risks and mitigations',
    },
    release_target: {
      type: 'string',
      description: 'Target release version or date',
    },
  },
}
```

### AIM Phase (Calibration)

#### 9. AssessmentReport

Systematic evaluation of outcomes vs. intentions.

```typescript
AssessmentReport: {
  type: 'object',
  description: 'Assessment of outcomes vs. intentions for a completed cycle',
  required: ['title', 'assessment_period', 'objectives_assessment'],
  properties: {
    title: {
      type: 'string',
      description: 'Assessment title',
    },
    assessment_period: {
      type: 'string',
      description: 'Period being assessed (e.g., "Q1 2024")',
    },
    roadmap_reference: {
      type: 'string',
      description: 'Reference to the roadmap being assessed',
    },
    overall_status: {
      type: 'string',
      enum: ['exceeded', 'met', 'partially-met', 'missed'],
      description: 'Overall assessment status',
    },
    objectives_assessment: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          objective: { type: 'string' },
          track: { type: 'string', enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'] },
          status: { type: 'string', enum: ['exceeded', 'met', 'partially-met', 'missed'] },
          key_results_achieved: { type: 'number' },
          key_results_total: { type: 'number' },
          narrative: { type: 'string' },
        },
      },
      description: 'Assessment of each objective',
    },
    assumptions_validated: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          assumption: { type: 'string' },
          outcome: { type: 'string', enum: ['validated', 'invalidated', 'inconclusive'] },
          evidence: { type: 'string' },
          implications: { type: 'string' },
        },
      },
      description: 'Assessment of key assumptions',
    },
    learnings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          category: { type: 'string', enum: ['process', 'product', 'market', 'team', 'technical'] },
          learning: { type: 'string' },
          action_recommended: { type: 'string' },
        },
      },
      description: 'Key learnings from the period',
    },
    metrics_summary: {
      type: 'object',
      description: 'Summary of key metrics',
    },
    assessment_date: {
      type: 'string',
      format: 'date',
      description: 'Date of assessment',
    },
  },
}
```

#### 10. CalibrationMemo

Course corrections based on learnings.

```typescript
CalibrationMemo: {
  type: 'object',
  description: 'Course correction recommendations based on assessment learnings',
  required: ['title', 'calibration_type', 'recommendations'],
  properties: {
    title: {
      type: 'string',
      description: 'Memo title',
    },
    assessment_reference: {
      type: 'string',
      description: 'Reference to the assessment report',
    },
    calibration_type: {
      type: 'string',
      enum: ['strategic-pivot', 'tactical-adjustment', 'process-improvement', 'resource-reallocation'],
      description: 'Type of calibration',
    },
    trigger: {
      type: 'string',
      description: 'What triggered this calibration',
    },
    current_state: {
      type: 'string',
      description: 'Description of current state',
    },
    target_state: {
      type: 'string',
      description: 'Desired future state',
    },
    recommendations: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          recommendation: { type: 'string' },
          track: { type: 'string', enum: ['Product', 'Strategy', 'OrgOps', 'Commercial', 'All'] },
          priority: { type: 'string', enum: ['immediate', 'next-cycle', 'future'] },
          owner: { type: 'string' },
          rationale: { type: 'string' },
        },
      },
      description: 'Specific recommendations',
    },
    artifacts_to_update: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          artifact_type: { type: 'string' },
          artifact_name: { type: 'string' },
          update_needed: { type: 'string' },
        },
      },
      description: 'EPF artifacts requiring updates',
    },
    decision_required: {
      type: 'boolean',
      description: 'Whether stakeholder decision is required',
    },
    decision_deadline: {
      type: 'string',
      format: 'date',
      description: 'Deadline for decision if required',
    },
  },
}
```

### Supporting Artifacts

#### 11. Objective

```typescript
Objective: {
  type: 'object',
  description: 'Strategic objective within a roadmap',
  required: ['title', 'track', 'statement'],
  properties: {
    title: {
      type: 'string',
      description: 'Objective title',
    },
    objective_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "obj-p-001")',
    },
    track: {
      type: 'string',
      enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'],
      description: 'Which track owns this objective',
    },
    statement: {
      type: 'string',
      description: 'Aspirational objective statement',
      minLength: 20,
      maxLength: 300,
    },
    rationale: {
      type: 'string',
      description: 'Why this objective matters',
    },
    owner: {
      type: 'string',
      description: 'Person accountable for this objective',
    },
    timeframe: {
      type: 'string',
      description: 'Target completion timeframe',
    },
    status: {
      type: 'string',
      enum: ['not-started', 'in-progress', 'at-risk', 'completed', 'cancelled'],
      description: 'Current status',
    },
  },
}
```

#### 12. KeyResult

```typescript
KeyResult: {
  type: 'object',
  description: 'Measurable key result tied to an objective',
  required: ['title', 'metric', 'target_value'],
  properties: {
    title: {
      type: 'string',
      description: 'Key result title',
    },
    kr_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "kr-p-001")',
    },
    metric: {
      type: 'string',
      description: 'What is being measured',
    },
    baseline_value: {
      type: 'string',
      description: 'Starting value',
    },
    target_value: {
      type: 'string',
      description: 'Target value to achieve',
    },
    current_value: {
      type: 'string',
      description: 'Current value',
    },
    measurement_method: {
      type: 'string',
      description: 'How the metric is measured',
    },
    update_frequency: {
      type: 'string',
      enum: ['daily', 'weekly', 'bi-weekly', 'monthly', 'quarterly'],
      description: 'How often the metric is updated',
    },
    status: {
      type: 'string',
      enum: ['not-started', 'on-track', 'at-risk', 'off-track', 'achieved'],
      description: 'Current status',
    },
    confidence: {
      type: 'number',
      minimum: 0,
      maximum: 100,
      description: 'Confidence percentage (0-100)',
    },
  },
}
```

#### 13. Assumption

```typescript
Assumption: {
  type: 'object',
  description: 'Explicit assumption requiring validation',
  required: ['title', 'assumption_statement', 'validation_criteria'],
  properties: {
    title: {
      type: 'string',
      description: 'Assumption title',
    },
    assumption_id: {
      type: 'string',
      description: 'Unique identifier',
    },
    assumption_statement: {
      type: 'string',
      description: 'Clear statement of the assumption',
      minLength: 20,
    },
    category: {
      type: 'string',
      enum: ['market', 'customer', 'technical', 'business-model', 'competitive', 'regulatory'],
      description: 'Assumption category',
    },
    risk_level: {
      type: 'string',
      enum: ['critical', 'high', 'medium', 'low'],
      description: 'Risk if assumption is wrong',
    },
    validation_criteria: {
      type: 'string',
      description: 'How to validate or invalidate this assumption',
    },
    validation_method: {
      type: 'string',
      description: 'Approach to test the assumption',
    },
    status: {
      type: 'string',
      enum: ['untested', 'testing', 'validated', 'invalidated', 'inconclusive'],
      description: 'Current validation status',
    },
    evidence: {
      type: 'string',
      description: 'Evidence collected',
    },
    implications_if_wrong: {
      type: 'string',
      description: 'Impact if assumption proves false',
    },
  },
}
```

#### 14. Track

```typescript
Track: {
  type: 'object',
  description: 'One of the four EPF tracks (Product, Strategy, OrgOps, Commercial)',
  required: ['name', 'description'],
  properties: {
    name: {
      type: 'string',
      enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'],
      description: 'Track name',
    },
    description: {
      type: 'string',
      description: 'Track purpose and scope',
    },
    owner: {
      type: 'string',
      description: 'Track owner/lead',
    },
    current_focus: {
      type: 'string',
      description: 'Current primary focus area',
    },
    active_objectives: {
      type: 'number',
      description: 'Number of active objectives',
    },
    health_status: {
      type: 'string',
      enum: ['healthy', 'attention-needed', 'at-risk'],
      description: 'Overall track health',
    },
  },
}
```

#### 15. Persona

```typescript
Persona: {
  type: 'object',
  description: 'User persona for feature development',
  required: ['name', 'role', 'primary_goal'],
  properties: {
    name: {
      type: 'string',
      description: 'Persona name (e.g., "Product Manager Paula")',
    },
    role: {
      type: 'string',
      description: 'Job role or title',
    },
    organization_type: {
      type: 'string',
      description: 'Type of organization they work in',
    },
    primary_goal: {
      type: 'string',
      description: 'Primary goal or job-to-be-done',
    },
    secondary_goals: {
      type: 'array',
      items: { type: 'string' },
      description: 'Secondary goals',
    },
    pain_points: {
      type: 'array',
      items: { type: 'string' },
      description: 'Key pain points',
    },
    behaviors: {
      type: 'array',
      items: { type: 'string' },
      description: 'Key behaviors and habits',
    },
    tech_savviness: {
      type: 'string',
      enum: ['low', 'medium', 'high', 'expert'],
      description: 'Technical proficiency level',
    },
    decision_factors: {
      type: 'array',
      items: { type: 'string' },
      description: 'What influences their decisions',
    },
    quote: {
      type: 'string',
      description: 'Representative quote from this persona',
    },
  },
}
```

#### 16. Milestone

Key decision points, launches, or events where multiple Key Results converge.

```typescript
Milestone: {
  type: 'object',
  description: 'Key decision point, launch, or event requiring specific KR completion',
  required: ['title', 'milestone_description', 'gate_type'],
  properties: {
    title: {
      type: 'string',
      description: 'Milestone name',
    },
    milestone_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "ms-001")',
    },
    milestone_description: {
      type: 'string',
      description: 'What the milestone represents and why it matters',
      minLength: 20,
      maxLength: 200,
    },
    target_date: {
      type: 'string',
      format: 'date',
      description: 'Target date or date range for the milestone',
    },
    gate_type: {
      type: 'string',
      enum: ['launch', 'review', 'funding', 'decision', 'demo', 'compliance'],
      description: 'Type of milestone gate',
    },
    status: {
      type: 'string',
      enum: ['planned', 'at-risk', 'on-track', 'achieved', 'missed', 'deferred'],
      description: 'Current milestone status',
    },
    stakeholders: {
      type: 'array',
      items: { type: 'string' },
      description: 'Key stakeholders for this milestone (board, customers, investors)',
    },
    success_criteria: {
      type: 'string',
      description: 'What defines success for this milestone',
    },
    buffer_days: {
      type: 'number',
      description: 'Days of buffer before milestone date',
    },
  },
}
```

#### 17. Capability

Discrete unit of value within a feature that can be independently specified, implemented, and tested.

```typescript
Capability: {
  type: 'object',
  description: 'Discrete, shippable unit of value within a feature',
  required: ['title', 'capability_id', 'description'],
  properties: {
    title: {
      type: 'string',
      description: 'Short, action-oriented capability name (verb-noun format)',
    },
    capability_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "cap-001")',
      pattern: '^cap-[0-9]+$',
    },
    description: {
      type: 'string',
      description: 'What this capability does and what value it provides',
      minLength: 30,
    },
    status: {
      type: 'string',
      enum: ['planned', 'in-development', 'testing', 'released', 'deprecated'],
      description: 'Implementation status',
    },
    priority: {
      type: 'string',
      enum: ['critical', 'high', 'medium', 'low'],
      description: 'Capability priority',
    },
    acceptance_criteria: {
      type: 'array',
      items: { type: 'string' },
      description: 'Testable criteria for capability completion',
    },
    inputs: {
      type: 'array',
      items: { type: 'string' },
      description: 'What data/actions the capability requires',
    },
    outputs: {
      type: 'array',
      items: { type: 'string' },
      description: 'What the capability produces',
    },
    constraints: {
      type: 'array',
      items: { type: 'string' },
      description: 'Limits, requirements, or boundaries',
    },
  },
}
```

#### 18. Scenario

Concrete user flow demonstrating how capabilities are used, with testable acceptance criteria.

```typescript
Scenario: {
  type: 'object',
  description: 'End-to-end user flow with testable acceptance criteria',
  required: ['title', 'scenario_id', 'actor', 'trigger', 'action', 'outcome', 'acceptance_criteria'],
  properties: {
    title: {
      type: 'string',
      description: 'Human-readable scenario name summarizing the user goal',
      minLength: 10,
    },
    scenario_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "scn-001")',
      pattern: '^scn-[0-9]+$',
    },
    jtbd_category: {
      type: 'string',
      description: 'Job-to-be-done category this scenario implements',
    },
    actor: {
      type: 'string',
      description: 'Who performs this scenario (matches persona)',
    },
    context: {
      type: 'string',
      description: 'Situation and conditions when scenario occurs',
      minLength: 20,
    },
    trigger: {
      type: 'string',
      description: 'What initiates the scenario',
      minLength: 10,
    },
    action: {
      type: 'string',
      description: 'Step-by-step what happens',
      minLength: 30,
    },
    outcome: {
      type: 'string',
      description: 'Value delivered / expected result',
      minLength: 20,
    },
    acceptance_criteria: {
      type: 'array',
      items: { type: 'string' },
      description: 'Testable conditions for scenario success',
      minItems: 1,
    },
    priority: {
      type: 'string',
      enum: ['critical', 'high', 'medium', 'low'],
      description: 'Scenario priority for testing',
    },
    test_status: {
      type: 'string',
      enum: ['not-tested', 'passing', 'failing', 'blocked'],
      description: 'Current test status',
    },
  },
}
```

#### 19. Trend

Directional shift in technology, market, user behavior, regulatory, or competitive dimensions.

```typescript
Trend: {
  type: 'object',
  description: 'Directional shift that informs opportunity evaluation',
  required: ['title', 'trend_id', 'category', 'trend_description', 'timeframe', 'impact'],
  properties: {
    title: {
      type: 'string',
      description: 'Trend name',
    },
    trend_id: {
      type: 'string',
      description: 'Unique identifier (e.g., "trend-tech-001")',
    },
    category: {
      type: 'string',
      enum: ['technology', 'market', 'user_behavior', 'regulatory', 'competitive'],
      description: 'Trend category',
    },
    trend_description: {
      type: 'string',
      description: 'What shift is occurring and what it enables',
      minLength: 20,
      maxLength: 300,
    },
    timeframe: {
      type: 'string',
      description: 'When this trend will significantly impact our space',
      enum: ['happening-now', '6-12-months', '1-2-years', '2-5-years'],
    },
    impact: {
      type: 'string',
      description: 'How this trend affects opportunity evaluation and strategy',
      minLength: 30,
      maxLength: 500,
    },
    confidence: {
      type: 'string',
      enum: ['high', 'medium', 'low'],
      description: 'Confidence in trend assessment',
    },
    evidence: {
      type: 'array',
      items: { type: 'string' },
      description: 'Sources supporting this trend',
      minItems: 1,
    },
    tracks_affected: {
      type: 'array',
      items: { type: 'string', enum: ['Product', 'Strategy', 'OrgOps', 'Commercial'] },
      description: 'Which tracks this trend affects',
    },
    last_validated: {
      type: 'string',
      format: 'date',
      description: 'When this trend was last validated',
    },
  },
}
```

## Relationship Type Schemas

```typescript
const relationshipTypeSchemas = {
  // North Star relationships
  GUIDES: {
    description: 'North Star guides strategic artifacts',
    sourceTypes: ['NorthStar'],
    targetTypes: ['StrategyFoundation', 'RoadmapRecipe', 'ValueModel'],
    cardinality: 'one-to-many',
  },

  // Insight relationships
  INFORMS: {
    description: 'Insight informs opportunity or strategy',
    sourceTypes: ['InsightAnalysis'],
    targetTypes: ['InsightOpportunity', 'StrategyFormula', 'FeatureDefinition'],
    cardinality: 'many-to-many',
  },
  SYNTHESIZES_INTO: {
    description: 'Multiple insights synthesize into an opportunity',
    sourceTypes: ['InsightAnalysis'],
    targetTypes: ['InsightOpportunity'],
    cardinality: 'many-to-one',
  },

  // Opportunity relationships
  ADDRESSED_BY: {
    description: 'Opportunity is addressed by strategy formula',
    sourceTypes: ['InsightOpportunity'],
    targetTypes: ['StrategyFormula'],
    cardinality: 'one-to-many',
  },

  // Strategy relationships
  IMPLEMENTS: {
    description: 'Roadmap implements strategy',
    sourceTypes: ['RoadmapRecipe'],
    targetTypes: ['StrategyFoundation', 'StrategyFormula'],
    cardinality: 'many-to-one',
  },

  // Objective/KR relationships
  HAS_KEY_RESULT: {
    description: 'Objective has key results',
    sourceTypes: ['Objective'],
    targetTypes: ['KeyResult'],
    cardinality: 'one-to-many',
  },
  BELONGS_TO_OBJECTIVE: {
    description: 'Key result belongs to objective',
    sourceTypes: ['KeyResult'],
    targetTypes: ['Objective'],
    cardinality: 'many-to-one',
  },
  TESTS_ASSUMPTION: {
    description: 'Key result tests an assumption',
    sourceTypes: ['KeyResult'],
    targetTypes: ['Assumption'],
    cardinality: 'many-to-many',
  },
  DEPENDS_ON_KR: {
    description: 'Key result depends on another key result',
    sourceTypes: ['KeyResult'],
    targetTypes: ['KeyResult'],
    cardinality: 'many-to-many',
    attributes: {
      dependency_type: {
        type: 'string',
        enum: ['requires', 'informs', 'enables'],
      },
    },
  },

  // Track relationships
  BELONGS_TO_TRACK: {
    description: 'Artifact belongs to a track',
    sourceTypes: ['Objective', 'ValueModel', 'FeatureDefinition'],
    targetTypes: ['Track'],
    cardinality: 'many-to-one',
  },

  // Value Model relationships
  CONTRIBUTES_TO: {
    description: 'Feature contributes to value model component',
    sourceTypes: ['FeatureDefinition'],
    targetTypes: ['ValueModel'],
    cardinality: 'many-to-many',
    attributes: {
      contribution_level: {
        type: 'string',
        enum: ['primary', 'secondary', 'tertiary'],
      },
      value_component_id: {
        type: 'string',
        description: 'Specific L2/L3 component ID',
      },
    },
  },
  CONTAINS_LAYER: {
    description: 'Value model contains layers',
    sourceTypes: ['ValueModel'],
    targetTypes: ['*'], // Would be ValueModelLayer if we had it
    cardinality: 'one-to-many',
  },

  // Feature relationships
  TARGETS_PERSONA: {
    description: 'Feature or scenario targets a persona',
    sourceTypes: ['FeatureDefinition', 'Scenario'],
    targetTypes: ['Persona'],
    cardinality: 'many-to-many',
  },
  REQUIRES_FEATURE: {
    description: 'Feature requires another feature',
    sourceTypes: ['FeatureDefinition'],
    targetTypes: ['FeatureDefinition'],
    cardinality: 'many-to-many',
  },
  HAS_ASSUMPTION: {
    description: 'Feature has underlying assumptions',
    sourceTypes: ['FeatureDefinition', 'StrategyFormula'],
    targetTypes: ['Assumption'],
    cardinality: 'one-to-many',
  },

  // Roadmap relationships
  CONTAINS_OBJECTIVE: {
    description: 'Roadmap contains objectives',
    sourceTypes: ['RoadmapRecipe'],
    targetTypes: ['Objective'],
    cardinality: 'one-to-many',
  },
  HAS_MILESTONE: {
    description: 'Roadmap has milestones',
    sourceTypes: ['RoadmapRecipe'],
    targetTypes: ['Milestone'],
    cardinality: 'one-to-many',
  },

  // Milestone relationships
  REQUIRES_KR: {
    description: 'Milestone requires key results to be achieved',
    sourceTypes: ['Milestone'],
    targetTypes: ['KeyResult'],
    cardinality: 'many-to-many',
  },

  // Capability relationships
  HAS_CAPABILITY: {
    description: 'Feature has discrete capabilities',
    sourceTypes: ['FeatureDefinition'],
    targetTypes: ['Capability'],
    cardinality: 'one-to-many',
  },

  // Scenario relationships
  HAS_SCENARIO: {
    description: 'Feature has user scenarios',
    sourceTypes: ['FeatureDefinition'],
    targetTypes: ['Scenario'],
    cardinality: 'one-to-many',
  },
  EXERCISES_CAPABILITY: {
    description: 'Scenario exercises one or more capabilities',
    sourceTypes: ['Scenario'],
    targetTypes: ['Capability'],
    cardinality: 'many-to-many',
  },

  // Trend relationships
  INCLUDES_TREND: {
    description: 'Insight analysis includes trends',
    sourceTypes: ['InsightAnalysis'],
    targetTypes: ['Trend'],
    cardinality: 'one-to-many',
  },
  VALIDATES_TREND: {
    description: 'Assessment validates or updates trend confidence',
    sourceTypes: ['AssessmentReport'],
    targetTypes: ['Trend'],
    cardinality: 'one-to-many',
  },
  INFLUENCES_OPPORTUNITY: {
    description: 'Trend influences opportunity evaluation',
    sourceTypes: ['Trend'],
    targetTypes: ['InsightOpportunity'],
    cardinality: 'many-to-many',
  },

  // Assessment relationships
  ASSESSES: {
    description: 'Assessment report assesses a roadmap',
    sourceTypes: ['AssessmentReport'],
    targetTypes: ['RoadmapRecipe'],
    cardinality: 'one-to-one',
  },
  VALIDATES: {
    description: 'Assessment validates/invalidates assumptions',
    sourceTypes: ['AssessmentReport'],
    targetTypes: ['Assumption'],
    cardinality: 'one-to-many',
  },

  // Calibration relationships
  RESPONDS_TO: {
    description: 'Calibration memo responds to assessment',
    sourceTypes: ['CalibrationMemo'],
    targetTypes: ['AssessmentReport'],
    cardinality: 'many-to-one',
  },
  RECOMMENDS_UPDATE_TO: {
    description: 'Calibration recommends updates to artifacts',
    sourceTypes: ['CalibrationMemo'],
    targetTypes: [
      'NorthStar',
      'StrategyFoundation',
      'RoadmapRecipe',
      'ValueModel',
      'FeatureDefinition',
    ],
    cardinality: 'one-to-many',
  },

  // Meeting integration (from existing Meeting pack)
  GENERATES: {
    description: 'Meeting generates EPF artifacts',
    sourceTypes: ['Meeting'],
    targetTypes: ['InsightAnalysis', 'Decision', 'ActionItem', 'Assumption'],
    cardinality: 'one-to-many',
  },
  DISCUSSES: {
    description: 'Meeting discusses EPF artifacts',
    sourceTypes: ['Meeting'],
    targetTypes: ['FeatureDefinition', 'RoadmapRecipe', 'AssessmentReport'],
    cardinality: 'many-to-many',
  },

  // General relationships
  RELATES_TO: {
    description: 'General relationship between EPF artifacts',
    sourceTypes: ['*'],
    targetTypes: ['*'],
    cardinality: 'many-to-many',
  },
  SUPERSEDES: {
    description: 'Newer version supersedes older',
    sourceTypes: [
      'NorthStar',
      'StrategyFoundation',
      'ValueModel',
      'RoadmapRecipe',
    ],
    targetTypes: [
      'NorthStar',
      'StrategyFoundation',
      'ValueModel',
      'RoadmapRecipe',
    ],
    cardinality: 'many-to-one',
  },
};
```

## UI Configurations

```typescript
const uiConfigs = {
  // READY Phase
  NorthStar: {
    icon: 'lucide--compass',
    color: '#6366f1', // Indigo - strategic foundation
    defaultView: 'card',
    listFields: ['title', 'organization', 'last_reviewed'],
    cardFields: [
      'title',
      'purpose_statement',
      'vision_statement',
      'mission_statement',
    ],
  },
  InsightAnalysis: {
    icon: 'lucide--lightbulb',
    color: '#f59e0b', // Amber - insights
    defaultView: 'list',
    listFields: ['title', 'insight_type', 'confidence_level', 'analysis_date'],
    cardFields: ['title', 'headline', 'evidence_summary', 'implications'],
  },
  StrategyFoundation: {
    icon: 'lucide--milestone',
    color: '#8b5cf6', // Purple - strategy
    defaultView: 'card',
    listFields: ['title', 'planning_horizon'],
    cardFields: ['title', 'current_reality', 'strategic_bets'],
  },
  InsightOpportunity: {
    icon: 'lucide--target',
    color: '#10b981', // Emerald - opportunities
    defaultView: 'card',
    listFields: ['title', 'timing', 'priority'],
    cardFields: ['title', 'opportunity_statement', 'strategic_fit', 'risks'],
  },
  StrategyFormula: {
    icon: 'lucide--route',
    color: '#8b5cf6', // Purple - strategy
    defaultView: 'card',
    listFields: ['title', 'success_probability'],
    cardFields: ['title', 'formula_statement', 'approach', 'risk_analysis'],
  },
  RoadmapRecipe: {
    icon: 'lucide--map',
    color: '#3b82f6', // Blue - roadmaps
    defaultView: 'card',
    listFields: ['title', 'timeframe', 'status', 'cycle'],
    cardFields: ['title', 'timeframe', 'tracks', 'milestones'],
  },

  // FIRE Phase
  ValueModel: {
    icon: 'lucide--layers',
    color: '#06b6d4', // Cyan - value modeling
    defaultView: 'card',
    listFields: ['title', 'track_name', 'status', 'version'],
    cardFields: ['title', 'track_name', 'description', 'layers'],
  },
  FeatureDefinition: {
    icon: 'lucide--puzzle',
    color: '#22c55e', // Green - features
    defaultView: 'card',
    listFields: ['title', 'status', 'priority', 'release_target'],
    cardFields: ['title', 'summary', 'personas', 'value_drivers'],
  },

  // AIM Phase
  AssessmentReport: {
    icon: 'lucide--clipboard-check',
    color: '#f97316', // Orange - assessment
    defaultView: 'card',
    listFields: ['title', 'assessment_period', 'overall_status'],
    cardFields: [
      'title',
      'overall_status',
      'objectives_assessment',
      'learnings',
    ],
  },
  CalibrationMemo: {
    icon: 'lucide--settings-2',
    color: '#ef4444', // Red - calibration/action
    defaultView: 'card',
    listFields: ['title', 'calibration_type', 'decision_required'],
    cardFields: ['title', 'trigger', 'recommendations', 'artifacts_to_update'],
  },

  // Supporting Artifacts
  Objective: {
    icon: 'lucide--flag',
    color: '#3b82f6', // Blue
    defaultView: 'list',
    listFields: ['title', 'track', 'status', 'timeframe'],
    cardFields: ['title', 'statement', 'rationale', 'owner'],
  },
  KeyResult: {
    icon: 'lucide--bar-chart-2',
    color: '#10b981', // Emerald
    defaultView: 'list',
    listFields: ['title', 'metric', 'current_value', 'target_value', 'status'],
    cardFields: [
      'title',
      'metric',
      'baseline_value',
      'current_value',
      'target_value',
      'confidence',
    ],
  },
  Assumption: {
    icon: 'lucide--help-circle',
    color: '#f59e0b', // Amber
    defaultView: 'list',
    listFields: ['title', 'category', 'risk_level', 'status'],
    cardFields: [
      'title',
      'assumption_statement',
      'validation_criteria',
      'status',
      'evidence',
    ],
  },
  Track: {
    icon: 'lucide--git-branch',
    color: '#6366f1', // Indigo
    defaultView: 'list',
    listFields: ['name', 'owner', 'health_status', 'active_objectives'],
    cardFields: ['name', 'description', 'current_focus', 'health_status'],
  },
  Persona: {
    icon: 'lucide--user',
    color: '#ec4899', // Pink
    defaultView: 'card',
    listFields: ['name', 'role', 'tech_savviness'],
    cardFields: ['name', 'role', 'primary_goal', 'pain_points', 'quote'],
  },
  Milestone: {
    icon: 'lucide--calendar-check',
    color: '#3b82f6', // Blue - aligned with roadmaps
    defaultView: 'list',
    listFields: ['title', 'target_date', 'gate_type', 'status'],
    cardFields: [
      'title',
      'milestone_description',
      'target_date',
      'gate_type',
      'status',
      'success_criteria',
    ],
  },
  Capability: {
    icon: 'lucide--box',
    color: '#22c55e', // Green - aligned with features
    defaultView: 'list',
    listFields: ['title', 'status', 'priority'],
    cardFields: [
      'title',
      'description',
      'status',
      'priority',
      'acceptance_criteria',
    ],
  },
  Scenario: {
    icon: 'lucide--play-circle',
    color: '#8b5cf6', // Purple - user flows
    defaultView: 'card',
    listFields: ['title', 'actor', 'priority', 'test_status'],
    cardFields: [
      'title',
      'actor',
      'context',
      'trigger',
      'action',
      'outcome',
      'acceptance_criteria',
    ],
  },
  Trend: {
    icon: 'lucide--trending-up',
    color: '#f59e0b', // Amber - aligned with insights
    defaultView: 'list',
    listFields: ['title', 'category', 'timeframe', 'confidence'],
    cardFields: [
      'title',
      'category',
      'trend_description',
      'timeframe',
      'impact',
      'confidence',
      'evidence',
    ],
  },
};
```

## Extraction Prompts

Based on EPF Wizards (Pathfinder, Synthesizer, Product Architect, etc.)

```typescript
const extractionPrompts = {
  NorthStar: {
    system: `You are a strategic planning expert extracting North Star elements.
Extract organizational purpose, vision, mission, values, and core beliefs.
Focus on enduring strategic foundations, not tactical plans.
Identify: purpose statement, problem being solved, who is served, vision timeframe, success indicators, core activities.`,
    user: 'Extract North Star strategic foundation elements from this content about organizational strategy.',
  },

  InsightAnalysis: {
    system: `You are the EPF Pathfinder - an insight analyst.
Extract insights from market research, customer feedback, competitive analysis, or technical assessments.
For each insight, identify: type (market/customer/technical/competitive), headline, evidence, implications, confidence level.
Tag which tracks (Product/Strategy/OrgOps/Commercial) are affected.`,
    user: 'Extract structured insights from this research or analysis content.',
  },

  InsightOpportunity: {
    system: `You are the EPF Synthesizer - connecting insights to opportunities.
Identify opportunities that emerge from combining multiple insights.
For each opportunity: state the opportunity clearly, cite source insights, assess market size, timing, strategic fit, and required capabilities.`,
    user: 'Identify strategic opportunities from these insights.',
  },

  StrategyFoundation: {
    system: `You are a strategic planning facilitator.
Extract current reality assessment (SWOT), strategic bets, and success metrics.
Identify what the organization believes about its position, what risks it's taking, and how it will measure success.`,
    user: 'Extract strategy foundation elements from this strategic planning content.',
  },

  StrategyFormula: {
    system: `You are a strategy consultant.
Extract concrete strategic approaches: what is the primary lever, supporting actions, differentiation, resource requirements.
Assess risks and expected outcomes.`,
    user: 'Extract strategy formula from this strategic discussion or plan.',
  },

  RoadmapRecipe: {
    system: `You are a product planning expert.
Extract roadmap elements: objectives, key results, timeframes, milestones, dependencies.
Organize by track (Product/Strategy/OrgOps/Commercial).
Identify cross-track dependencies.`,
    user: 'Extract roadmap planning elements from this content.',
  },

  ValueModel: {
    system: `You are the EPF Product Architect.
Extract value model hierarchy: layers (L1), components (L2), and sub-components (L3).
Identify which track (Product/Strategy/OrgOps/Commercial) each element belongs to.
Note active/premium status and unique value propositions.`,
    user: 'Extract value model structure from this product or capability description.',
  },

  FeatureDefinition: {
    system: `You are a product manager extracting feature definitions.
Extract: feature summary, target personas (with goals, pain points, scenarios), value drivers, functional requirements, non-functional requirements, dependencies, assumptions, risks.
Connect features to value model components where possible.`,
    user: 'Extract comprehensive feature definition from this feature discussion or specification.',
  },

  AssessmentReport: {
    system: `You are a performance analyst.
Extract assessment elements: which objectives were met/missed, key results achieved, assumptions validated/invalidated, and key learnings.
Categorize learnings by type (process/product/market/team/technical).`,
    user: 'Extract assessment findings from this retrospective or review content.',
  },

  CalibrationMemo: {
    system: `You are an organizational change advisor.
Extract calibration recommendations: what needs to change, why (trigger), current vs. target state, specific recommendations by track, and which artifacts need updating.
Note urgency and whether decisions are required.`,
    user: 'Extract calibration recommendations from this strategic discussion or pivot planning.',
  },

  Objective: {
    system: `You are an OKR coach.
Extract objectives: aspirational statements, which track owns them, rationale, owner, and timeframe.
Objectives should be qualitative and inspiring.`,
    user: 'Extract objectives from this planning or goal-setting content.',
  },

  KeyResult: {
    system: `You are an OKR coach.
Extract key results: specific metrics, baseline values, target values, measurement methods.
Key results should be quantitative and measurable.`,
    user: 'Extract measurable key results from this content.',
  },

  Assumption: {
    system: `You are a risk analyst.
Extract explicit assumptions: what is assumed to be true, validation criteria, risk level if wrong, and how to test it.
Categorize assumptions (market/customer/technical/business-model/competitive).`,
    user: 'Extract assumptions and risks from this content.',
  },

  Persona: {
    system: `You are a UX researcher.
Extract persona details: name, role, organization type, primary goal, pain points, behaviors, tech savviness, decision factors.
Include a representative quote if available.`,
    user: 'Extract user persona details from this research or discussion.',
  },

  Milestone: {
    system: `You are a program manager identifying key decision points and milestones.
Extract milestones: name, target date, gate type (launch/review/funding/decision/demo/compliance), stakeholders, and success criteria.
Focus on points where multiple work streams converge or where external stakeholders are involved.
Identify which key results must be achieved for each milestone.`,
    user: 'Extract milestones and key decision points from this roadmap or planning content.',
  },

  Capability: {
    system: `You are a product architect decomposing features into capabilities.
Extract discrete, shippable units of value: name (verb-noun format), description, acceptance criteria, inputs, outputs, and constraints.
Each capability should be independently testable and deliver clear value.
Status should reflect implementation progress (planned/in-development/testing/released).`,
    user: 'Extract discrete capabilities from this feature specification or product discussion.',
  },

  Scenario: {
    system: `You are a UX designer creating user scenarios.
Extract scenarios with: title, actor (persona), context, trigger, action steps, expected outcome, and acceptance criteria.
Each scenario should be an end-to-end user flow that exercises one or more capabilities.
Include job-to-be-done category where applicable.`,
    user: 'Extract user scenarios and flows from this feature discussion or user research.',
  },

  Trend: {
    system: `You are a strategic analyst identifying market and technology trends.
Extract trends with: name, category (technology/market/user_behavior/regulatory/competitive), description, timeframe, impact assessment, confidence level, and evidence sources.
Focus on directional shifts that should inform opportunity evaluation and strategic planning.
Note which tracks (Product/Strategy/OrgOps/Commercial) are affected.`,
    user: 'Extract trends and directional shifts from this market research or strategic analysis.',
  },
};
```

## Summary

### Object Types: 19

1. **READY Phase (6):** NorthStar, InsightAnalysis, StrategyFoundation, InsightOpportunity, StrategyFormula, RoadmapRecipe
2. **FIRE Phase (2):** ValueModel, FeatureDefinition
3. **AIM Phase (2):** AssessmentReport, CalibrationMemo
4. **Supporting (9):** Objective, KeyResult, Assumption, Track, Persona, Milestone, Capability, Scenario, Trend

### Relationship Types: 29

- North Star/Strategy: GUIDES, IMPLEMENTS, SUPERSEDES
- Insights: INFORMS, SYNTHESIZES_INTO, ADDRESSED_BY, INCLUDES_TREND
- OKRs: HAS_KEY_RESULT, BELONGS_TO_OBJECTIVE, TESTS_ASSUMPTION, DEPENDS_ON_KR
- Value/Features: CONTRIBUTES_TO, TARGETS_PERSONA, REQUIRES_FEATURE, HAS_ASSUMPTION, HAS_CAPABILITY, HAS_SCENARIO, EXERCISES_CAPABILITY
- Roadmap: CONTAINS_OBJECTIVE, HAS_MILESTONE, REQUIRES_KR
- Assessment: ASSESSES, VALIDATES, RESPONDS_TO, RECOMMENDS_UPDATE_TO, VALIDATES_TREND
- Trends: INFLUENCES_OPPORTUNITY
- Integration: GENERATES, DISCUSSES, RELATES_TO, BELONGS_TO_TRACK

### UI Configurations: 19

All object types have icon, color, default view, list fields, and card fields configured.

### Extraction Prompts: 17

All major object types have system and user prompts for AI-powered extraction.
