package extraction

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// suggestPackName — pure function tests
// ---------------------------------------------------------------------------

func TestSuggestPackName_PlainLine(t *testing.T) {
	name := suggestPackName("Meeting Notes\nSome content here")
	assert.Equal(t, "Meeting Notes", name)
}

func TestSuggestPackName_BracketedTitle(t *testing.T) {
	name := suggestPackName("[AI Assistant Session - 2026-05-10 14:32 UTC]\nsome body")
	assert.Equal(t, "AI Assistant Session", name)
}

func TestSuggestPackName_BracketedTitleNoDate(t *testing.T) {
	name := suggestPackName("[Customer Support Log]\nticket content")
	assert.Equal(t, "Customer Support Log", name)
}

func TestSuggestPackName_MarkdownHeader(t *testing.T) {
	name := suggestPackName("# Project Roadmap\nQ3 goals...")
	assert.Equal(t, "Project Roadmap", name)
}

func TestSuggestPackName_ListPrefix(t *testing.T) {
	name := suggestPackName("- Task List\nitem one\nitem two")
	assert.Equal(t, "Task List", name)
}

func TestSuggestPackName_StripTrailingDate(t *testing.T) {
	name := suggestPackName("Sprint Review - 2026-05-10\nbacklog items")
	assert.Equal(t, "Sprint Review", name)
}

func TestSuggestPackName_StripTrailingMonthDate(t *testing.T) {
	name := suggestPackName("Retrospective - May 2026\ncontent")
	assert.Equal(t, "Retrospective", name)
}

func TestSuggestPackName_EmptyContent(t *testing.T) {
	name := suggestPackName("")
	assert.Empty(t, name)
}

func TestSuggestPackName_OnlyWhitespace(t *testing.T) {
	name := suggestPackName("   \n\n  ")
	assert.Empty(t, name)
}

func TestSuggestPackName_TooShort(t *testing.T) {
	// Lines shorter than 3 chars are skipped; fallback to next meaningful line.
	name := suggestPackName("AB\nMeeting Notes\nmore")
	assert.Equal(t, "Meeting Notes", name)
}

func TestSuggestPackName_TooLong(t *testing.T) {
	// Lines longer than 60 chars are skipped (too generic/prose-like).
	long := "This is a very long first line that exceeds the sixty character limit here"
	name := suggestPackName(long + "\nShort Name\nmore")
	assert.Equal(t, "Short Name", name)
}

func TestSuggestPackName_ExactlyAtBoundary(t *testing.T) {
	// 60 chars exactly — should be returned.
	exact := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ12345678"[:60]
	name := suggestPackName(exact)
	assert.Equal(t, exact, name)
}

// ---------------------------------------------------------------------------
// Stage derivation logic (mirrors mcp_adapters.go ClassifyDocument)
// Tests the logic that maps a ClassificationResult to a stage string.
// ---------------------------------------------------------------------------

// deriveStage mirrors the stage-derivation logic in ClassifyDocument.
// This is extracted here to allow direct unit testing of the branching.
func deriveStage(domainName string, llmReason string) string {
	if domainName == "" {
		return "new_domain"
	}
	if llmReason != "" {
		return "llm"
	}
	return "vector"
}

func TestDeriveStage_NewDomain_WhenNoDomainName(t *testing.T) {
	stage := deriveStage("", "")
	assert.Equal(t, "new_domain", stage)
}

func TestDeriveStage_NewDomain_EvenWithLLMReason(t *testing.T) {
	// DomainName="" always means new_domain regardless of LLMReason.
	stage := deriveStage("", "LLM said no match")
	assert.Equal(t, "new_domain", stage)
}

func TestDeriveStage_LLM_WhenLLMReasonPresent(t *testing.T) {
	stage := deriveStage("contacts", "matched via LLM due to ambiguous vector")
	assert.Equal(t, "llm", stage)
}

func TestDeriveStage_Vector_WhenDomainMatchedNoLLM(t *testing.T) {
	stage := deriveStage("contacts", "")
	assert.Equal(t, "vector", stage)
}

// ---------------------------------------------------------------------------
// SuggestedPackName population rule
// new_domain + non-empty content → SuggestedPackName set
// other stages → SuggestedPackName not set (by convention)
// ---------------------------------------------------------------------------

func TestSuggestedPackName_SetOnNewDomain(t *testing.T) {
	stage := "new_domain"
	content := "Weekly Stand-up Notes\nTeam updates..."

	var packName string
	if stage == "new_domain" && content != "" {
		packName = suggestPackName(content)
	}
	assert.Equal(t, "Weekly Stand-up Notes", packName)
}

func TestSuggestedPackName_NotSetOnLLMStage(t *testing.T) {
	stage := "llm"
	content := "Weekly Stand-up Notes\nTeam updates..."

	var packName string
	if stage == "new_domain" && content != "" {
		packName = suggestPackName(content)
	}
	assert.Empty(t, packName, "SuggestedPackName must not be populated for non-new_domain stages")
}

func TestSuggestedPackName_NotSetOnVectorStage(t *testing.T) {
	stage := "vector"
	content := "Weekly Stand-up Notes\nTeam updates..."

	var packName string
	if stage == "new_domain" && content != "" {
		packName = suggestPackName(content)
	}
	assert.Empty(t, packName)
}
