# Add EPF Template Pack

## Summary

Add a template pack implementing the **Emergent Product Framework (EPF)** methodology, enabling teams to manage product development through structured READY-FIRE-AIM lifecycle phases with AI-powered artifact extraction and relationship tracking.

## Problem Statement

Product teams using Emergent lack a structured methodology for:

1. **Strategic Planning** - No systematic way to capture and connect vision, mission, insights, and strategy
2. **Feature Development** - No framework linking features to value models, personas, and business outcomes
3. **Continuous Calibration** - No structured approach to validate assumptions and adjust course
4. **Cross-functional Alignment** - No way to track how Product, Strategy, OrgOps, and Commercial tracks interweave

Teams end up with disconnected documents, lost context, and difficulty tracing decisions back to strategic intent.

## Proposed Solution

Implement the EPF methodology as a template pack that provides:

### READY Phase (Strategic Foundation)

- **North Star** - Purpose, vision, mission, values, core beliefs
- **Insight Analysis** - Market/customer/technical insights with evidence chains
- **Strategy Foundation** - Current reality, strategic bets, success criteria
- **Insight Opportunity** - Synthesized opportunities from insight patterns
- **Strategy Formula** - Concrete strategic approaches with risk/reward analysis
- **Roadmap Recipe** - Time-boxed objectives with measurable key results

### FIRE Phase (Execution)

- **Value Model** - Hierarchical value decomposition (layers → components → sub-components)
- **Feature Definition** - Rich feature specs with personas, scenarios, contexts, and value mapping
- **Track Mapping** - Cross-references between Product/Strategy/OrgOps/Commercial artifacts

### AIM Phase (Calibration)

- **Assessment Report** - Systematic evaluation of outcomes vs. intentions
- **Calibration Memo** - Course corrections based on learnings

### Supporting Artifacts

- **Objective** - Strategic objectives with owner and timeline
- **Key Result** - Measurable outcomes tied to objectives
- **Assumption** - Explicit assumptions with validation criteria

## Why EPF?

The Emergent Product Framework is designed for:

1. **Emergence-Based Design** - Acknowledges that product development is exploratory; structure enables adaptation
2. **Schema-Enforced Rigor** - JSON schemas ensure artifacts are complete and consistent
3. **AI-Agent Collaboration** - Wizards (Pathfinder, Synthesizer, Product Architect) guide artifact creation
4. **Braided Four-Track Model** - Explicitly connects Product, Strategy, OrgOps, and Commercial concerns
5. **Evidence-Based Decisions** - Every insight requires evidence; every assumption requires validation criteria

## Benefits

| Benefit                            | Description                                                                        |
| ---------------------------------- | ---------------------------------------------------------------------------------- |
| **Structured Product Development** | Teams follow a proven methodology instead of ad-hoc processes                      |
| **AI-Powered Extraction**          | Extract EPF artifacts from meetings, documents, and conversations                  |
| **Relationship Tracking**          | Trace features to value drivers, assumptions to key results, decisions to strategy |
| **Cross-Functional Visibility**    | See how all four tracks (Product/Strategy/OrgOps/Commercial) connect               |
| **Continuous Calibration**         | Built-in AIM phase ensures regular reflection and course correction                |

## Scope

### In Scope

- EPF object type schemas (15+ artifact types)
- EPF relationship types (20+ relationship types)
- Extraction prompts adapted from EPF wizards
- UI configurations (icons, colors, display fields)
- Seed data for the template pack

### Out of Scope

- EPF-specific UI views (can be added later)
- Integration with external EPF tooling
- Custom EPF reporting dashboards
- Migration tools from other methodologies

## Success Criteria

1. Users can install the EPF template pack and immediately start creating EPF artifacts
2. AI extraction correctly identifies and structures EPF artifacts from unstructured content
3. Relationships between EPF artifacts are captured and visualizable
4. Template pack passes validation against EPF JSON schemas
5. Documentation enables users to understand EPF methodology within Emergent

## References

- EPF Repository: `/root/epf/`
- EPF White Paper: `/root/epf/docs/EPF_WHITE_PAPER.md`
- EPF Schemas: `/root/epf/schemas/`
- EPF Templates: `/root/epf/templates/`
- EPF Wizards: `/root/epf/wizards/`
