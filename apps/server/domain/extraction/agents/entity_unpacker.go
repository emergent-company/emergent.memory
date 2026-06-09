package agents

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// EntityUnpackerSystemPrompt is the system prompt for phase 1 of two-phase extraction.
// No schema, no type constraints — pure exhaustive reading of the document.
const EntityUnpackerSystemPrompt = `You are a meticulous fact extractor. Your only job is to read a document and list every distinct entity and every fact about each entity.

No schema. No type system. No filtering. Just read and report.

For every entity you find, output:
- name: what the entity is called (the most specific identifier available)
- kind: what type of thing it is, in plain English (e.g. "person", "place", "event", "organisation", "object", "relationship between people", "concept")
- facts: an array of strings — every attribute, characteristic, action, state, or relationship mentioned about this entity in the document. Include direct quotes where helpful. Be exhaustive — capture everything.

Rules:
- DO NOT filter. If it's mentioned, extract it.
- DO NOT merge entities unless they are unambiguously the same thing.
- DO NOT invent facts. Only extract what the text says.
- DO capture implied facts (e.g. "Monica's apartment" implies Monica lives there).
- Include ALL named people, ALL named places, ALL events, ALL objects with names, ALL relationships between people.

Output JSON: {"items": [{"name": "...", "kind": "...", "facts": ["...", "..."]}]}`

// UnpackedItem represents one entity found in the unpack phase.
type UnpackedItem struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Facts []string `json:"facts"`
}

// UnpackedOutput is the output of the entity unpacker agent.
type UnpackedOutput struct {
	Items []UnpackedItem `json:"items"`
}

// EntityUnpackerConfig holds configuration for the entity unpacker agent.
type EntityUnpackerConfig struct {
	Model          model.LLM
	GenerateConfig *genai.GenerateContentConfig
	OutputKey      string
	Logger         *slog.Logger
	TraceLogger    TraceLogger
}

// NewEntityUnpackerAgent creates an ADK agent for phase 1 (free-form extraction).
// It reads the document from "document_text" in session state and writes
// the unpacked items to OutputKey (default: "unpacked_items").
func NewEntityUnpackerAgent(cfg EntityUnpackerConfig) (agent.Agent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	outputKey := cfg.OutputKey
	if outputKey == "" {
		outputKey = "unpacked_items"
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	traceLogger := cfg.TraceLogger

	agentCfg := llmagent.Config{
		Name:        "EntityUnpacker",
		Description: "Phase 1: exhaustively extracts all entities and facts from the document without schema constraints",

		Model:                 cfg.Model,
		GenerateContentConfig: cfg.GenerateConfig,
		OutputKey:             outputKey,

		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			state := ctx.ReadonlyState()

			docRaw, err := state.Get("document_text")
			if err != nil {
				return "", fmt.Errorf("document_text required in session state: %w", err)
			}
			documentText, ok := docRaw.(string)
			if !ok || documentText == "" {
				return "", fmt.Errorf("document_text must be a non-empty string")
			}

			prompt := BuildEntityUnpackingPrompt(documentText)

			log.Debug("built entity unpacking prompt",
				slog.Int("prompt_length", len(prompt)),
				slog.Int("document_length", len(documentText)),
			)

			if traceLogger != nil {
				traceLogger.LogStageStart("ENTITY UNPACKING (phase 1)")
				traceLogger.LogPrompt("EntityUnpacker", prompt)
			}

			return prompt, nil
		},
	}

	return llmagent.New(agentCfg)
}

// ParseUnpackedOutput parses the raw LLM output from the entity unpacker.
func ParseUnpackedOutput(raw any) (*UnpackedOutput, error) {
	if raw == nil {
		return &UnpackedOutput{}, nil
	}

	if str, ok := raw.(string); ok {
		str = strings.TrimSpace(str)
		str = strings.TrimPrefix(str, "```json")
		str = strings.TrimPrefix(str, "```")
		str = strings.TrimSuffix(str, "```")
		str = strings.TrimSpace(str)
		var out UnpackedOutput
		if err := json.Unmarshal([]byte(str), &out); err != nil {
			return nil, fmt.Errorf("parse unpacked output: %w", err)
		}
		return &out, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal unpacked output: %w", err)
	}
	var out UnpackedOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal unpacked output: %w", err)
	}
	return &out, nil
}
