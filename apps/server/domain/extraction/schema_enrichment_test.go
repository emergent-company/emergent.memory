package extraction

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type stubLLM struct {
	response string
	err      error
}

func (m *stubLLM) Name() string { return "stub" }

func (m *stubLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{
			Content: genai.NewContentFromText(m.response, "model"),
		}, nil)
	}
}

// ---------------------------------------------------------------------------
// extractJSONObject
// ---------------------------------------------------------------------------

func TestExtractJSONObject_PureJSON(t *testing.T) {
	input := `{"name":"Alice","age":30}`
	got := extractJSONObject(input)
	if got != input {
		t.Fatalf("expected %q, got %q", input, got)
	}
}

func TestExtractJSONObject_ProsePreamble(t *testing.T) {
	input := `We need to extract information about the characters.
{"name":"Alice","age":30}`
	want := `{"name":"Alice","age":30}`
	got := extractJSONObject(input)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExtractJSONObject_MarkdownFencesJSON(t *testing.T) {
	input := "```json\n{\"name\":\"Alice\"}\n```"
	got := extractJSONObject(input)
	want := `{"name":"Alice"}`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExtractJSONObject_MarkdownFencesGeneric(t *testing.T) {
	input := "```\n{\"name\":\"Alice\"}\n```"
	got := extractJSONObject(input)
	want := `{"name":"Alice"}`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExtractJSONObject_NestedBraces(t *testing.T) {
	input := `{"outer":{"inner":"value"},"arr":[1,2,{"deep":3}]}`
	got := extractJSONObject(input)
	if got != input {
		t.Fatalf("expected full input, got %q", got)
	}
}

func TestExtractJSONObject_StringWithBraces(t *testing.T) {
	input := `{"name":"some {thing} with braces"}`
	got := extractJSONObject(input)
	if got != input {
		t.Fatalf("expected full input, got %q", got)
	}
}

func TestExtractJSONObject_EscapedQuotes(t *testing.T) {
	input := `{"name":"escaped \"{braces}\" inside"}`
	got := extractJSONObject(input)
	if got != input {
		t.Fatalf("expected full input, got %q", got)
	}
}

func TestExtractJSONObject_NoJSON(t *testing.T) {
	input := "just some text without braces"
	got := extractJSONObject(input)
	if got != input {
		t.Fatalf("expected input unchanged, got %q", got)
	}
}

func TestExtractJSONObject_EmptyString(t *testing.T) {
	got := extractJSONObject("")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractJSONObject_DeepseekStylePreamble(t *testing.T) {
	input := `We need to create a schema for this document. Let me think through what types make sense.

{
  "types": [
    {
      "name": "Character",
      "description": "A person in the story"
    }
  ]
}`
	want := `{
  "types": [
    {
      "name": "Character",
      "description": "A person in the story"
    }
  ]
}`
	got := extractJSONObject(input)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// isRelationshipObjectType
// ---------------------------------------------------------------------------

func TestIsRelationshipObjectType_RejectsBondTypes(t *testing.T) {
	cases := []string{"Relationship", "Bond", "Connection", "Link", "Association", "Relation",
		"relationship", "RELATIONSHIP", "bond", "BOND", "connection", "link", "association", "relation"}
	for _, name := range cases {
		if !IsRelationshipObjectType(name) {
			t.Errorf("expected %q to be rejected as relationship type", name)
		}
	}
}

func TestIsRelationshipObjectType_AllowsEntityTypes(t *testing.T) {
	cases := []string{"Character", "Event", "Place", "Organization", "Episode", "RelationshipManager"}
	for _, name := range cases {
		if IsRelationshipObjectType(name) {
			t.Errorf("expected %q to be allowed, but was rejected", name)
		}
	}
}

// ---------------------------------------------------------------------------
// EnrichSchemaProperties
// ---------------------------------------------------------------------------

func TestEnrichSchemaProperties_AllTypesHaveProperties_NoLLMCall(t *testing.T) {
	existing := map[string]any{
		"Character": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
	}
	llm := &stubLLM{response: "should not be called"}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 type, got %d", len(got))
	}
	if &got == &existing {
		t.Fatal("must return a different map (shallow copy)")
	}
}

func TestEnrichSchemaProperties_SomeTypesMissingProperties_FillsGaps(t *testing.T) {
	existing := map[string]any{
		"Character": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
		"Event": map[string]any{
			"type":        "object",
			"properties":  nil,
			"description": "Something that happens",
		},
	}
	llm := &stubLLM{response: `{"Event":{"description":"Something that happens","properties":{"name":{"type":"string","description":"event name"},"date":{"type":"string","description":"when it happened"}},"required":["name"]}}`}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 types, got %d", len(got))
	}
	event := got["Event"].(map[string]any)
	props := event["properties"].(map[string]any)
	if len(props) == 0 {
		t.Fatal("Event should have properties after enrichment")
	}
	char := got["Character"].(map[string]any)
	if _, ok := char["properties"].(map[string]any)["name"]; !ok {
		t.Fatal("Character should keep its 'name' property")
	}
}

func TestEnrichSchemaProperties_LLMHallucinatesUnknownType_Filtered(t *testing.T) {
	existing := map[string]any{
		"Character": map[string]any{
			"type":       "object",
			"properties": nil,
		},
	}
	llm := &stubLLM{response: `{"Character":{"description":"A person","properties":{"name":{"type":"string"}},"required":["name"]},"HallucinatedType":{"description":"not in input","properties":{"x":{"type":"string"}},"required":["x"]}}`}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["HallucinatedType"]; ok {
		t.Fatal("HallucinatedType should be filtered out")
	}
}

func TestEnrichSchemaProperties_LLMReturnsInvalidJSON_Error(t *testing.T) {
	existing := map[string]any{
		"Character": map[string]any{
			"type":       "object",
			"properties": nil,
		},
	}
	llm := &stubLLM{response: "not json at all"}
	_, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestEnrichSchemaProperties_NoSparseTypes_NoLLMCall(t *testing.T) {
	existing := map[string]any{
		"A": map[string]any{
			"type":       "object",
			"properties": map[string]any{"a": map[string]any{"type": "string"}},
		},
		"B": map[string]any{
			"type":       "object",
			"properties": map[string]any{"b": map[string]any{"type": "number"}},
		},
	}
	llm := &stubLLM{response: "should not be called"}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 types, got %d", len(got))
	}
}

func TestEnrichSchemaProperties_EmptyPropertiesMap_TreatedAsSparse(t *testing.T) {
	existing := map[string]any{
		"Character": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
	llm := &stubLLM{response: `{"Character":{"description":"A person","properties":{"name":{"type":"string","description":"the name"}},"required":["name"]}}`}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	char := got["Character"].(map[string]any)
	props := char["properties"].(map[string]any)
	if len(props) == 0 {
		t.Fatal("expected properties to be filled for Character with empty map")
	}
}

func TestEnrichSchemaProperties_NonMapEntry_Skipped(t *testing.T) {
	existing := map[string]any{
		"Character": "not a map",
		"Event": map[string]any{
			"type":       "object",
			"properties": nil,
		},
	}
	llm := &stubLLM{response: `{"Event":{"description":"An event","properties":{"name":{"type":"string","description":"name"}},"required":["name"]}}`}
	got, err := EnrichSchemaProperties(context.Background(), "doc text", "test", existing, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["Character"]; !ok {
		t.Fatal("Character should still be present (passthrough)")
	}
	// Event should now have properties.
	ev := got["Event"].(map[string]any)
	if _, has := ev["properties"]; !has {
		t.Fatal("Event should have properties after enrichment")
	}
}

// ---------------------------------------------------------------------------
// GenerateSchemaFromDocument
// ---------------------------------------------------------------------------

func TestGenerateSchemaFromDocument_ValidResponse(t *testing.T) {
	llm := &stubLLM{response: `{"types":[{"name":"Character","description":"A person","properties":{"name":{"type":"string","description":"the name"}},"required":["name"]},{"name":"Event","description":"Something that happens","properties":{"date":{"type":"string","description":"when"}},"required":["date"]}]}`}
	got, err := GenerateSchemaFromDocument(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 types, got %d", len(got))
	}
	for _, name := range []string{"Character", "Event"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected %q in output", name)
		}
		entry, ok := got[name].(map[string]any)
		if !ok {
			t.Fatalf("%q entry is not a map", name)
		}
		if entry["type"] != "object" {
			t.Errorf("%q missing type='object'", name)
		}
		if entry["required"] == nil {
			t.Errorf("%q missing required", name)
		}
		if entry["properties"] == nil {
			t.Errorf("%q missing properties", name)
		}
	}
}

func TestGenerateSchemaFromDocument_FiltersRelationshipType(t *testing.T) {
	llm := &stubLLM{response: `{"types":[{"name":"Character","description":"A person","properties":{"name":{"type":"string"}},"required":["name"]},{"name":"Relationship","description":"A bond","properties":{"type":{"type":"string"}},"required":["type"]}]}`}
	got, err := GenerateSchemaFromDocument(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["Relationship"]; ok {
		t.Fatal("Relationship type should be filtered out")
	}
	if _, ok := got["Character"]; !ok {
		t.Fatal("Character should be present")
	}
}

func TestGenerateSchemaFromDocument_EmptyTypesList_Error(t *testing.T) {
	llm := &stubLLM{response: `{"types":[]}`}
	_, err := GenerateSchemaFromDocument(context.Background(), "doc text", "test", llm)
	if err == nil {
		t.Fatal("expected error for empty types list")
	}
}

func TestGenerateSchemaFromDocument_InvalidJSONWithRetrySucceeds(t *testing.T) {
	llm := &callCountingLLM{responses: []string{
		"some text without valid json",
		`{"types":[{"name":"Character","description":"A person","properties":{"name":{"type":"string"}},"required":["name"]}]}`,
	}}
	got, err := GenerateSchemaFromDocument(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 type after retry, got %d", len(got))
	}
}

func TestGenerateSchemaFromDocument_EmptyName_Skipped(t *testing.T) {
	llm := &stubLLM{response: `{"types":[{"name":"","description":"empty","properties":{"x":{"type":"string"}},"required":["x"]},{"name":"Character","description":"valid","properties":{"name":{"type":"string"}},"required":["name"]}]}`}
	got, err := GenerateSchemaFromDocument(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got[""]; ok {
		t.Fatal("empty-name type should be filtered out")
	}
	if _, ok := got["Character"]; !ok {
		t.Fatal("Character should be present")
	}
}

// ---------------------------------------------------------------------------
// GenerateSchemaWithRelationships
// ---------------------------------------------------------------------------

func TestGenerateSchemaWithRelationships_ValidCombined(t *testing.T) {
	llm := &stubLLM{response: `{
  "types": [
    {"name":"Character","description":"A person","properties":{"name":{"type":"string","description":"name"}},"required":["name"]}
  ],
  "relationships": [
    {"name":"KNOWS","sourceType":"Character","targetType":"Character","cardinality":"many-to-many","description":"two characters know each other","inverseType":"KNOWN_BY","inverseLabel":"known by"}
  ]
}`}
	result, err := GenerateSchemaWithRelationships(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ObjectTypes) != 1 {
		t.Fatalf("expected 1 object type, got %d", len(result.ObjectTypes))
	}
	if len(result.RelationshipTypes) != 1 {
		t.Fatalf("expected 1 relationship type, got %d", len(result.RelationshipTypes))
	}
	rel := result.RelationshipTypes["KNOWS"].(map[string]any)
	if rel["inverseType"] != "KNOWN_BY" {
		t.Fatalf("expected inverseType=KNOWN_BY, got %v", rel["inverseType"])
	}
	if rel["inverseLabel"] != "known by" {
		t.Fatalf("expected inverseLabel='known by', got %v", rel["inverseLabel"])
	}
}

func TestGenerateSchemaWithRelationships_FiltersRelationshipType(t *testing.T) {
	llm := &stubLLM{response: `{
  "types": [
    {"name":"Relationship","description":"bond","properties":{"type":{"type":"string"}},"required":["type"]},
    {"name":"Character","description":"A person","properties":{"name":{"type":"string"}},"required":["name"]}
  ],
  "relationships": []
}`}
	result, err := GenerateSchemaWithRelationships(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.ObjectTypes["Relationship"]; ok {
		t.Fatal("Relationship object type should be filtered out")
	}
}

func TestGenerateSchemaWithRelationships_InvalidJSON_Error(t *testing.T) {
	llm := &stubLLM{response: "not json"}
	_, err := GenerateSchemaWithRelationships(context.Background(), "doc text", "test", llm)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGenerateSchemaWithRelationships_EmptyNameSkipped(t *testing.T) {
	llm := &stubLLM{response: `{
  "types": [{"name":"Character","description":"A person","properties":{"n":{"type":"string"}},"required":["n"]}],
  "relationships": [{"name":"","sourceType":"Character","targetType":"Character","cardinality":"one-to-one","description":"empty"}]
}`}
	result, err := GenerateSchemaWithRelationships(context.Background(), "doc text", "test", llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.RelationshipTypes[""]; ok {
		t.Fatal("empty-name relationship should be filtered out")
	}
}

// ---------------------------------------------------------------------------
// GenerateRelationshipsFromSchema
// ---------------------------------------------------------------------------

func TestGenerateRelationshipsFromSchema_ValidResponse(t *testing.T) {
	llm := &stubLLM{response: `{"relationships":[
    {"name":"KNOWS","sourceType":"Character","targetType":"Character","cardinality":"many-to-many","description":"knows each other","inverseType":"KNOWN_BY","inverseLabel":"known by"}
  ]}`}
	got, err := GenerateRelationshipsFromSchema(context.Background(), "doc text", "test", []string{"Character", "Event"}, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(got))
	}
	rel := got["KNOWS"].(map[string]any)
	if rel["inverseType"] != "KNOWN_BY" {
		t.Fatalf("expected inverseType=KNOWN_BY, got %v", rel["inverseType"])
	}
}

func TestGenerateRelationshipsFromSchema_EmptyNamesSkipped(t *testing.T) {
	llm := &stubLLM{response: `{"relationships":[
    {"name":"","sourceType":"Character","targetType":"Character","cardinality":"many-to-many","description":"empty name","inverseType":"X","inverseLabel":"x"},
    {"name":"KNOWS","sourceType":"Character","targetType":"Character","cardinality":"many-to-many","description":"valid","inverseType":"KNOWN_BY","inverseLabel":"known by"}
  ]}`}
	got, err := GenerateRelationshipsFromSchema(context.Background(), "doc text", "test", []string{"Character"}, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 relationship (empty name filtered), got %d", len(got))
	}
}

func TestGenerateRelationshipsFromSchema_RetrySucceeds(t *testing.T) {
	llm := &multiResponseLLM{responses: []string{
		"not valid json",
		`{"relationships":[{"name":"KNOWS","sourceType":"Character","targetType":"Character","cardinality":"many-to-many","description":"test","inverseType":"KNOWN_BY","inverseLabel":"known by"}]}`,
	}}
	got, err := GenerateRelationshipsFromSchema(context.Background(), "doc text", "test", []string{"Character"}, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 relationship after retry, got %d", len(got))
	}
}

func TestGenerateRelationshipsFromSchema_AllRetriesFail_Error(t *testing.T) {
	llm := &stubLLM{response: "not json at all"}
	_, err := GenerateRelationshipsFromSchema(context.Background(), "doc text", "test", []string{"Character"}, llm)
	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
}

// ---------------------------------------------------------------------------
// callLLMJSON and callLLM
// ---------------------------------------------------------------------------

func TestCallLLMJSON_ValidResponse(t *testing.T) {
	llm := &stubLLM{response: `{"key":"value"}`}
	got, err := callLLMJSON(context.Background(), llm, "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"key":"value"}` {
		t.Fatalf("expected '{\"key\":\"value\"}', got %q", got)
	}
}

func TestCallLLMJSON_LLMError(t *testing.T) {
	llm := &stubLLM{err: context.DeadlineExceeded}
	_, err := callLLMJSON(context.Background(), llm, "test prompt")
	if err == nil {
		t.Fatal("expected error from LLM")
	}
}

func TestCallLLM_ValidResponse(t *testing.T) {
	llm := &stubLLM{response: "simple string response"}
	got, err := callLLM(context.Background(), llm, "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "simple string response" {
		t.Fatalf("expected 'simple string response', got %q", got)
	}
}

func TestCallLLM_LLMError(t *testing.T) {
	llm := &stubLLM{err: context.Canceled}
	_, err := callLLM(context.Background(), llm, "test prompt")
	if err == nil {
		t.Fatal("expected error from LLM")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type callCountingLLM struct {
	responses []string
	call      int
}

func (m *callCountingLLM) Name() string { return "counter" }

func (m *callCountingLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.call >= len(m.responses) {
			yield(nil, nil)
			return
		}
		resp := m.responses[m.call]
		m.call++
		yield(&model.LLMResponse{
			Content: genai.NewContentFromText(resp, "model"),
		}, nil)
	}
}

type multiResponseLLM struct {
	responses []string
	call      int
}

func (m *multiResponseLLM) Name() string { return "multi" }

func (m *multiResponseLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.call >= len(m.responses) {
			yield(nil, nil)
			return
		}
		resp := m.responses[m.call]
		m.call++
		yield(&model.LLMResponse{
			Content: genai.NewContentFromText(resp, "model"),
		}, nil)
	}
}
