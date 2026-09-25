package textsplitter_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
)

func TestConfigPresets(t *testing.T) {
	d := textsplitter.DefaultConfig()
	if d.ChunkSize != 1000 || d.ChunkOverlap != 200 {
		t.Fatalf("DefaultConfig() = %+v, want ChunkSize=1000 ChunkOverlap=200", d)
	}

	e := textsplitter.ExtractionConfig()
	if e.ChunkSize != 16000 || e.ChunkOverlap != 3200 {
		t.Fatalf("ExtractionConfig() = %+v, want ChunkSize=16000 ChunkOverlap=3200", e)
	}
}

func TestSplitEmpty(t *testing.T) {
	if got := textsplitter.Split("", textsplitter.DefaultConfig()); got != nil {
		t.Fatalf("Split(\"\") = %#v, want nil", got)
	}
}

func TestSplitShortTextIsSingleChunk(t *testing.T) {
	got := textsplitter.Split("hello world", textsplitter.DefaultConfig())
	if len(got) != 1 {
		t.Fatalf("Split(short) returned %d chunks, want 1: %#v", len(got), got)
	}
	if got[0] != "hello world" {
		t.Fatalf("Split(short)[0] = %q, want %q", got[0], "hello world")
	}
}

func TestSplitLongTextProducesMultipleChunks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		cfg  textsplitter.Config
	}{
		{"newline separated", strings.Repeat("line one two three four\n", 100), textsplitter.Config{ChunkSize: 100, ChunkOverlap: 20}},
		{"space separated", strings.Repeat("the quick brown fox ", 100), textsplitter.Config{ChunkSize: 200, ChunkOverlap: 40}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := textsplitter.Split(tc.in, tc.cfg)
			if len(got) < 2 {
				t.Fatalf("Split produced %d chunks, want >= 2", len(got))
			}
			for i, c := range got {
				if strings.TrimSpace(c) == "" {
					t.Fatalf("chunk %d is empty/whitespace", i)
				}
			}
		})
	}
}

func TestSplitNormalizesInvalidConfig(t *testing.T) {
	in := strings.Repeat("word ", 500) // 2500 chars
	cfg := textsplitter.Config{ChunkSize: -10, ChunkOverlap: 999999}
	got := textsplitter.Split(in, cfg)
	if len(got) == 0 {
		t.Fatal("Split with invalid config produced no chunks")
	}
	for _, c := range got {
		if strings.TrimSpace(c) == "" {
			t.Fatal("Split with invalid config produced an empty chunk")
		}
	}
}
