// varR bench test: run production ExtractionPipeline (Gemini + ResponseSchema)
// on the extract-bench synthetic conversation and compute entity/rel F1.
//
// Usage:
//
//	GOOGLE_API_KEY=xxx go test -v -run TestVarR_ExtractBench -timeout 120s ./apps/server/domain/extraction/agents/
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// varrGroundTruth mirrors the bench/extract-bench/ground_truth.json structure.
type varrGTEntity struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type varrGTRel struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

type varrGroundTruth struct {
	Entities      []varrGTEntity `json:"entities"`
	Relationships []varrGTRel    `json:"relationships"`
}

func TestVarR_ExtractBench(t *testing.T) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		t.Skip("GOOGLE_API_KEY not set")
	}

	// Locate bench dir relative to this test file
	_, testFile, _, _ := runtime.Caller(0)
	// testFile = .../apps/server/domain/extraction/agents/varr_bench_test.go
	// bench dir = 5 levels up + /bench/extract-bench/
	benchDir := filepath.Join(filepath.Dir(testFile), "../../../../../bench/extract-bench")

	// Load ground truth
	gtPath := filepath.Join(benchDir, "ground_truth.json")
	gtData, err := os.ReadFile(gtPath)
	if err != nil {
		t.Fatalf("read ground truth: %v", err)
	}
	var gt varrGroundTruth
	if err := json.Unmarshal(gtData, &gt); err != nil {
		t.Fatalf("parse ground truth: %v", err)
	}

	// Load conversation
	convPath := filepath.Join(benchDir, "synthetic_conversation.txt")
	convData, err := os.ReadFile(convPath)
	if err != nil {
		t.Fatalf("read conversation: %v", err)
	}
	conversation := string(convData)

	// Build open-schema (no type enum restriction — mimics no-schema mode)
	// to match bench conditions where we don't know schema in advance.
	objectSchemas := map[string]ObjectSchema{
		"Person":       {Name: "Person", Description: "A human individual mentioned in the conversation"},
		"Place":        {Name: "Place", Description: "A city, location, or geographic entity"},
		"Organization": {Name: "Organization", Description: "A company, institution, or workplace"},
		"Event":        {Name: "Event", Description: "A notable occurrence, meeting, or gathering"},
		"Date":         {Name: "Date", Description: "A specific date, month, or time period"},
		"Object":       {Name: "Object", Description: "A physical object or animal"},
	}

	relationshipSchemas := map[string]RelationshipSchema{
		"lives_in": {
			Name:                 "lives_in",
			Description:          "Person currently resides in a Place",
			SourceTypes:          []string{"Person"},
			TargetTypes:          []string{"Place"},
			ExtractionGuidelines: "Include both explicit ('I live in X') and implicit ('we moved to X' = both speakers live there). Resolve 'we' to all conversation participants.",
		},
		"works_at": {
			Name:        "works_at",
			Description: "Person is employed by an Organization",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Organization"},
		},
		"is_married_to": {
			Name:                 "is_married_to",
			Description:          "Person is married to another Person",
			SourceTypes:          []string{"Person"},
			TargetTypes:          []string{"Person"},
			ExtractionGuidelines: "Create ONLY ONE direction per pair. If Sarah and Daniel are married, pick ONE canonical direction (alphabetical first name as source). Do not create both sarah→daniel and daniel→sarah.",
		},
		"is_friends_with": {
			Name:                 "is_friends_with",
			Description:          "Person has a friendship with another Person",
			SourceTypes:          []string{"Person"},
			TargetTypes:          []string{"Person"},
			ExtractionGuidelines: "Include implicit friendships ('they've been friends since college'). Create ONLY ONE direction. Resolve who is friends with whom from conversational context.",
		},
		"owns": {
			Name:        "owns",
			Description: "Person owns an Object",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Object"},
		},
		"participated_in": {
			Name:        "participated_in",
			Description: "Person participated in or attended an Event",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Event"},
		},
		"attended": {
			Name:        "attended",
			Description: "Person attended an Event",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Event"},
		},
		"visited": {
			Name:        "visited",
			Description: "Person visited a Place",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Place"},
		},
		"likes": {
			Name:        "likes",
			Description: "Person likes or frequents a Place or Object",
			SourceTypes: []string{"Person"},
		},
		"occurred_at": {
			Name:        "occurred_at",
			Description: "Event occurred at a Place",
			SourceTypes: []string{"Event"},
			TargetTypes: []string{"Place"},
		},
		"happened_on": {
			Name:        "happened_on",
			Description: "Event happened on a Date",
			SourceTypes: []string{"Event"},
			TargetTypes: []string{"Date"},
		},
	}

	// Set up Gemini model factory via AI Studio API key
	llmConfig := &config.LLMConfig{
		GoogleAPIKey:    apiKey,
		Model:           "gemini-2.5-flash",
		MaxOutputTokens: 8192,
		Temperature:     0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	modelFactory := adk.NewModelFactory(llmConfig, logger, nil, nil)

	pipeline, err := NewExtractionPipeline(ExtractionPipelineConfig{
		ModelFactory:    modelFactory,
		OrphanThreshold: 0.3,
		MaxRetries:      2,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	ctx := context.Background()
	result, err := pipeline.Run(ctx, ExtractionPipelineInput{
		DocumentText:        conversation,
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	})
	if err != nil {
		t.Fatalf("pipeline.Run: %v", err)
	}

	// --- Evaluate ---
	normalize := func(s string) string {
		return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, " ", "-")))
	}

	// Entity eval: match by normalized name
	extractedEntityNames := map[string]bool{}
	for _, e := range result.Entities {
		extractedEntityNames[normalize(e.Name)] = true
	}
	truthEntityNames := map[string]bool{}
	for _, e := range gt.Entities {
		truthEntityNames[normalize(e.Name)] = true
	}

	entityTP := 0
	for k := range extractedEntityNames {
		if truthEntityNames[k] {
			entityTP++
		}
	}
	entityFP := len(extractedEntityNames) - entityTP
	entityFN := len(truthEntityNames) - entityTP
	entityP := float64(entityTP) / float64(entityTP+entityFP)
	entityR := float64(entityTP) / float64(entityTP+entityFN)
	entityF1 := 0.0
	if entityP+entityR > 0 {
		entityF1 = 2 * entityP * entityR / (entityP + entityR)
	}

	// Build name→canonical key map from ground truth
	nameToKey := map[string]string{}
	for _, e := range gt.Entities {
		nameToKey[normalize(e.Name)] = normalize(e.Key)
	}

	// Build tempID→canonical key map for extracted entities
	tempToKey := map[string]string{}
	for _, e := range result.Entities {
		norm := normalize(e.Name)
		if key, ok := nameToKey[norm]; ok {
			tempToKey[e.TempID] = key
		} else {
			tempToKey[e.TempID] = norm
		}
	}

	// Relationship pair eval (type-agnostic)
	extractedPairs := map[[2]string]bool{}
	for _, r := range result.Relationships {
		src := tempToKey[r.SourceRef]
		tgt := tempToKey[r.TargetRef]
		if src != "" && tgt != "" {
			extractedPairs[[2]string{src, tgt}] = true
		}
	}
	truthPairs := map[[2]string]bool{}
	for _, r := range gt.Relationships {
		truthPairs[[2]string{normalize(r.Source), normalize(r.Target)}] = true
	}

	pairTP := 0
	for p := range extractedPairs {
		if truthPairs[p] {
			pairTP++
		}
	}
	pairFP := len(extractedPairs) - pairTP
	pairFN := len(truthPairs) - pairTP
	pairP := float64(pairTP) / float64(pairTP+pairFP)
	pairR := float64(pairTP) / float64(pairTP+pairFN)
	pairF1 := 0.0
	if pairP+pairR > 0 {
		pairF1 = 2 * pairP * pairR / (pairP + pairR)
	}

	// Missing and extra pairs
	var missingPairs, extraPairs [][2]string
	for p := range truthPairs {
		if !extractedPairs[p] {
			missingPairs = append(missingPairs, p)
		}
	}
	for p := range extractedPairs {
		if !truthPairs[p] {
			extraPairs = append(extraPairs, p)
		}
	}
	sort.Slice(missingPairs, func(i, j int) bool { return missingPairs[i][0] < missingPairs[j][0] })
	sort.Slice(extraPairs, func(i, j int) bool { return extraPairs[i][0] < extraPairs[j][0] })

	// Print results
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("  varR: Production Pipeline (Gemini + ResponseSchema)")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\n  Entities extracted: %d\n", len(result.Entities))
	fmt.Printf("  Entity TP=%d FP=%d FN=%d  P=%.2f R=%.2f F1=%.2f\n",
		entityTP, entityFP, entityFN, entityP, entityR, entityF1)
	fmt.Printf("\n  Relationships extracted: %d\n", len(result.Relationships))
	fmt.Printf("  Pair TP=%d FP=%d FN=%d  P=%.2f R=%.2f F1=%.2f\n",
		pairTP, pairFP, pairFN, pairP, pairR, pairF1)
	if len(missingPairs) > 0 {
		fmt.Printf("  MISSING pairs: %v\n", missingPairs)
	}
	if len(extraPairs) > 0 {
		fmt.Printf("  EXTRA pairs:   %v\n", extraPairs)
	}
	fmt.Printf("\n  SUMMARY entity_F1=%.2f  rel_pair_F1=%.2f\n", entityF1, pairF1)
	fmt.Println(strings.Repeat("=", 60))

	t.Logf("varR entity_F1=%.2f rel_pair_F1=%.2f", entityF1, pairF1)
}
