package agents

// TwoPhaseExtractionPipeline is an experimental parallel to ExtractionPipeline.
//
// Architecture:
//
//	Phase 1 — Unpack (EntityUnpacker)
//	  LLM reads document with NO schema constraints and extracts everything it
//	  sees as free-form {name, kind, facts[]} items. Cognitively cheap: just read.
//
//	Phase 2 — Normalize (EntityNormalizer, replaces EntityExtractor)
//	  LLM receives the unpacked items + schema and maps each item onto a schema
//	  type, filling property slots from the item's facts. Cognitively cheap: just
//	  classify and slot-fill from already-extracted data.
//
//	Phase 3 — EntityProcessor (unchanged)
//	Phase 4 — RelationshipRetryLoop (unchanged)
//
// The pipeline shares ExtractionPipelineInput / ExtractionPipelineOutput with
// the single-phase pipeline — it is a drop-in experiment, not a replacement.
// Only swap into the worker once comparison tests confirm measurable improvement.

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// TwoPhaseExtractionPipeline orchestrates the two-phase entity extraction experiment.
type TwoPhaseExtractionPipeline struct {
	modelFactory        *adk.ModelFactory
	objectSchemas       map[string]ObjectSchema
	relationshipSchemas map[string]RelationshipSchema
	orphanThreshold     float64
	maxRetries          uint
	log                 *slog.Logger
	traceLogger         TraceLogger
}

// TwoPhaseExtractionInput extends ExtractionPipelineInput with optional per-type
// hints and negative examples sourced from SchemaExtractionPrompts.
// When not supplied, the normalization phase falls back to schema descriptions only.
type TwoPhaseExtractionInput struct {
	ExtractionPipelineInput                   // embeds the standard input
	TypeHints               map[string]string // SchemaExtractionPrompts.TypeHints
	NegativeExamples        []string          // SchemaExtractionPrompts.NegativeExamples
}

// NewTwoPhaseExtractionPipeline creates a two-phase extraction pipeline.
// Configuration is identical to ExtractionPipelineConfig.
func NewTwoPhaseExtractionPipeline(cfg ExtractionPipelineConfig) (*TwoPhaseExtractionPipeline, error) {
	if cfg.ModelFactory == nil {
		return nil, fmt.Errorf("model factory is required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	orphanThreshold := cfg.OrphanThreshold
	if orphanThreshold <= 0 {
		orphanThreshold = 0.3
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	return &TwoPhaseExtractionPipeline{
		modelFactory:        cfg.ModelFactory,
		objectSchemas:       cfg.ObjectSchemas,
		relationshipSchemas: cfg.RelationshipSchemas,
		orphanThreshold:     orphanThreshold,
		maxRetries:          maxRetries,
		log:                 log,
		traceLogger:         cfg.TraceLogger,
	}, nil
}

// Run executes the two-phase extraction pipeline.
// Accepts TwoPhaseExtractionInput; if a plain ExtractionPipelineInput is needed,
// wrap it: TwoPhaseExtractionInput{ExtractionPipelineInput: input}.
func (p *TwoPhaseExtractionPipeline) Run(ctx context.Context, input TwoPhaseExtractionInput) (*ExtractionPipelineOutput, error) {
	if input.DocumentText == "" {
		return nil, fmt.Errorf("document text is required")
	}

	p.log.Info("starting two-phase extraction pipeline",
		slog.Int("document_length", len(input.DocumentText)),
		slog.Int("schema_count", len(input.ObjectSchemas)),
		slog.Int("type_hints_count", len(input.TypeHints)),
	)

	llm, err := p.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	pipelineAgent, err := p.createPipelineAgent(llm, input.TypeHints, input.NegativeExamples)
	if err != nil {
		return nil, fmt.Errorf("failed to create two-phase pipeline agent: %w", err)
	}

	initialState := map[string]any{
		"document_text": input.DocumentText,
	}
	if len(input.ObjectSchemas) > 0 {
		initialState["object_schemas"] = input.ObjectSchemas
	}
	if len(input.RelationshipSchemas) > 0 {
		initialState["relationship_schemas"] = input.RelationshipSchemas
	}
	if len(input.AllowedTypes) > 0 {
		initialState["allowed_types"] = input.AllowedTypes
	}
	if len(input.ExistingEntities) > 0 {
		initialState["existing_entities"] = input.ExistingEntities
	}
	if input.ProjectContext != "" {
		initialState["project_context"] = input.ProjectContext
	}
	if input.DomainGuidance != "" {
		initialState["domain_guidance"] = input.DomainGuidance
	}
	if len(input.TypeHints) > 0 {
		initialState["type_hints"] = input.TypeHints
	}
	if len(input.NegativeExamples) > 0 {
		initialState["negative_examples"] = input.NegativeExamples
	}

	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "extraction_two_phase",
		UserID:  "system",
		State:   initialState,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	sess := createResp.Session

	r, err := runner.New(runner.Config{
		Agent:          pipelineAgent,
		SessionService: sessionService,
		AppName:        "extraction_two_phase",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	userMessage := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText("Extract entities and relationships using two-phase approach.")},
	}

	for _, err := range r.Run(ctx, "system", sess.ID(), userMessage, agent.RunConfig{}) {
		if err != nil {
			return nil, fmt.Errorf("two-phase pipeline execution failed: %w", err)
		}
	}

	getResp, err := sessionService.Get(ctx, &session.GetRequest{
		AppName:   "extraction_two_phase",
		UserID:    "system",
		SessionID: sess.ID(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return p.extractResults(getResp.Session)
}

// createPipelineAgent builds the two-phase sequential agent.
func (p *TwoPhaseExtractionPipeline) createPipelineAgent(
	llm model.LLM,
	typeHints map[string]string,
	negativeExamples []string,
) (agent.Agent, error) {
	// Phase 1 — EntityUnpacker (no schema constraint).
	unpacker, err := NewEntityUnpackerAgent(EntityUnpackerConfig{
		Model:          llm,
		GenerateConfig: p.modelFactory.ExtractionGenerateConfig(),
		OutputKey:      "unpacked_items",
		Logger:         p.log,
		TraceLogger:    p.traceLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create entity unpacker: %w", err)
	}

	// Phase 2 — EntityNormalizer (maps unpacked → schema types).
	normalizer, err := p.createEntityNormalizerAgent(llm, typeHints, negativeExamples)
	if err != nil {
		return nil, fmt.Errorf("failed to create entity normalizer: %w", err)
	}

	// Phase 3 — EntityProcessor (adds temp_ids, unchanged from single-phase).
	entityProcessor, err := p.createEntityProcessorAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to create entity processor: %w", err)
	}

	// Phase 4 — RelationshipRetryLoop (unchanged from single-phase).
	relCfg := p.modelFactory.ExtractionGenerateConfig()
	if len(p.relationshipSchemas) > 0 {
		relCfg = p.modelFactory.ExtractionGenerateConfigWithSchema(
			BuildRelationshipSchemaFromMemorySchema(p.relationshipSchemas))
	}
	relationshipBuilder, err := NewRelationshipBuilderAgent(RelationshipBuilderConfig{
		Model:          llm,
		GenerateConfig: relCfg,
		OutputKey:      "extracted_relationships",
		Logger:         p.log,
		TraceLogger:    p.traceLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship builder: %w", err)
	}
	qualityChecker, err := NewQualityCheckerAgent(QualityCheckerConfig{
		OrphanThreshold: p.orphanThreshold,
		Logger:          p.log,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create quality checker: %w", err)
	}
	relProcessor, err := p.createRelationshipProcessorAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship processor: %w", err)
	}
	relWithQuality, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "TwoPhase_RelationshipWithQuality",
			SubAgents: []agent.Agent{relationshipBuilder, relProcessor, qualityChecker},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship-with-quality: %w", err)
	}
	relLoop, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "TwoPhase_RelationshipRetryLoop",
			SubAgents: []agent.Agent{relWithQuality},
		},
		MaxIterations: p.maxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship loop: %w", err)
	}

	return sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "TwoPhaseExtractionPipeline",
			SubAgents: []agent.Agent{unpacker, normalizer, entityProcessor, relLoop},
		},
	})
}

// createEntityNormalizerAgent creates the phase-2 normalizer agent.
// It reads "unpacked_items" from state and writes "extracted_entities_raw".
func (p *TwoPhaseExtractionPipeline) createEntityNormalizerAgent(
	llm model.LLM,
	typeHints map[string]string,
	negativeExamples []string,
) (agent.Agent, error) {
	log := p.log
	traceLogger := p.traceLogger

	agentCfg := llmagent.Config{
		Name:        "EntityNormalizer",
		Description: "Phase 2: maps unpacked free-form items onto strict schema types and property slots",

		Model:                 llm,
		GenerateContentConfig: p.modelFactory.ExtractionGenerateConfig(),
		OutputKey:             "extracted_entities_raw",

		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			state := ctx.ReadonlyState()

			// Read unpacked items from phase 1.
			rawUnpacked, err := state.Get("unpacked_items")
			if err != nil {
				return "", fmt.Errorf("unpacked_items not found in state: %w", err)
			}

			unpacked, err := ParseUnpackedOutput(rawUnpacked)
			if err != nil {
				return "", fmt.Errorf("failed to parse unpacked items: %w", err)
			}

			if len(unpacked.Items) == 0 {
				log.Warn("EntityNormalizer: no unpacked items — phase 1 produced nothing")
			}

			// Read schema from state (may have been updated at runtime).
			objectSchemas := make(map[string]ObjectSchema)
			if schemasRaw, err := state.Get("object_schemas"); err == nil {
				if schemas, ok := schemasRaw.(map[string]ObjectSchema); ok {
					objectSchemas = schemas
				} else if schemasMap, ok := schemasRaw.(map[string]any); ok {
					for k, v := range schemasMap {
						if schemaMap, ok := v.(map[string]any); ok {
							objectSchemas[k] = convertToObjectSchema(schemaMap)
						}
					}
				}
			}

			var allowedTypes []string
			if typesRaw, err := state.Get("allowed_types"); err == nil {
				if types, ok := typesRaw.([]string); ok {
					allowedTypes = types
				}
			}

			// Type hints: prefer runtime state, fall back to constructor-time hints.
			effectiveHints := typeHints
			if hintsRaw, err := state.Get("type_hints"); err == nil {
				if hints, ok := hintsRaw.(map[string]string); ok {
					effectiveHints = hints
				}
			}

			// Negative examples: prefer runtime state.
			effectiveNeg := negativeExamples
			if negRaw, err := state.Get("negative_examples"); err == nil {
				if neg, ok := negRaw.([]string); ok {
					effectiveNeg = neg
				}
			}

			prompt := BuildEntityNormalizationPrompt(
				unpacked.Items,
				objectSchemas,
				allowedTypes,
				effectiveHints,
				effectiveNeg,
			)

			log.Debug("built entity normalization prompt",
				slog.Int("prompt_length", len(prompt)),
				slog.Int("unpacked_items", len(unpacked.Items)),
				slog.Int("schema_types", len(objectSchemas)),
				slog.Int("type_hints", len(effectiveHints)),
			)

			if traceLogger != nil {
				traceLogger.LogStageStart("ENTITY NORMALIZATION (phase 2)")
				traceLogger.LogPrompt("EntityNormalizer", prompt)
			}

			return prompt, nil
		},
	}

	return llmagent.New(agentCfg)
}

// createEntityProcessorAgent mirrors the single-phase implementation exactly.
// Adds temp_ids to normalized entities and writes "extracted_entities" to state.
func (p *TwoPhaseExtractionPipeline) createEntityProcessorAgent() (agent.Agent, error) {
	log := p.log
	return agent.New(agent.Config{
		Name:        "TwoPhase_EntityProcessor",
		Description: "Adds temp_ids to normalized entities for relationship building",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				_, span := tracing.Start(ctx, "extraction.two_phase.entity_processor")
				defer span.End()

				state := ctx.Session().State()

				rawEntitiesAny, err := state.Get("extracted_entities_raw")
				if err != nil {
					// Normalizer produced nothing — continue with empty entity list.
					log.Warn("TwoPhase_EntityProcessor: no extracted_entities_raw in state")
					span.SetStatus(codes.Ok, "no entities")
					if err2 := state.Set("extracted_entities", []InternalEntity{}); err2 != nil {
						yield(nil, err2)
						return
					}
					yield(nil, nil)
					return
				}

				var entities []ExtractedEntity
				switch v := rawEntitiesAny.(type) {
				case string:
					output, parseErr := ParseEntityExtractionOutput(v)
					if parseErr != nil {
						yield(nil, fmt.Errorf("two-phase: parse entity output: %w", parseErr))
						return
					}
					entities = output.Entities
				case *EntityExtractionOutput:
					entities = v.Entities
				case map[string]any:
					if entArr, ok := v["entities"].([]any); ok {
						for _, e := range entArr {
							data, _ := json.Marshal(e)
							var entity ExtractedEntity
							if json.Unmarshal(data, &entity) == nil {
								entities = append(entities, entity)
							}
						}
					}
				default:
					data, _ := json.Marshal(v)
					output, parseErr := ParseEntityExtractionOutput(string(data))
					if parseErr != nil {
						yield(nil, fmt.Errorf("two-phase: parse entity output from %T: %w", v, parseErr))
						return
					}
					entities = output.Entities
				}

				existingIDs := make(map[string]bool)
				internalEntities := make([]InternalEntity, 0, len(entities))
				for _, e := range entities {
					tempID := generateTempID(e.Name, e.Type, existingIDs)
					existingIDs[tempID] = true
					internalEntities = append(internalEntities, InternalEntity{
						TempID:           tempID,
						Name:             e.Name,
						Type:             e.Type,
						Description:      e.Description,
						Properties:       e.Properties,
						Action:           e.Action,
						ExistingEntityID: e.ExistingEntityID,
					})
				}

				log.Debug("two-phase: processed entities", slog.Int("count", len(internalEntities)))
				span.SetAttributes(attribute.Int("memory.extraction.two_phase.entity_count", len(internalEntities)))

				if err := state.Set("extracted_entities", internalEntities); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					yield(nil, fmt.Errorf("two-phase: set extracted_entities: %w", err))
					return
				}

				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "TwoPhase_EntityProcessor"
				evt.Content = genai.NewContentFromText(
					fmt.Sprintf("Two-phase: processed %d entities", len(internalEntities)), "model")
				evt.Actions.StateDelta = map[string]any{
					"extracted_entities": internalEntities,
				}
				span.SetStatus(codes.Ok, "")
				yield(evt, nil)
			}
		},
	})
}

// createRelationshipProcessorAgent mirrors the single-phase implementation.
func (p *TwoPhaseExtractionPipeline) createRelationshipProcessorAgent() (agent.Agent, error) {
	log := p.log
	return agent.New(agent.Config{
		Name:        "TwoPhase_RelationshipProcessor",
		Description: "Normalises relationship builder output for quality checking",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				_, span := tracing.Start(ctx, "extraction.two_phase.relationship_processor")
				defer span.End()

				state := ctx.Session().State()

				relsRaw, err := state.Get("extracted_relationships")
				if err != nil {
					span.SetStatus(codes.Ok, "")
					yield(nil, nil)
					return
				}

				var relationships []ExtractedRelationship
				switch v := relsRaw.(type) {
				case []ExtractedRelationship:
					relationships = v
				case *RelationshipExtractionOutput:
					relationships = v.Relationships
				case string:
					output, parseErr := ParseRelationshipExtractionOutput(v)
					if parseErr != nil {
						yield(nil, nil)
						return
					}
					relationships = output.Relationships
				default:
					data, _ := json.Marshal(v)
					var output RelationshipExtractionOutput
					if json.Unmarshal(data, &output) == nil {
						relationships = output.Relationships
					}
				}

				log.Debug("two-phase: processed relationships", slog.Int("count", len(relationships)))
				span.SetAttributes(attribute.Int("memory.extraction.two_phase.relationship_count", len(relationships)))

				if err := state.Set("extracted_relationships", relationships); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					yield(nil, fmt.Errorf("two-phase: set extracted_relationships: %w", err))
					return
				}

				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "TwoPhase_RelationshipProcessor"
				evt.Content = genai.NewContentFromText(
					fmt.Sprintf("Two-phase: processed %d relationships", len(relationships)), "model")
				evt.Actions.StateDelta = map[string]any{"extracted_relationships": relationships}
				span.SetStatus(codes.Ok, "")
				yield(evt, nil)
			}
		},
	})
}

// extractResults reads final state from the session after pipeline completion.
func (p *TwoPhaseExtractionPipeline) extractResults(sess session.Session) (*ExtractionPipelineOutput, error) {
	state := sess.State()

	var entities []InternalEntity
	if entRaw, err := state.Get("extracted_entities"); err == nil {
		switch v := entRaw.(type) {
		case []InternalEntity:
			entities = v
		default:
			data, _ := json.Marshal(v)
			_ = json.Unmarshal(data, &entities)
		}
	}

	var relationships []ExtractedRelationship
	if relsRaw, err := state.Get("extracted_relationships"); err == nil {
		switch v := relsRaw.(type) {
		case []ExtractedRelationship:
			relationships = v
		case *RelationshipExtractionOutput:
			relationships = v.Relationships
		case string:
			if out, err := ParseRelationshipExtractionOutput(v); err == nil {
				relationships = out.Relationships
			}
		default:
			data, _ := json.Marshal(v)
			var out RelationshipExtractionOutput
			if json.Unmarshal(data, &out) == nil {
				relationships = out.Relationships
			}
		}
	}

	p.log.Info("two-phase extraction completed",
		slog.Int("entity_count", len(entities)),
		slog.Int("relationship_count", len(relationships)),
	)

	return &ExtractionPipelineOutput{
		Entities:      entities,
		Relationships: relationships,
	}, nil
}
