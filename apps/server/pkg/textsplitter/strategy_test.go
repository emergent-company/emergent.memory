package textsplitter

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestSplitDocumentDefaultMatchesSplit(t *testing.T) {
	text := strings.Repeat("alpha beta gamma. ", 200)
	cfg := Config{ChunkSize: 200, ChunkOverlap: 40}

	want := Split(text, cfg)
	got, err := SplitDocument(text, cfg, Options{})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("chunk count mismatch: got %d want %d", len(got), len(want))
	}
	var gotTexts []string
	for _, c := range got {
		gotTexts = append(gotTexts, c.Text)
	}
	if !reflect.DeepEqual(gotTexts, want) {
		t.Fatalf("default strategy output differs from Split\n got=%q\nwant=%q", gotTexts, want)
	}
}

func TestSplitDocumentUnknownStrategy(t *testing.T) {
	_, err := SplitDocument("hello world", Config{ChunkSize: 100}, Options{Strategy: "does_not_exist"})
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestSplitDocumentSupportedStrategies(t *testing.T) {
	got := SupportedStrategies()
	want := []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedStrategies()=%v want %v", got, want)
	}
	if !IsSupportedStrategy("markdown_structure") || IsSupportedStrategy("nope") {
		t.Fatal("IsSupportedStrategy returned wrong result")
	}
}

func TestMarkdownStructureRespectsHeadings(t *testing.T) {
	text := "# Alpha\n\n" + strings.Repeat("alpha body ", 60) + "\n\n# Beta\n\n" + strings.Repeat("beta body ", 60)
	chunks, err := SplitDocument(text, Config{ChunkSize: 300}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if strings.Contains(c.Text, "# Alpha") && strings.Contains(c.Text, "# Beta") {
			t.Fatalf("two sections were merged into one chunk: %q", c.Text)
		}
	}
}

func TestMarkdownStructureCodeBlockStaysIntact(t *testing.T) {
	code := "```go\n" + strings.Repeat("x := 1\n", 100) + "```"
	text := "intro paragraph\n\n" + code
	chunks, err := SplitDocument(text, Config{ChunkSize: 300}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "x := 1") {
			found = true
			if !c.Oversized {
				t.Fatalf("code chunk should be flagged oversized: %+v", c)
			}
			if strings.Count(c.Text, "```") != 2 {
				t.Fatalf("code fence was split: %q", c.Text)
			}
		}
	}
	if !found {
		t.Fatal("code block not found in output")
	}
}

func TestMarkdownStructureTableThatFitsStaysIntact(t *testing.T) {
	table := "| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |"
	text := "some intro text\n\n" + table
	chunks, err := SplitDocument(text, Config{ChunkSize: 200}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	var withTable []string
	for _, c := range chunks {
		if strings.Contains(c.Text, "| a | b |") {
			withTable = append(withTable, c.Text)
		}
	}
	if len(withTable) != 1 {
		t.Fatalf("table should appear whole in exactly one chunk, got %d: %v", len(withTable), withTable)
	}
}

func TestMarkdownStructureOversizedTableSplitsWithHeader(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("| name | value |\n| --- | --- |\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("| row-padding-value-here | another-long-value-here |\n")
	}
	text := sb.String()

	chunks, err := SplitDocument(text, Config{ChunkSize: 300}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected oversized table to split, got %d chunk(s)", len(chunks))
	}
	for i, c := range chunks {
		if !strings.HasPrefix(c.Text, "| name | value |") {
			t.Fatalf("chunk %d missing carried header: %q", i, c.Text)
		}
	}
}

func TestMarkdownStructureParentChild(t *testing.T) {
	text := "# Alpha\n\nfirst body that is reasonably long enough\n\n# Beta\n\nsecond body that is reasonably long enough"

	off, err := SplitDocument(text, Config{ChunkSize: 1000}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	for _, c := range off {
		if c.IsParent || c.ParentIndex != -1 {
			t.Fatalf("parent-child should be off by default: %+v", c)
		}
	}

	on, err := SplitDocument(text, Config{ChunkSize: 1000}, Options{Strategy: StrategyMarkdownStructure, ParentChild: true})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	var parents, children int
	for _, c := range on {
		if c.IsParent {
			parents++
		} else {
			children++
			if c.ParentIndex < 0 || c.ParentIndex >= len(on) {
				t.Fatalf("child has invalid parent index %d", c.ParentIndex)
			}
		}
	}
	if parents != 2 || children == 0 {
		t.Fatalf("expected 2 parents and some children, got parents=%d children=%d", parents, children)
	}
}

func TestMarkdownStructureUnknownHeaderRecorded(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("| no-separator-here-padding-value | another-long-value-here |\n")
	}
	chunks, err := SplitDocument(sb.String(), Config{ChunkSize: 300}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected oversized table to split, got %d chunk(s)", len(chunks))
	}
	for i, c := range chunks {
		if c.HeaderCarry != "unknown" {
			t.Fatalf("chunk %d should record unknown header carry, got %q", i, c.HeaderCarry)
		}
	}
}

func TestParentChildRequiresMarkdownStrategy(t *testing.T) {
	_, err := SplitDocument("some text", Config{ChunkSize: 1000}, Options{Strategy: StrategyRecursiveCharacter, ParentChild: true})
	if err == nil {
		t.Fatal("expected error for parent-child with recursive strategy")
	}
}

func TestMarkdownStructureParentTextTruncated(t *testing.T) {
	body := strings.Repeat("x", maxParentTextChars*2)
	text := "# Big Section\n\n" + body

	chunks, err := SplitDocument(text, Config{ChunkSize: 1000}, Options{Strategy: StrategyMarkdownStructure, ParentChild: true})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}

	var parent *Chunk
	for i := range chunks {
		if chunks[i].IsParent {
			parent = &chunks[i]
			break
		}
	}
	if parent == nil {
		t.Fatal("expected a parent chunk")
	}
	if parent.Text == "" {
		t.Fatal("parent text unexpectedly empty")
	}
	if len(parent.Text) > maxParentTextChars {
		t.Fatalf("parent text length %d exceeds cap %d", len(parent.Text), maxParentTextChars)
	}
}

func nonWhitespaceRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func TestSplitDocumentEmptyInput(t *testing.T) {
	for _, strat := range []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure} {
		t.Run(string(strat), func(t *testing.T) {
			chunks, err := SplitDocument("", Config{ChunkSize: 100, ChunkOverlap: 20}, Options{Strategy: strat})
			if err != nil {
				t.Fatalf("SplitDocument: %v", err)
			}
			if len(chunks) != 0 {
				t.Fatalf("expected no chunks, got %d: %v", len(chunks), chunks)
			}
		})
	}
}

func TestSplitDocumentWhitespaceOnlyInput(t *testing.T) {
	for _, strat := range []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure} {
		t.Run(string(strat), func(t *testing.T) {
			chunks, err := SplitDocument("   \n\t ", Config{ChunkSize: 100, ChunkOverlap: 20}, Options{Strategy: strat})
			if err != nil {
				t.Fatalf("SplitDocument: %v", err)
			}
			if len(chunks) != 0 {
				t.Fatalf("expected no chunks, got %d: %v", len(chunks), chunks)
			}
		})
	}
}

func TestSplitDocumentUnicodeEmojiValidUTF8(t *testing.T) {
	text := strings.Repeat("😀 café naïve 漢字 ", 40)
	wantNonWS := nonWhitespaceRunes(text)
	cfg := Config{ChunkSize: 20, ChunkOverlap: 0}

	for _, strat := range []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure} {
		t.Run(string(strat), func(t *testing.T) {
			chunks, err := SplitDocument(text, cfg, Options{Strategy: strat})
			if err != nil {
				t.Fatalf("SplitDocument: %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("expected chunks")
			}
			total := 0
			for i, c := range chunks {
				if !utf8.ValidString(c.Text) {
					t.Fatalf("chunk %d has invalid UTF-8", i)
				}
				if len(c.Text) == 0 {
					t.Fatalf("chunk %d is empty", i)
				}
				total += nonWhitespaceRunes(c.Text)
			}
			if total < wantNonWS {
				t.Fatalf("non-whitespace content lost: got %d runes, want >= %d", total, wantNonWS)
			}
		})
	}
}

func TestSplitDocumentOverlapExceedsSize(t *testing.T) {
	text := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 20)
	for _, cfg := range []Config{
		{ChunkSize: 50, ChunkOverlap: 50},
		{ChunkSize: 50, ChunkOverlap: 200},
	} {
		for _, strat := range []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure} {
			chunks, err := SplitDocument(text, cfg, Options{Strategy: strat})
			if err != nil {
				t.Fatalf("cfg=%+v strat=%s: %v", cfg, strat, err)
			}
			if len(chunks) == 0 {
				t.Fatalf("cfg=%+v strat=%s: no chunks", cfg, strat)
			}
			if len(chunks) >= len(text) {
				t.Fatalf("cfg=%+v strat=%s: unbounded chunk count %d", cfg, strat, len(chunks))
			}
			for i, c := range chunks {
				if c.Text == "" {
					t.Fatalf("cfg=%+v strat=%s: chunk %d empty", cfg, strat, i)
				}
			}
		}
	}
}

func TestSplitDocumentOversizedSingleParagraph(t *testing.T) {
	text := strings.Repeat("word ", 10000) // 50000 chars, no blank lines
	cfg := Config{ChunkSize: 200, ChunkOverlap: 40}
	chunks, err := SplitDocument(text, cfg, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected paragraph to split into multiple chunks, got %d", len(chunks))
	}
	wantNonWS := nonWhitespaceRunes(text)
	total := 0
	for i, c := range chunks {
		if c.Text == "" {
			t.Fatalf("chunk %d empty", i)
		}
		total += nonWhitespaceRunes(c.Text)
	}
	if total < wantNonWS {
		t.Fatalf("content dropped: got %d non-whitespace runes, want >= %d", total, wantNonWS)
	}
}

func TestSplitDocumentOversizedSingleTableRow(t *testing.T) {
	bigCell := strings.Repeat("x", 500)
	text := "| a | b |\n| --- | --- |\n| " + bigCell + " | tail |"
	chunks, err := SplitDocument(text, Config{ChunkSize: 200}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, bigCell) {
			found = true
			if c.HeaderCarry != "" {
				t.Fatalf("expected empty HeaderCarry for header-known table, got %q", c.HeaderCarry)
			}
		}
	}
	if !found {
		t.Fatal("oversized row was truncated or dropped")
	}
}

func TestSplitDocumentDeeplyNestedParentChild(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 6; i++ {
		sb.WriteString(strings.Repeat("#", i))
		sb.WriteString(" Heading Level ")
		sb.WriteByte(byte('0' + i))
		sb.WriteString("\n\n")
		sb.WriteString("> nested blockquote text with some words\n")
		sb.WriteString("- list item alpha\n")
		sb.WriteString("- list item beta\n\n")
	}
	text := sb.String()
	chunks, err := SplitDocument(text, Config{ChunkSize: 60}, Options{Strategy: StrategyMarkdownStructure, ParentChild: true})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	parents := 0
	for i, c := range chunks {
		if c.IsParent {
			parents++
			if c.ParentIndex != -1 {
				t.Fatalf("parent chunk %d has ParentIndex %d, want -1", i, c.ParentIndex)
			}
			continue
		}
		if c.ParentIndex < 0 || c.ParentIndex >= len(chunks) {
			t.Fatalf("child chunk %d has invalid ParentIndex %d", i, c.ParentIndex)
		}
		if !chunks[c.ParentIndex].IsParent {
			t.Fatalf("child chunk %d ParentIndex %d does not reference a parent", i, c.ParentIndex)
		}
	}
	if parents == 0 {
		t.Fatal("expected at least one parent chunk")
	}
}

func TestSplitDocumentCodeFenceHeadingLinesIntact(t *testing.T) {
	text := "# Real Heading\n\nBefore\n\n```python\n# not a heading\ndef f():\n    # also not a heading\n    return 1\n```\n\nAfter"
	chunks, err := SplitDocument(text, Config{ChunkSize: 60}, Options{Strategy: StrategyMarkdownStructure})
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	var codeChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Text, "```python") {
			codeChunk = &chunks[i]
			break
		}
	}
	if codeChunk == nil {
		t.Fatal("code fence chunk not found")
	}
	if strings.Count(codeChunk.Text, "```") != 2 {
		t.Fatalf("code fence was split: %q", codeChunk.Text)
	}
	if !strings.Contains(codeChunk.Text, "# not a heading") {
		t.Fatalf("fenced # line dropped: %q", codeChunk.Text)
	}
	if !codeChunk.Oversized {
		t.Fatalf("code block larger than chunk size should be flagged oversized: %+v", codeChunk)
	}
	for _, c := range chunks {
		if strings.TrimSpace(c.Text) == "# not a heading" {
			t.Fatalf("fenced # line leaked as a standalone heading: %+v", c)
		}
	}
}

func TestSplitRecursivePathologicalNoPanic(t *testing.T) {
	inputs := []string{
		"",
		"   \n\t ",
		strings.Repeat("😀 café naïve 漢字 ", 40),
		strings.Repeat("word ", 10000),
		strings.Repeat("a", 50000),
	}
	cfgs := []Config{
		{ChunkSize: 50, ChunkOverlap: 50},
		{ChunkSize: 200, ChunkOverlap: 200},
		{ChunkSize: 1, ChunkOverlap: 0},
		{ChunkSize: 5, ChunkOverlap: 4},
	}
	for _, cfg := range cfgs {
		for _, text := range inputs {
			parts := Split(text, cfg)
			for i, p := range parts {
				if p == "" {
					t.Fatalf("cfg=%+v: part %d empty", cfg, i)
				}
				if !utf8.ValidString(p) {
					t.Fatalf("cfg=%+v: part %d invalid UTF-8", cfg, i)
				}
			}
		}
	}
}
