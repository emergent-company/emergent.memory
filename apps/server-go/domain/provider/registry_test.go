package provider

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	if len(r.providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(r.providers))
	}

	if !r.IsSupported(ProviderGoogleAI) {
		t.Error("expected google-ai to be supported")
	}
	if !r.IsSupported(ProviderVertexAI) {
		t.Error("expected vertex-ai to be supported")
	}
	if r.IsSupported(ProviderType("openai")) {
		t.Error("expected openai to NOT be supported")
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()

	// Google AI
	googleDef, err := r.Get(ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if googleDef.DisplayName != "Google AI" {
		t.Errorf("expected display name 'Google AI', got %q", googleDef.DisplayName)
	}
	if len(googleDef.CredentialFields) != 1 {
		t.Fatalf("expected 1 credential field for Google AI, got %d", len(googleDef.CredentialFields))
	}
	if googleDef.CredentialFields[0].Name != "api_key" {
		t.Errorf("expected field name 'api_key', got %q", googleDef.CredentialFields[0].Name)
	}
	if !googleDef.CredentialFields[0].Secret {
		t.Error("expected api_key field to be secret")
	}

	// Vertex AI
	vertexDef, err := r.Get(ProviderVertexAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vertexDef.DisplayName != "Vertex AI" {
		t.Errorf("expected display name 'Vertex AI', got %q", vertexDef.DisplayName)
	}
	if len(vertexDef.CredentialFields) != 3 {
		t.Fatalf("expected 3 credential fields for Vertex AI, got %d", len(vertexDef.CredentialFields))
	}

	// Verify field names
	fieldNames := make(map[string]bool)
	for _, f := range vertexDef.CredentialFields {
		fieldNames[f.Name] = true
	}
	for _, expected := range []string{"service_account_json", "gcp_project", "location"} {
		if !fieldNames[expected] {
			t.Errorf("expected Vertex AI to have field %q", expected)
		}
	}

	// Unsupported
	_, err = r.Get(ProviderType("openai"))
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()

	defs := r.List()
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	types := make(map[ProviderType]bool)
	for _, d := range defs {
		types[d.Type] = true
	}
	if !types[ProviderGoogleAI] || !types[ProviderVertexAI] {
		t.Errorf("expected both google-ai and vertex-ai in list, got %v", types)
	}
}

func TestRegistrySupportedTypes(t *testing.T) {
	r := NewRegistry()

	types := r.SupportedTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types))
	}

	typeSet := make(map[ProviderType]bool)
	for _, pt := range types {
		typeSet[pt] = true
	}
	if !typeSet[ProviderGoogleAI] || !typeSet[ProviderVertexAI] {
		t.Errorf("expected both google-ai and vertex-ai, got %v", typeSet)
	}
}

func TestProviderTypeConstants(t *testing.T) {
	if ProviderGoogleAI != "google-ai" {
		t.Errorf("expected ProviderGoogleAI to be 'google-ai', got %q", ProviderGoogleAI)
	}
	if ProviderVertexAI != "vertex-ai" {
		t.Errorf("expected ProviderVertexAI to be 'vertex-ai', got %q", ProviderVertexAI)
	}
}

func TestModelTypeConstants(t *testing.T) {
	if ModelTypeEmbedding != "embedding" {
		t.Errorf("expected ModelTypeEmbedding to be 'embedding', got %q", ModelTypeEmbedding)
	}
	if ModelTypeGenerative != "generative" {
		t.Errorf("expected ModelTypeGenerative to be 'generative', got %q", ModelTypeGenerative)
	}
}

func TestOperationTypeConstants(t *testing.T) {
	if OperationGenerate != "generate" {
		t.Errorf("expected OperationGenerate to be 'generate', got %q", OperationGenerate)
	}
	if OperationEmbed != "embed" {
		t.Errorf("expected OperationEmbed to be 'embed', got %q", OperationEmbed)
	}
}
