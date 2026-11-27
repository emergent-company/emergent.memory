package agents

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildEntityExtractionPromptKeepsMentionSpellingGuidance pins the prompt
// rule that lets the model bind a re-mention under a variant spelling to the
// existing identity. #1123 suspected the rebase dropped this text; it is
// load-bearing for recall, so keep it asserted.
func TestBuildEntityExtractionPromptKeepsMentionSpellingGuidance(t *testing.T) {
	prompt := BuildEntityExtractionPrompt(
		"Katarzyna Nowack chaired the meeting.",
		map[string]ObjectSchema{"Person": {Name: "Person"}},
		[]string{"Person"},
		[]ExistingEntityContext{{ID: "canonical-1", Name: "Katarzyna Nowak", TypeName: "Person"}},
	)

	assert.Contains(t, prompt, "Treat spelling variants, transliterations and abbreviations as the same entity")
	assert.Contains(t, prompt, "provide its existing_entity_id")
}

// TestTrimExistingEntitiesForPromptMatchesRenderedPrompt is the #1123 index /
// prompt alignment: the trimmed set the worker indexes for identity resolution
// must be exactly the set the prompt renders. An id the model was never shown
// must not be accepted by persistence.
func TestTrimExistingEntitiesForPromptMatchesRenderedPrompt(t *testing.T) {
	typesToExtract := []string{"Person", "Organization", "Location", "Project", "Event"}
	schemas := make(map[string]ObjectSchema, len(typesToExtract))
	for _, typeName := range typesToExtract {
		schemas[typeName] = ObjectSchema{Name: typeName}
	}

	var existing []ExistingEntityContext
	for _, typeName := range typesToExtract {
		for i := 0; i < 30; i++ {
			existing = append(existing, ExistingEntityContext{
				ID:       fmt.Sprintf("%s-%02d", typeName, i),
				Name:     fmt.Sprintf("%s entity %d", typeName, i),
				TypeName: typeName,
			})
		}
	}

	shown := TrimExistingEntitiesForPrompt(existing, typesToExtract)
	require.Len(t, shown, ExistingEntityPromptMaxTotal, "total cap must bound the indexed set")

	prompt := BuildEntityExtractionPrompt("Document text.", schemas, typesToExtract, existing)

	shownIDs := make(map[string]bool, len(shown))
	for _, entity := range shown {
		shownIDs[entity.ID] = true
		assert.Contains(t, prompt, "[id: "+entity.ID+"]", "indexed entity must be rendered")
	}
	for _, entity := range existing {
		if shownIDs[entity.ID] {
			continue
		}
		assert.NotContains(t, prompt, "[id: "+entity.ID+"]", "an unrendered id must not be indexed")
	}
}

// TestTrimExistingEntitiesForPromptPerTypeAndTotal covers the caps themselves:
// per-type truncation comes first, then the overall total.
func TestTrimExistingEntitiesForPromptPerTypeAndTotal(t *testing.T) {
	var existing []ExistingEntityContext
	for _, typeName := range []string{"A", "B", "C", "D", "E", "F"} {
		for i := 0; i < 20; i++ {
			existing = append(existing, ExistingEntityContext{
				ID:       fmt.Sprintf("%s-%02d", typeName, i),
				Name:     fmt.Sprintf("%s %d", typeName, i),
				TypeName: typeName,
			})
		}
	}

	shown := TrimExistingEntitiesForPrompt(existing, []string{"A", "B", "C", "D", "E", "F"})
	require.Len(t, shown, ExistingEntityPromptMaxTotal)

	counts := make(map[string]int)
	for _, entity := range shown {
		counts[entity.TypeName]++
	}
	for typeName, n := range counts {
		assert.LessOrEqual(t, n, ExistingEntityPromptMaxPerType, "type %s exceeded its cap", typeName)
	}
	assert.Equal(t, ExistingEntityPromptMaxPerType, counts["A"], "first type fills its per-type cap")
}
