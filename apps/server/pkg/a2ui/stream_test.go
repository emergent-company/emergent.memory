package a2ui

import (
	"strings"
	"testing"
)

// streamAll feeds every chunk and appends Flush's tail, the way a caller
// should: the concatenation is what the user is allowed to see.
func streamAll(f *TextFilter, chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(f.Feed(c))
	}
	b.WriteString(f.Flush())
	return b.String()
}

func TestTextFilterPassesPlainText(t *testing.T) {
	got := streamAll(NewTextFilter(), "Hello", ", ", "world.\n", "Second line.")
	if want := "Hello, world.\nSecond line."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTextFilterRemovesSurface(t *testing.T) {
	block := "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n" +
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"p1","component":"proposal","kind":"change","summary":"hi"}]}}` + "\n" +
		"```\n"

	// Whole block in one chunk.
	if got, want := streamAll(NewTextFilter(), "before\n", block, "after"), "before\nafter"; got != want {
		t.Fatalf("one chunk: got %q, want %q", got, want)
	}

	// Split on arbitrary boundaries, including inside the fence marker.
	chunks := []string{
		"before\n", "``", "`a2", "ui", "\n",
		`{"createSurface":{"surface`, `Id":"s1","catalogId":"basic"}}`,
		"\n```", "\nafter",
	}
	if got, want := streamAll(NewTextFilter(), chunks...), "before\nafter"; got != want {
		t.Fatalf("split: got %q, want %q", got, want)
	}
}

// A markdown code fence that is not an a2ui surface must pass through, even
// though its opening "```" is a prefix of the surface marker.
func TestTextFilterKeepsOtherCodeFences(t *testing.T) {
	chunks := []string{"see:\n", "```", "go\n", "fmt.Println()\n", "```", "\n"}
	want := "see:\n```go\nfmt.Println()\n```\n"
	if got := streamAll(NewTextFilter(), chunks...); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An unterminated surface (the model was cut off) is dropped, not shown.
func TestTextFilterDropsUnterminatedSurface(t *testing.T) {
	got := streamAll(NewTextFilter(), "before\n```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\"}}")
	if want := "before\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A line that cannot be a fence is shown as it arrives, so streaming keeps its
// token-by-token feel.
func TestTextFilterStreamsPartialLine(t *testing.T) {
	f := NewTextFilter()
	if got := f.Feed("Hel"); got != "Hel" {
		t.Fatalf("first partial = %q, want %q", got, "Hel")
	}
	if got := f.Feed("lo."); got != "lo." {
		t.Fatalf("continuation = %q, want %q", got, "lo.")
	}
	if got := f.Feed("\nnext"); got != "\nnext" {
		t.Fatalf("newline+next = %q, want %q", got, "\nnext")
	}
	if got := f.Flush(); got != "" {
		t.Fatalf("flush = %q, want empty (nothing held)", got)
	}
}

// The filter and the extractor must agree: whatever the filter showed, plus
// the extractor's stripped text, never contains the fence.
func TestTextFilterAgreesWithExtractor(t *testing.T) {
	full := "Answer first.\n\n```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n```\n\nAnd after."
	msgs, rest, found := ExtractFromText(full)
	if !found || len(msgs) != 1 {
		t.Fatalf("extractor found=%v msgs=%d", found, len(msgs))
	}
	streamed := streamAll(NewTextFilter(), "Answer first.\n\n", "```a2ui\n", `{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}`, "\n```", "\n\nAnd after.")
	if strings.Contains(streamed, "```a2ui") {
		t.Fatalf("streamed text leaked the fence: %q", streamed)
	}
	if streamed != rest {
		t.Fatalf("streamed %q != extracted %q", streamed, rest)
	}
}
