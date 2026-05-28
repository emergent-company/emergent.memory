package discoveryjobs

import (
	"context"
	"iter"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type testModelFactory struct {
	model model.LLM
	err   error
}

func (f *testModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	return f.model, f.err
}

type testLLM struct {
	response string
	err      error
}

func (m *testLLM) Name() string { return "test" }

func (m *testLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
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

func TestCompleteWithLLM(t *testing.T) {
	svc := &Service{
		modelFactory: &testModelFactory{
			model: &testLLM{response: `{"domainContext":"test","typeHints":{"A":"a"}}`},
		},
		log: slog.Default(),
	}

	result, err := svc.completeWithLLM(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"domainContext":"test","typeHints":{"A":"a"}}` {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestCompleteWithLLM_NilModelFactory(t *testing.T) {
	svc := &Service{
		modelFactory: nil,
		log:          slog.Default(),
	}

	_, err := svc.completeWithLLM(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for nil modelFactory")
	}
}

func TestCompleteWithLLM_EmptyResponse(t *testing.T) {
	svc := &Service{
		modelFactory: &testModelFactory{
			model: &testLLM{response: ""},
		},
		log: slog.Default(),
	}

	result, err := svc.completeWithLLM(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got: %q", result)
	}
}

func TestCompleteWithLLM_MultipleParts(t *testing.T) {
	mock := &multiPartLLM{responses: []string{"part1", " part2"}}
	svc := &Service{
		modelFactory: &testModelFactory{model: mock},
		log:          slog.Default(),
	}

	result, err := svc.completeWithLLM(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "part1 part2" {
		t.Fatalf("expected 'part1 part2', got: %q", result)
	}
}

type multiPartLLM struct {
	responses []string
}

func (m *multiPartLLM) Name() string { return "test" }

func (m *multiPartLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, r := range m.responses {
			if !yield(&model.LLMResponse{
				Content: genai.NewContentFromText(r, "model"),
			}, nil) {
				return
			}
		}
	}
}

func TestGenerateExtractionPrompts(t *testing.T) {
	svc := &Service{
		modelFactory: &testModelFactory{
			model: &testLLM{response: `{"domainContext":"test domain","typeHints":{"Entity":"extract entities"},"relationshipHints":{"rel":"extract rels"},"negativeExamples":["skip this"]}`},
		},
		log: slog.Default(),
	}

	types := []DiscoveredType{
		{TypeName: "Entity", Description: "An entity"},
	}
	rels := []DiscoveredRelationship{
		{SourceType: "Entity", TargetType: "Entity", RelationType: "rel", Description: "a relation"},
	}

	prompts, err := svc.generateExtractionPrompts(context.Background(), types, rels, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompts.DomainContext != "test domain" {
		t.Fatalf("unexpected domainContext: %q", prompts.DomainContext)
	}
	if len(prompts.TypeHints) != 1 || prompts.TypeHints["Entity"] != "extract entities" {
		t.Fatalf("unexpected typeHints: %v", prompts.TypeHints)
	}
	if len(prompts.RelationshipHints) != 1 || prompts.RelationshipHints["rel"] != "extract rels" {
		t.Fatalf("unexpected relationshipHints: %v", prompts.RelationshipHints)
	}
	if len(prompts.NegativeExamples) != 1 || prompts.NegativeExamples[0] != "skip this" {
		t.Fatalf("unexpected negativeExamples: %v", prompts.NegativeExamples)
	}
}

func TestGenerateExtractionPrompts_NilModelFactory(t *testing.T) {
	svc := &Service{
		modelFactory: nil,
		log:          slog.Default(),
	}

	prompts, err := svc.generateExtractionPrompts(context.Background(), nil, nil, "test")
	if err != nil {
		t.Fatalf("expected nil error for nil modelFactory, got: %v", err)
	}
	if prompts != nil {
		t.Fatalf("expected nil prompts for nil modelFactory, got: %v", prompts)
	}
}

func TestGenerateExtractionPrompts_InvalidJSON(t *testing.T) {
	svc := &Service{
		modelFactory: &testModelFactory{
			model: &testLLM{response: `not json`},
		},
		log: slog.Default(),
	}

	_, err := svc.generateExtractionPrompts(context.Background(), nil, nil, "test")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGenerateExtractionPrompts_WithMarkdownFences(t *testing.T) {
	svc := &Service{
		modelFactory: &testModelFactory{
			model: &testLLM{response: "```json\n{\"domainContext\":\"test\",\"typeHints\":{},\"relationshipHints\":{},\"negativeExamples\":[]}\n```"},
		},
		log: slog.Default(),
	}

	prompts, err := svc.generateExtractionPrompts(context.Background(), nil, nil, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompts.DomainContext != "test" {
		t.Fatalf("unexpected domainContext: %q", prompts.DomainContext)
	}
}

// ---------------------------------------------------------------------------
// isReifiedType
// ---------------------------------------------------------------------------

func TestIsReifiedType_RejectsRelationshipSuffix(t *testing.T) {
	cases := []string{
		"ReportingRelationship",
		"ManagementRelationship",
		"Relationship",
	}
	for _, name := range cases {
		if !isReifiedType(name) {
			t.Errorf("isReifiedType(%q) = false, want true", name)
		}
	}
}

func TestIsReifiedType_RejectsAssociationSuffix(t *testing.T) {
	cases := []string{
		"EmployeeAssociation",
		"VendorAssociation",
		"Association",
	}
	for _, name := range cases {
		if !isReifiedType(name) {
			t.Errorf("isReifiedType(%q) = false, want true", name)
		}
	}
}

func TestIsReifiedType_AllowsLegitimateEntityTypes(t *testing.T) {
	cases := []string{
		"Person",
		"Organization",
		"Department",
		"CalendarEvent",   // "Event" alone is NOT filtered — only Relationship/Association
		"MeetingActivity", // "Activity" alone is NOT filtered
		"Connection",      // NOT filtered — ambiguous domain noun
	}
	for _, name := range cases {
		if isReifiedType(name) {
			t.Errorf("isReifiedType(%q) = true, want false", name)
		}
	}
}

// ---------------------------------------------------------------------------
// normalizePropertyCrossRefs
// ---------------------------------------------------------------------------

func TestNormalizePropertyCrossRefs_NilPassthrough(t *testing.T) {
	if normalizePropertyCrossRefs(nil) != nil {
		t.Error("nil input should return nil")
	}
}

func TestNormalizePropertyCrossRefs_ScalarJSONSchemaKept(t *testing.T) {
	props := map[string]any{
		"name": map[string]any{"type": "string"},
		"age":  map[string]any{"type": "integer"},
	}
	out := normalizePropertyCrossRefs(props)
	if _, ok := out["name"].(map[string]any); !ok {
		t.Error("scalar JSON Schema property should be preserved as-is")
	}
	typeVal := out["name"].(map[string]any)["type"]
	if typeVal != "string" {
		t.Errorf("expected type=string, got %v", typeVal)
	}
}

func TestNormalizePropertyCrossRefs_EmbeddedObjectFlattened(t *testing.T) {
	// An embedded object without a scalar "type" field should become a string ref.
	props := map[string]any{
		"manager": map[string]any{
			"id":   map[string]any{"type": "string"},
			"name": map[string]any{"type": "string"},
		},
	}
	out := normalizePropertyCrossRefs(props)
	replaced, ok := out["manager"].(map[string]any)
	if !ok {
		t.Fatal("manager key missing from output")
	}
	if replaced["type"] != "string" {
		t.Errorf("expected flattened type=string, got %v", replaced["type"])
	}
	desc, _ := replaced["description"].(string)
	if !strings.Contains(desc, "manager") {
		t.Errorf("expected description to reference key name, got %q", desc)
	}
}

func TestNormalizePropertyCrossRefs_NonObjectPassthrough(t *testing.T) {
	// Non-map values should pass through unchanged.
	props := map[string]any{
		"count": 42,
		"flag":  true,
	}
	out := normalizePropertyCrossRefs(props)
	if out["count"] != 42 {
		t.Errorf("integer value should pass through, got %v", out["count"])
	}
}

// TestBuildTypeDiscoveryPrompt_IncludesKBPurpose verifies assumption D1:
// the kbPurpose string passed to buildTypeDiscoveryPrompt appears verbatim
// in the returned prompt, so the LLM receives domain context.
func TestBuildTypeDiscoveryPrompt_IncludesKBPurpose(t *testing.T) {
	s := &Service{log: slog.Default()}
	kbPurpose := "HR org-chart system tracking employee reporting lines, departments, and team structures"
	docs := []DocumentContent{
		{Content: "Alice reports to Bob. Bob is VP of Engineering."},
	}
	prompt := s.buildTypeDiscoveryPrompt(docs, kbPurpose)
	if !strings.Contains(prompt, kbPurpose) {
		t.Errorf("D1 FAIL: kbPurpose not found in prompt\nkbPurpose: %q\nprompt (first 500 chars): %.500s", kbPurpose, prompt)
	}
	t.Log("D1 PASS: kbPurpose appears verbatim in buildTypeDiscoveryPrompt output")
}
