package provider

import "testing"

// TestGenerativeProviderOrder_RegistryDerived verifies generative
// auto-selection order keeps the legacy preference first and then includes
// non-legacy registry vendors by Order, while omitting vendors that do not
// serve generative models.
func TestGenerativeProviderOrder_RegistryDerived(t *testing.T) {
	order := generativeProviderOrder()
	pos := make(map[ProviderType]int, len(order))
	for i, p := range order {
		pos[p] = i
	}

	// Legacy preference is preserved.
	if pos[ProviderDeepSeek] > pos[ProviderOpenAI] ||
		pos[ProviderOpenAI] > pos[ProviderVertexAI] ||
		pos[ProviderVertexAI] > pos[ProviderGoogleAI] {
		t.Errorf("legacy generative preference not preserved: %v", order)
	}

	// A non-legacy generative vendor is selectable.
	anthropic, ok := pos[ProviderAnthropic]
	if !ok {
		t.Fatalf("generative order missing non-legacy vendor %q: %v", ProviderAnthropic, order)
	}
	if anthropic < pos[ProviderGoogleAI] {
		t.Errorf("non-legacy vendor %q should follow the legacy preference: %v", ProviderAnthropic, order)
	}

	// Embedding-only vendor must not appear in generative order.
	if _, ok := pos[ProviderJina]; ok {
		t.Errorf("embedding-only vendor %q must not be in generative order", ProviderJina)
	}
}

// TestEmbeddingProviderOrder_RegistryDerived verifies embedding
// auto-selection order keeps the legacy preference first and then includes
// non-legacy registry vendors that serve embeddings, while omitting
// generative-only vendors.
func TestEmbeddingProviderOrder_RegistryDerived(t *testing.T) {
	order := embeddingProviderOrder()
	pos := make(map[ProviderType]int, len(order))
	for i, p := range order {
		pos[p] = i
	}

	if pos[ProviderGoogleAI] > pos[ProviderVertexAI] ||
		pos[ProviderVertexAI] > pos[ProviderOpenAI] ||
		pos[ProviderOpenAI] > pos[ProviderDeepSeek] {
		t.Errorf("legacy embedding preference not preserved: %v", order)
	}

	// A non-legacy embedding vendor is selectable.
	jina, ok := pos[ProviderJina]
	if !ok {
		t.Fatalf("embedding order missing non-legacy vendor %q: %v", ProviderJina, order)
	}
	if jina < pos[ProviderDeepSeek] {
		t.Errorf("non-legacy embedding vendor %q should follow the legacy preference: %v", ProviderJina, order)
	}

	// Generative-only vendor must not appear in embedding order.
	if _, ok := pos[ProviderAnthropic]; ok {
		t.Errorf("generative-only vendor %q must not be in embedding order", ProviderAnthropic)
	}
}
