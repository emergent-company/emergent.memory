package adk

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/emergent-company/emergent/internal/config"
	"google.golang.org/genai"
)

func TestNewModelFactory(t *testing.T) {
	cfg := &config.LLMConfig{
		GCPProjectID:     "test-project",
		VertexAILocation: "us-central1",
		Model:            "gemini-1.5-pro",
		Temperature:      0.1,
		MaxOutputTokens:  8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	factory := NewModelFactory(cfg, log)

	if factory == nil {
		t.Fatal("NewModelFactory returned nil")
	}
	if factory.cfg != cfg {
		t.Error("NewModelFactory didn't set config")
	}
	if factory.log != log {
		t.Error("NewModelFactory didn't set logger")
	}
}

func TestModelFactoryCreateModelWithName_ValidationErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name      string
		cfg       *config.LLMConfig
		modelName string
		wantErr   string
	}{
		{
			name: "missing GCP project ID",
			cfg: &config.LLMConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
			},
			modelName: "gemini-1.5-pro",
			wantErr:   "GCP project ID is required for Vertex AI",
		},
		{
			name: "missing Vertex AI location",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
			},
			modelName: "gemini-1.5-pro",
			wantErr:   "Vertex AI location is required",
		},
		{
			name: "missing model name",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			modelName: "",
			wantErr:   "model name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log)
			_, err := factory.CreateModelWithName(context.Background(), tt.modelName)

			if err == nil {
				t.Error("CreateModelWithName() expected error, got nil")
			} else if err.Error() != tt.wantErr {
				t.Errorf("CreateModelWithName() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestModelFactoryDefaultGenerateConfig(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 4096,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	config := factory.DefaultGenerateConfig()

	if config == nil {
		t.Fatal("DefaultGenerateConfig returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("DefaultGenerateConfig Temperature is nil")
	}
	if *config.Temperature != 0.5 {
		t.Errorf("DefaultGenerateConfig Temperature = %f, want 0.5", *config.Temperature)
	}
	if config.MaxOutputTokens != 4096 {
		t.Errorf("DefaultGenerateConfig MaxOutputTokens = %d, want 4096", config.MaxOutputTokens)
	}
}

func TestModelFactoryExtractionGenerateConfig(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5, // Should be ignored for extraction
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	config := factory.ExtractionGenerateConfig()

	if config == nil {
		t.Fatal("ExtractionGenerateConfig returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("ExtractionGenerateConfig Temperature is nil")
	}
	// Extraction should always use temperature 0
	if *config.Temperature != 0.0 {
		t.Errorf("ExtractionGenerateConfig Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.MaxOutputTokens != 8192 {
		t.Errorf("ExtractionGenerateConfig MaxOutputTokens = %d, want 8192", config.MaxOutputTokens)
	}
}

func TestModelFactoryIsEnabled(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name string
		cfg  *config.LLMConfig
		want bool
	}{
		{
			name: "enabled with all fields",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				Model:            "gemini-1.5-pro",
			},
			want: true,
		},
		{
			name: "disabled without project",
			cfg: &config.LLMConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
				Model:            "gemini-1.5-pro",
			},
			want: false,
		},
		{
			name: "disabled without location",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
				Model:            "gemini-1.5-pro",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log)
			got := factory.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelFactoryModelName(t *testing.T) {
	cfg := &config.LLMConfig{
		Model: "gemini-1.5-flash",
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	got := factory.ModelName()
	if got != "gemini-1.5-flash" {
		t.Errorf("ModelName() = %q, want %q", got, "gemini-1.5-flash")
	}
}

func TestPtrFloat32(t *testing.T) {
	tests := []struct {
		name  string
		value float32
	}{
		{"zero", 0.0},
		{"positive", 0.5},
		{"negative", -0.5},
		{"one", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ptr := ptrFloat32(tt.value)
			if ptr == nil {
				t.Fatal("ptrFloat32 returned nil")
			}
			if *ptr != tt.value {
				t.Errorf("ptrFloat32(%f) = %f, want %f", tt.value, *ptr, tt.value)
			}
		})
	}
}

func TestProvideModelFactory(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GCPProjectID:     "test-project",
			VertexAILocation: "us-central1",
			Model:            "gemini-1.5-pro",
			Temperature:      0.1,
			MaxOutputTokens:  8192,
		},
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	factory := provideModelFactory(cfg, log)

	if factory == nil {
		t.Fatal("provideModelFactory returned nil")
	}
	if factory.cfg.GCPProjectID != "test-project" {
		t.Errorf("provideModelFactory cfg.GCPProjectID = %q, want %q", factory.cfg.GCPProjectID, "test-project")
	}
	if factory.cfg.Model != "gemini-1.5-pro" {
		t.Errorf("provideModelFactory cfg.Model = %q, want %q", factory.cfg.Model, "gemini-1.5-pro")
	}
}

func TestModelFactoryCreateModel_ValidationErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name    string
		cfg     *config.LLMConfig
		wantErr string
	}{
		{
			name: "missing GCP project ID",
			cfg: &config.LLMConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
				Model:            "gemini-1.5-pro",
			},
			wantErr: "GCP project ID is required for Vertex AI",
		},
		{
			name: "missing Vertex AI location",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
				Model:            "gemini-1.5-pro",
			},
			wantErr: "Vertex AI location is required",
		},
		{
			name: "missing model name (uses config's empty model)",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				Model:            "",
			},
			wantErr: "model name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log)
			_, err := factory.CreateModel(context.Background())

			if err == nil {
				t.Error("CreateModel() expected error, got nil")
			} else if err.Error() != tt.wantErr {
				t.Errorf("CreateModel() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	schema := &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Test schema",
		Required:    []string{"entities"},
		Properties: map[string]*genai.Schema{
			"entities": {
				Type:        genai.TypeArray,
				Description: "Array of entities",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"name", "type"},
					Properties: map[string]*genai.Schema{
						"name": {Type: genai.TypeString},
						"type": {Type: genai.TypeString, Enum: []string{"Person", "Organization"}},
					},
				},
			},
		},
	}

	config := factory.ExtractionGenerateConfigWithSchema(schema)

	if config == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema Temperature is nil")
	}
	if *config.Temperature != 0.0 {
		t.Errorf("ExtractionGenerateConfigWithSchema Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.MaxOutputTokens != 8192 {
		t.Errorf("ExtractionGenerateConfigWithSchema MaxOutputTokens = %d, want 8192", config.MaxOutputTokens)
	}
	if config.ResponseMIMEType != "application/json" {
		t.Errorf("ExtractionGenerateConfigWithSchema ResponseMIMEType = %q, want %q", config.ResponseMIMEType, "application/json")
	}
	if config.ResponseSchema == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema ResponseSchema is nil")
	}
	if config.ResponseSchema != schema {
		t.Error("ExtractionGenerateConfigWithSchema ResponseSchema doesn't match input schema")
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema_NilSchema(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 4096,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	config := factory.ExtractionGenerateConfigWithSchema(nil)

	if config == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema returned nil for nil schema")
	}
	if *config.Temperature != 0.0 {
		t.Errorf("Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.ResponseMIMEType != "application/json" {
		t.Errorf("ResponseMIMEType = %q, want %q", config.ResponseMIMEType, "application/json")
	}
	if config.ResponseSchema != nil {
		t.Error("ResponseSchema should be nil when nil schema passed")
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema_SchemaWithEnumConstraint(t *testing.T) {
	cfg := &config.LLMConfig{
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log)

	schema := &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"relationships"},
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"type"},
					Properties: map[string]*genai.Schema{
						"type": {
							Type: genai.TypeString,
							Enum: []string{"WORKS_AT", "LOCATED_IN", "PARENT_OF"},
						},
					},
				},
			},
		},
	}

	config := factory.ExtractionGenerateConfigWithSchema(schema)

	if config.ResponseSchema == nil {
		t.Fatal("ResponseSchema is nil")
	}

	relSchema := config.ResponseSchema.Properties["relationships"]
	if relSchema == nil {
		t.Fatal("relationships property is nil")
	}

	typeSchema := relSchema.Items.Properties["type"]
	if typeSchema == nil {
		t.Fatal("type property is nil")
	}

	if len(typeSchema.Enum) != 3 {
		t.Errorf("type.Enum length = %d, want 3", len(typeSchema.Enum))
	}
}
