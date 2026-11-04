package discoveryjobs

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/model"
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

// ---------------------------------------------------------------------------
// loadDocumentText — GAP 5
// ---------------------------------------------------------------------------

func TestLoadDocumentText_EmptyDocID(t *testing.T) {
	s := &Service{log: slog.Default()}
	result := s.loadDocumentText(context.Background(), "")
	if result != "" {
		t.Errorf("loadDocumentText(\"\") should return empty string, got %q", result)
	}
}

func TestLoadDocumentText_NilDocSvc(t *testing.T) {
	s := &Service{log: slog.Default()}
	result := s.loadDocumentText(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
	if result != "" {
		t.Errorf("loadDocumentText with nil docSvc should return empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// buildTypeDiscoveryPrompt
// ---------------------------------------------------------------------------

// TestBuildTypeDiscoveryPrompt_IncludesKBPurpose verifies assumption D1:
// the kbPurpose string passed to buildTypeDiscoveryPrompt appears verbatim
// in the returned prompt, so the LLM receives domain context.
func TestBuildTypeDiscoveryPrompt_IncludesKBPurpose(t *testing.T) {
	s := &Service{log: slog.Default()}
	kbPurpose := "HR org-chart system tracking employee reporting lines, departments, and team structures"
	docs := []DocumentContent{
		{Content: "Alice reports to Bob. Bob is VP of Engineering."},
	}
	prompt := s.buildTypeDiscoveryPrompt(docs, kbPurpose, 0)
	if !strings.Contains(prompt, kbPurpose) {
		t.Errorf("D1 FAIL: kbPurpose not found in prompt\nkbPurpose: %q\nprompt (first 500 chars): %.500s", kbPurpose, prompt)
	}
	t.Log("D1 PASS: kbPurpose appears verbatim in buildTypeDiscoveryPrompt output")
}

// TestBuildTypeDiscoveryPrompt_TypeBudget pins the requested type count to the
// budget: the reduced budget is what makes a truncation retry shorter.
func TestBuildTypeDiscoveryPrompt_TypeBudget(t *testing.T) {
	s := testService()
	docs := []DocumentContent{{Content: "Alice reports to Bob."}}

	if p := s.buildTypeDiscoveryPrompt(docs, "hr", 0); !strings.Contains(p, "up to 20 important entity types") {
		t.Errorf("default budget not in prompt: %.200s", p)
	}
	if p := s.buildTypeDiscoveryPrompt(docs, "hr", discoveryReducedMaxTypes); !strings.Contains(p, "up to 5 important entity types") {
		t.Errorf("reduced budget not in prompt: %.200s", p)
	}
}

// TestDecideDiscoveryBatch_SingleDocumentTruncationRetriesWithFewerTypes
// covers the failure that stalled discovery on a 1-document job: a batch that
// cannot be split has to shrink the answer instead of failing outright.
func TestDecideDiscoveryBatch_SingleDocumentTruncationRetriesWithFewerTypes(t *testing.T) {
	s := testService()
	// A response cut off before the first type object completed, so nothing is
	// salvageable by JSON repair.
	raw := `{"discovered_types":[{"type_name":"Per`
	outcome := s.decideDiscoveryBatch(raw, ErrOutputTruncated, 1)
	if !outcome.RetryReducedTypes {
		t.Fatalf("single-document truncation should retry with a reduced type budget; got %+v", outcome)
	}
	if outcome.RetrySmaller {
		t.Fatal("a 1-document batch cannot be split, so RetrySmaller must be false")
	}
}

// TestDecideDiscoveryBatch_MultiDocumentTruncationSplits keeps the existing
// behaviour: a multi-document batch is split, not retried at a reduced budget.
func TestDecideDiscoveryBatch_MultiDocumentTruncationSplits(t *testing.T) {
	s := testService()
	raw := `{"discovered_types":[{"type_name":"Per`
	outcome := s.decideDiscoveryBatch(raw, ErrOutputTruncated, 4)
	if !outcome.RetrySmaller || outcome.RetryReducedTypes {
		t.Fatalf("multi-document truncation should split; got %+v", outcome)
	}
}

// TestDecideDiscoveryBatch_SingleDocumentEmptyBodyFails keeps an empty body out
// of the reduced-budget retry: it is not an output-limit failure, so retrying
// would repeat the same call for nothing.
func TestDecideDiscoveryBatch_SingleDocumentEmptyBodyFails(t *testing.T) {
	s := testService()
	outcome := s.decideDiscoveryBatch("", nil, 1)
	if outcome.RetryReducedTypes || outcome.RetrySmaller {
		t.Fatalf("empty body on a single document should fail, not retry; got %+v", outcome)
	}
	if outcome.Err == nil {
		t.Fatal("empty body should produce an error")
	}
}

// TestDecideDiscoveryBatch_NonTruncationParseErrorFails ensures a malformed
// (not truncated) response is a failure rather than a silent retry, which would
// hide a prompt/model problem behind repeated spend.
func TestDecideDiscoveryBatch_NonTruncationParseErrorFails(t *testing.T) {
	s := testService()
	outcome := s.decideDiscoveryBatch("not json at all", nil, 1)
	if outcome.RetryReducedTypes || outcome.RetrySmaller {
		t.Fatalf("a non-truncation parse error should fail; got %+v", outcome)
	}
	if outcome.Err == nil {
		t.Fatal("expected an error")
	}
}

// TestDecideDiscoveryBatch_CompleteEmptyAnswerIsValid pins the distinction the
// function was introduced for: a complete answer with zero types is not a
// failure (the job-level check reports "no entity types").
func TestDecideDiscoveryBatch_CompleteEmptyAnswerIsValid(t *testing.T) {
	s := testService()
	outcome := s.decideDiscoveryBatch(`{"discovered_types":[]}`, nil, 1)
	if outcome.Err != nil || outcome.RetryReducedTypes || outcome.RetrySmaller {
		t.Fatalf("complete empty answer should be valid; got %+v", outcome)
	}
	if len(outcome.Types) != 0 {
		t.Fatalf("expected no types, got %d", len(outcome.Types))
	}
}

// TestDocumentCap_DefaultsAndOverride pins the per-run document cap.
func TestDocumentCap_DefaultsAndOverride(t *testing.T) {
	if got := (&Service{}).documentCap(); got != DefaultDiscoveryMaxDocuments {
		t.Fatalf("zero Service cap = %d, want %d", got, DefaultDiscoveryMaxDocuments)
	}
	if got := (&Service{maxDocuments: 7}).documentCap(); got != 7 {
		t.Fatalf("explicit cap = %d, want 7", got)
	}
	t.Setenv("DISCOVERY_MAX_DOCUMENTS", "12")
	if got := maxDocumentsFromEnv(); got != 12 {
		t.Fatalf("env cap = %d, want 12", got)
	}
	t.Setenv("DISCOVERY_MAX_DOCUMENTS", "not-a-number")
	if got := maxDocumentsFromEnv(); got != DefaultDiscoveryMaxDocuments {
		t.Fatalf("invalid env cap = %d, want default", got)
	}
}

// ---------------------------------------------------------------------------
// Batch retry wiring: truncation -> split or reduced budget
// ---------------------------------------------------------------------------

// seqResponse is one scripted model answer.
type seqResponse struct {
	text   string
	finish genai.FinishReason
	err    error
}

// sequenceLLM answers with a scripted response per call and records each
// prompt, so a test can assert how many calls a retry took and what the retry
// actually asked for (the budget lives in the prompt text).
type sequenceLLM struct {
	responses []seqResponse
	prompts   []string
	calls     int
}

func (m *sequenceLLM) Name() string { return "sequence" }

func (m *sequenceLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if req != nil && len(req.Contents) > 0 && req.Contents[0] != nil && len(req.Contents[0].Parts) > 0 {
			m.prompts = append(m.prompts, req.Contents[0].Parts[0].Text)
		}
		idx := m.calls
		m.calls++
		if idx >= len(m.responses) {
			yield(nil, fmt.Errorf("unexpected model call %d", idx+1))
			return
		}
		next := m.responses[idx]
		if next.err != nil {
			yield(nil, next.err)
			return
		}
		resp := &model.LLMResponse{Content: genai.NewContentFromText(next.text, "model")}
		if next.finish != "" {
			resp.FinishReason = next.finish
		}
		yield(resp, nil)
	}
}

// truncatedPartial is what a response cut off mid-object looks like.
const truncatedPartial = `{"discovered_types":[{"type_name":"Cust`

// completeAnswer is a parseable answer proposing a single type.
const completeAnswer = `{"discovered_types":[{"type_name":"Customer","description":"A buyer","inferred_schema":{},"example_instances":["Acme"],"confidence":0.9,"occurrences":2}]}`

func newBatchTestService(llm model.LLM) *Service {
	return &Service{
		modelFactory: &testModelFactory{model: llm},
		log:          slog.Default(),
	}
}

func testDocs(n int) []DocumentContent {
	docs := make([]DocumentContent, n)
	for i := range docs {
		docs[i] = DocumentContent{
			ID:       uuid.New(),
			Filename: fmt.Sprintf("doc-%d.txt", i+1),
			Content:  fmt.Sprintf("document %d content", i+1),
		}
	}
	return docs
}

// allGroupTypes flattens the per-answer groups into one list, for assertions
// that care about the types rather than which answer produced them.
func allGroupTypes(groups []discoveredGroup) []DiscoveredType {
	var out []DiscoveredType
	for _, g := range groups {
		out = append(out, g.Types...)
	}
	return out
}

// TestDiscoverBatchTypes_ReducedBudgetRetry pins the wiring, not just the
// decision: a one-document truncation makes exactly one more call, and that
// call asks for the reduced type budget.
func TestDiscoverBatchTypes_ReducedBudgetRetry(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
		{text: completeAnswer},
	}}
	svc := newBatchTestService(llm)

	groups, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	types := allGroupTypes(groups)
	if len(types) != 1 || types[0].TypeName != "Customer" {
		t.Fatalf("types = %+v, want one Customer", types)
	}
	if len(groups) != 1 || len(groups[0].DocumentIDs) != 1 {
		t.Fatalf("groups = %+v, want one group over the single document", groups)
	}
	if llm.calls != 2 {
		t.Fatalf("model calls = %d, want 2 (default budget, then reduced)", llm.calls)
	}
	if !strings.Contains(llm.prompts[0], fmt.Sprintf("up to %d important entity types", discoveryDefaultMaxTypes)) {
		t.Fatalf("first prompt should ask for the default budget:\n%s", llm.prompts[0])
	}
	if !strings.Contains(llm.prompts[1], fmt.Sprintf("up to %d important entity types", discoveryReducedMaxTypes)) {
		t.Fatalf("retry prompt should ask for the reduced budget:\n%s", llm.prompts[1])
	}
}

// TestDiscoverBatchTypes_ReducedBudgetStops pins the stop guard: a reduced
// attempt that is still truncated fails, and it does not loop a third time.
func TestDiscoverBatchTypes_ReducedBudgetStops(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
	}}
	svc := newBatchTestService(llm)

	_, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", discoveryReducedMaxTypes)
	if err == nil {
		t.Fatal("expected an error for a still-truncated reduced attempt")
	}
	if !strings.Contains(err.Error(), "output limit") {
		t.Fatalf("error = %v, want it to name the output limit", err)
	}
	if llm.calls != 1 {
		t.Fatalf("model calls = %d, want 1: a retry already at the reduced budget must not retry again", llm.calls)
	}
}

// TestDiscoverBatchTypes_ReducedBudgetStopsAfterSecondTruncation pins the
// two-call ceiling when the caller started at the default budget.
func TestDiscoverBatchTypes_ReducedBudgetStopsAfterSecondTruncation(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
	}}
	svc := newBatchTestService(llm)

	_, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0)
	if err == nil {
		t.Fatal("expected an error when both attempts truncate")
	}
	if llm.calls != 2 {
		t.Fatalf("model calls = %d, want exactly 2", llm.calls)
	}
	if !strings.Contains(err.Error(), "output limit") {
		t.Fatalf("error = %v, want it to name the output limit", err)
	}
}

// TestDiscoverBatchTypes_MultiDocumentSplitKeepsBudget pins the other retry
// shape: a truncated multi-document batch is split, and the halves keep the
// caller's budget (splitting is the fix there, not a smaller ask).
func TestDiscoverBatchTypes_MultiDocumentSplitKeepsBudget(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{text: truncatedPartial, finish: genai.FinishReasonMaxTokens},
		{text: completeAnswer},
		{text: completeAnswer},
	}}
	svc := newBatchTestService(llm)

	docs := testDocs(2)
	groups, err := svc.discoverBatchTypes(context.Background(), docs, 1, "test purpose", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	types := allGroupTypes(groups)
	if len(types) != 2 {
		t.Fatalf("types = %d, want 2 (one per half)", len(types))
	}
	// Provenance must not be the whole original batch: each half's answer is
	// attributed to the half's own document.
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (one per answered half)", len(groups))
	}
	for i, g := range groups {
		if len(g.DocumentIDs) != 1 || g.DocumentIDs[0] != docs[i].ID {
			t.Fatalf("group %d documents = %v, want [%s]", i, g.DocumentIDs, docs[i].ID)
		}
	}
	if llm.calls != 3 {
		t.Fatalf("model calls = %d, want 3 (original + two halves)", llm.calls)
	}
	for _, prompt := range llm.prompts[1:] {
		if strings.Contains(prompt, fmt.Sprintf("up to %d important entity types", discoveryReducedMaxTypes)) {
			t.Fatalf("a split retry must not lower the type budget:\n%s", prompt)
		}
	}
}

// TestDiscoverBatchTypes_CompleteEmptyAnswerIsNotAFailure pins the distinction
// the retry logic depends on: a complete answer that found no types is stored
// as zero types, not retried and not failed.
func TestDiscoverBatchTypes_CompleteEmptyAnswerIsNotAFailure(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{{text: `{"discovered_types":[]}`}}}
	svc := newBatchTestService(llm)

	groups, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types := allGroupTypes(groups); len(types) != 0 {
		t.Fatalf("types = %+v, want none", types)
	}
	if llm.calls != 1 {
		t.Fatalf("model calls = %d, want 1", llm.calls)
	}
}

// TestIsTerminalStatus pins the vocabulary the cancellation fencing depends on:
// every write the worker makes is refused once the job is terminal, so a status
// classified wrongly here would either strand a live run or let a cancelled one
// finish into a second pack.
// TestStatusPredicatesAreDerived pins the single vocabulary: the fence's
// non-terminal predicate and the cancellable set are filtered from AllStatuses,
// so a new status cannot leave one of them disagreeing with
// IsTerminalStatus/IsCancellableStatus (which the card and the API both mirror).
func TestStatusPredicatesAreDerived(t *testing.T) {
	contains := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}

	all := AllStatuses()
	if len(all) != 8 {
		t.Fatalf("AllStatuses() = %v, want the 8 lifecycle statuses", all)
	}
	for _, status := range all {
		if got, want := contains(nonTerminalStatuses, status), !IsTerminalStatus(status); got != want {
			t.Errorf("nonTerminalStatuses contains %q = %v, want %v", status, got, want)
		}
		if got, want := contains(cancellableStatuses, status), IsCancellableStatus(status); got != want {
			t.Errorf("cancellableStatuses contains %q = %v, want %v", status, got, want)
		}
	}

	// The boundary the card and the 409 message rest on.
	if IsCancellableStatus(StatusCreatingPack) {
		t.Error("a run that is creating its pack is not cancellable")
	}
	if !IsCancellableStatus(StatusExtractingTypes) {
		t.Error("a run that is extracting types is cancellable")
	}
	for _, terminal := range []string{StatusCompleted, StatusFailed, StatusCancelled} {
		if IsCancellableStatus(terminal) {
			t.Errorf("%q is terminal and must not be cancellable", terminal)
		}
	}
}

func TestIsTerminalStatus(t *testing.T) {
	for _, status := range []string{StatusCompleted, StatusFailed, StatusCancelled} {
		if !IsTerminalStatus(status) {
			t.Errorf("IsTerminalStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{StatusPending, StatusAnalyzingDocuments, StatusExtractingTypes, StatusRefiningSchemas, StatusCreatingPack} {
		if IsTerminalStatus(status) {
			t.Errorf("IsTerminalStatus(%q) = true, want false", status)
		}
	}
}
