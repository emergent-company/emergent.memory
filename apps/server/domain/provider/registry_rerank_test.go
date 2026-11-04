package provider

import (
	"encoding/json"
	"testing"
)

func TestRerankAPIForVendors(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		pt   ProviderType
		want RerankAPI
	}{
		{ProviderCohere, RerankAPICohere},
		{ProviderAliyun, RerankAPIDashScope},
		{ProviderVolcengine, RerankAPIVolcengine},
		{ProviderLKEAP, RerankAPITencentLKEAP},
		{ProviderOpenAI, ""},
		{ProviderGeneric, ""},
	}
	for _, tt := range tests {
		if got := r.RerankAPIFor(tt.pt); got != tt.want {
			t.Errorf("RerankAPIFor(%q) = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestRerankVendorHasRerankModelType(t *testing.T) {
	r := NewRegistry()

	rerankVendors := []ProviderType{ProviderCohere, ProviderAliyun, ProviderVolcengine, ProviderLKEAP}
	for _, pt := range rerankVendors {
		def, err := r.Get(pt)
		if err != nil {
			t.Fatalf("Get(%q): %v", pt, err)
		}

		hasModelType := false
		for _, mt := range def.ModelTypes {
			if mt == ModelTypeRerank {
				hasModelType = true
				break
			}
		}
		if !hasModelType {
			t.Errorf("vendor %q does not declare ModelTypeRerank in ModelTypes", pt)
		}

		if url := def.DefaultBaseURLs[ModelTypeRerank]; url == "" {
			t.Errorf("vendor %q has empty DefaultBaseURLs[ModelTypeRerank]", pt)
		}
	}

	// Cohere is rerank-only: it must not declare generative or embedding.
	cohere, err := r.Get(ProviderCohere)
	if err != nil {
		t.Fatalf("Get(cohere): %v", err)
	}
	for _, mt := range cohere.ModelTypes {
		if mt == ModelTypeGenerative || mt == ModelTypeEmbedding {
			t.Errorf("cohere is rerank-only but declares model type %q", mt)
		}
	}
}

func TestDefinitionRerankAPIJSON(t *testing.T) {
	def := &ProviderDefinition{
		Type:      ProviderCohere,
		RerankAPI: RerankAPICohere,
	}

	b, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTripped map[string]any
	if err := json.Unmarshal(b, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, ok := roundTripped["rerankApi"]; !ok || got != string(RerankAPICohere) {
		t.Errorf("expected rerankApi key with value %q, got %v", RerankAPICohere, roundTripped["rerankApi"])
	}

	// Empty RerankAPI must be omitted.
	empty := &ProviderDefinition{Type: ProviderOpenAI}
	b2, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	var m2 map[string]any
	if err := json.Unmarshal(b2, &m2); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if _, ok := m2["rerankApi"]; ok {
		t.Errorf("expected rerankApi to be omitted when empty, got %v", m2)
	}
}
