package textsplitter

import (
	"strings"
	"testing"
)

func TestDetectShape(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Shape
	}{
		{
			name: "prose",
			text: "The quick brown fox jumps over the lazy dog. It was a bright cold day in April, " +
				"and the clocks were striking thirteen. The hallway smelt of boiled cabbage and old rag mats.",
			want: ShapeProse,
		},
		{
			name: "markdown headings",
			text: "# Introduction\n\nSome text about the thing.\n\n" +
				"## Details\n\nMore text that explains the details of the thing.\n\n" +
				"## Usage\n\nHow to use the thing.",
			want: ShapeMarkdown,
		},
		{
			name: "code heavy",
			text: "# Handler\n\nThis section shows the handler.\n\n" +
				"```go\nfunc handler(w http.ResponseWriter, r *http.Request) {\n\tw.WriteHeader(200)\n\tw.Write([]byte(\"ok\"))\n}\n" +
				"func other() {\n\treturn\n}\nfunc third() {\n\treturn\n}\n```\n\n" +
				"```go\nfunc example() error {\n\tvar buf bytes.Buffer\n\tbuf.WriteString(\"x\")\n\treturn nil\n}\n```\n\n" +
				"~~~yaml\nkey: value\nother: thing\n~~~\n",
			want: ShapeCodeHeavy,
		},
		{
			name: "tabular",
			text: "| name | type | default |\n|------|------|---------|\n" +
				"| a | string | \"\" |\n| b | int | 0 |\n| c | bool | false |\n| d | float | 0.0 |\n| e | list | [] |\n",
			want: ShapeTabular,
		},
		{
			name: "transcript with timestamps",
			text: "00:00:03 Alice: Let's start with the roadmap.\n" +
				"00:00:09 Bob: Sure. We shipped the search fix last week.\n" +
				"00:00:21 Alice: What about the chunking work?\n" +
				"00:00:30 Bob: Still in review.\n" +
				"00:01:02 Alice: Fine. Let's move on.\n",
			want: ShapeTranscript,
		},
		{
			name: "transcript with speaker turns",
			text: "Alice: Let's start with the roadmap.\nBob: Sure. We shipped the search fix last week.\n" +
				"Alice: What about the chunking work?\nBob: Still in review.\nAlice: Fine, next topic.\n" +
				"Bob: Deployment is on Thursday.\n",
			want: ShapeTranscript,
		},
		{
			name: "email thread",
			text: "From: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: Re: roadmap\n" +
				"Date: Mon, 3 Nov 2026 09:12:00 +0000\n\nHi Bob,\n\nCan we move the review?\n\nThanks,\nAlice\n",
			want: ShapeEmail,
		},
		{
			name: "empty",
			text: "",
			want: ShapeProse,
		},
		{
			name: "prose with a single heading",
			text: "# Notes\n\nThe quick brown fox jumps over the lazy dog and keeps running for a while.",
			want: ShapeProse,
		},
		{
			name: "prose with prose-colon lines below the turn threshold",
			text: "Note: this is a note.\n\nThe rest of the document is ordinary running prose that goes on for a while " +
				"and contains no further structure at all, which is what keeps the ratio below the threshold.",
			want: ShapeProse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, sig := DetectShape(tt.text)
			if got != tt.want {
				t.Fatalf("DetectShape() = %q, want %q (signals: %+v)", got, tt.want, sig)
			}
		})
	}
}

func TestDetectShapeSignals(t *testing.T) {
	text := "# Title\n\n| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n"
	_, sig := DetectShape(text)
	if sig.Headings != 1 {
		t.Fatalf("Headings = %d, want 1", sig.Headings)
	}
	if sig.TableRows != 4 {
		t.Fatalf("TableRows = %d, want 4", sig.TableRows)
	}
	if sig.TableRatio != 0.8 { // four table rows among five non-empty lines
		t.Fatalf("TableRatio = %v, want 0.8", sig.TableRatio)
	}
}

func TestDetectShapeTruncatesScan(t *testing.T) {
	// A huge prose document must still classify quickly and correctly.
	text := make([]byte, shapeScanLimit+10000)
	for i := range text {
		text[i] = 'a'
	}
	got, _ := DetectShape(string(text))
	if got != ShapeProse {
		t.Fatalf("DetectShape() = %q, want prose", got)
	}
}

func TestProfileFor(t *testing.T) {
	tests := []struct {
		shape      Shape
		strategy   Strategy
		chunkSize  int
		overlap    int
		parentNone bool
	}{
		{ShapeProse, StrategyRecursiveCharacter, 1000, 200, true},
		{ShapeMarkdown, StrategyMarkdownStructure, 1200, 0, true},
		{ShapeCodeHeavy, StrategyMarkdownStructure, 2000, 0, true},
		{ShapeTabular, StrategyRecursiveCharacter, 1200, 120, true},
		{ShapeTranscript, StrategyRecursiveCharacter, 1600, 160, true},
		{ShapeEmail, StrategyRecursiveCharacter, 1200, 120, true},
		{Shape("unknown"), StrategyRecursiveCharacter, 1000, 200, true},
	}

	for _, tt := range tests {
		p := ProfileFor(tt.shape)
		if p.Options.Strategy != tt.strategy {
			t.Errorf("ProfileFor(%q).Strategy = %q, want %q", tt.shape, p.Options.Strategy, tt.strategy)
		}
		if p.Config.ChunkSize != tt.chunkSize {
			t.Errorf("ProfileFor(%q).ChunkSize = %d, want %d", tt.shape, p.Config.ChunkSize, tt.chunkSize)
		}
		if p.Config.ChunkOverlap != tt.overlap {
			t.Errorf("ProfileFor(%q).ChunkOverlap = %d, want %d", tt.shape, p.Config.ChunkOverlap, tt.overlap)
		}
		if p.Options.ParentChild {
			t.Errorf("ProfileFor(%q) enabled parent-child by default", tt.shape)
		}
	}
}

func TestProfileForProducesValidConfig(t *testing.T) {
	// Every profile must survive the splitter's own normalisation and the
	// project-config bounds the API enforces.
	for _, p := range DefaultProfiles() {
		if p.Config.ChunkSize < 100 || p.Config.ChunkSize > 25000 {
			t.Errorf("%s: chunk size %d outside API bounds", p.Shape, p.Config.ChunkSize)
		}
		if p.Config.ChunkOverlap < 0 || p.Config.ChunkOverlap >= p.Config.ChunkSize {
			t.Errorf("%s: overlap %d invalid for size %d", p.Shape, p.Config.ChunkOverlap, p.Config.ChunkSize)
		}
		if p.Options.ParentChild && p.Options.Strategy != StrategyMarkdownStructure {
			t.Errorf("%s: parent-child with non-structure strategy", p.Shape)
		}
		if _, err := SplitDocument("hello world", p.Config, p.Options); err != nil {
			t.Errorf("%s: SplitDocument failed: %v", p.Shape, err)
		}
	}
}

func TestDefaultProfilesCoversEveryShape(t *testing.T) {
	profiles := DefaultProfiles()
	if len(profiles) != len(SupportedShapes()) {
		t.Fatalf("DefaultProfiles() has %d entries, want %d", len(profiles), len(SupportedShapes()))
	}
	for i, s := range SupportedShapes() {
		if profiles[i].Shape != s {
			t.Fatalf("profile %d is %q, want %q", i, profiles[i].Shape, s)
		}
		if !IsSupportedShape(string(profiles[i].Shape)) {
			t.Fatalf("%q is not a supported shape", profiles[i].Shape)
		}
	}
}

func TestSplitDocumentWithProfileShapes(t *testing.T) {
	// Structure-aware profiles must actually split the sample they were
	// designed for: two sections, each long enough to exhaust the budget.
	para := strings.Repeat("Alpha beta gamma delta. ", 40) // ~960 chars
	md := "# One\n\n" + para + "\n\n## Two\n\n" + para + "\n"
	p := ProfileFor(ShapeMarkdown)
	chunks, err := SplitDocument(md, p.Config, p.Options)
	if err != nil {
		t.Fatalf("SplitDocument: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for a two-section document, got %d", len(chunks))
	}
}

func TestIsSpeakerAndTimestampLines(t *testing.T) {
	speakers := []string{"Alice: hi", "Speaker 1: hi", "BOB: hi", "Dr Smith: hi"}
	for _, s := range speakers {
		if !isSpeakerLine(s) {
			t.Errorf("isSpeakerLine(%q) = false, want true", s)
		}
	}
	notSpeakers := []string{
		"this: is not a turn",
		"The following: is prose",
		"a: too short",
		"Note that the server: is down",
	}
	for _, s := range notSpeakers {
		if isSpeakerLine(s) {
			t.Errorf("isSpeakerLine(%q) = true, want false", s)
		}
	}

	times := []string{"00:12:34 Speaker", "12:34 hello", "[00:01:02] turn", "0:00:01 -"}
	for _, s := range times {
		if !isTimestampLine(s) {
			t.Errorf("isTimestampLine(%q) = false, want true", s)
		}
	}
	notTimes := []string{"12:3 is not a time", "123:45 no", "abc 12:34"}
	for _, s := range notTimes {
		if isTimestampLine(s) {
			t.Errorf("isTimestampLine(%q) = true, want false", s)
		}
	}
}
