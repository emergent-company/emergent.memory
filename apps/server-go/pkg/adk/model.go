// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"fmt"
	"log/slog"

	"go.uber.org/fx"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/internal/config"
)

// Module provides the ADK ModelFactory as an fx module
var Module = fx.Module("adk",
	fx.Provide(provideModelFactory),
)

// provideModelFactory creates a ModelFactory from the main config
func provideModelFactory(cfg *config.Config, log *slog.Logger) *ModelFactory {
	return NewModelFactory(&cfg.LLM, log)
}

// ModelFactory creates ADK-compatible LLM models from configuration.
type ModelFactory struct {
	cfg *config.LLMConfig
	log *slog.Logger
}

// NewModelFactory creates a new ModelFactory with the given configuration.
func NewModelFactory(cfg *config.LLMConfig, log *slog.Logger) *ModelFactory {
	return &ModelFactory{
		cfg: cfg,
		log: log,
	}
}

// CreateModel creates an ADK-compatible Gemini model for Vertex AI.
//
// The model uses Vertex AI backend with the configured GCP project and location.
// If the configuration is missing required fields, an error is returned.
func (f *ModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	return f.CreateModelWithName(ctx, f.cfg.Model)
}

// CreateModelWithName creates an ADK-compatible Gemini model with a specific model name.
//
// This allows overriding the default model for specific use cases (e.g., using
// a different model for extraction vs verification).
func (f *ModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if f.cfg.GCPProjectID == "" {
		return nil, fmt.Errorf("GCP project ID is required for Vertex AI")
	}
	if f.cfg.VertexAILocation == "" {
		return nil, fmt.Errorf("Vertex AI location is required")
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Configure for Vertex AI backend
	clientCfg := &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  f.cfg.GCPProjectID,
		Location: f.cfg.VertexAILocation,
	}

	f.log.Debug("creating ADK Gemini model",
		slog.String("model", modelName),
		slog.String("project", f.cfg.GCPProjectID),
		slog.String("location", f.cfg.VertexAILocation),
	)

	llm, err := gemini.NewModel(ctx, modelName, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}

	return llm, nil
}

// DefaultGenerateConfig returns a default GenerateContentConfig for extraction tasks.
//
// The config uses low temperature for deterministic JSON extraction output.
func (f *ModelFactory) DefaultGenerateConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:     ptrFloat32(float32(f.cfg.Temperature)),
		MaxOutputTokens: int32(f.cfg.MaxOutputTokens),
	}
}

// ExtractionGenerateConfig returns a GenerateContentConfig optimized for entity extraction.
//
// Uses temperature=0 for deterministic structured output.
func (f *ModelFactory) ExtractionGenerateConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:     ptrFloat32(0.0),
		MaxOutputTokens: int32(f.cfg.MaxOutputTokens),
	}
}

// ExtractionGenerateConfigWithSchema returns a GenerateContentConfig with ResponseSchema
// for guaranteed structured JSON output. The schema constrains LLM output format.
//
// This is ~35% faster than embedding schema in the prompt and guarantees valid JSON.
func (f *ModelFactory) ExtractionGenerateConfigWithSchema(schema *genai.Schema) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:      ptrFloat32(0.0),
		MaxOutputTokens:  int32(f.cfg.MaxOutputTokens),
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}
}

// IsEnabled returns true if the LLM configuration is valid for creating models.
func (f *ModelFactory) IsEnabled() bool {
	return f.cfg.IsEnabled()
}

// ModelName returns the configured default model name.
func (f *ModelFactory) ModelName() string {
	return f.cfg.Model
}

// Helper function for pointer values
func ptrFloat32(v float32) *float32 {
	return &v
}
