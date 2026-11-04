package toolresult

import (
	"strconv"
	"strings"
	"testing"
)

// TestParseRecordCaps: a long list says how many it did not show.
func TestParseRecordCaps(t *testing.T) {
	var hits []string
	for i := range 7 {
		hits = append(hits, `{"object": {"id": "e`+string(rune('0'+i))+`", "name": "Result `+string(rune('0'+i))+`"}}`)
	}
	raw := `{"ok": true, "data": {"data": [` + strings.Join(hits, ",") + `], "total": 7}}`

	got := Parse(raw)
	if len(got.Items) != ItemLimit {
		t.Errorf("items = %d, want %d", len(got.Items), ItemLimit)
	}
	if got.MoreRecords != 2 {
		t.Errorf("moreRecords = %d, want 2", got.MoreRecords)
	}
	if got.Outcome != "7 records" {
		t.Errorf("outcome = %q, want %q", got.Outcome, "7 records")
	}
}

// TestParseFieldCaps: a record with many fields shows the first eight and counts
// the rest.
func TestParseFieldCaps(t *testing.T) {
	var pairs []string
	for i := range 12 {
		pairs = append(pairs, `"field`+string(rune('a'+i))+`": `+strconv.Itoa(i))
	}
	got := Parse(`{` + strings.Join(pairs, ",") + `}`)

	if len(got.Fields) != FieldLimit {
		t.Errorf("fields = %d, want %d", len(got.Fields), FieldLimit)
	}
	if got.MoreFields != 4 {
		t.Errorf("moreFields = %d, want 4", got.MoreFields)
	}
	if got.Outcome != "12 fields" {
		t.Errorf("outcome = %q, want %q", got.Outcome, "12 fields")
	}
}

// TestParseTextCaps: body text is bounded by lines and by line width, and both
// remainders are counted.
func TestParseTextCaps(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		body := strings.TrimSuffix(strings.Repeat("a line of the document\n", 12), "\n")
		got := Parse(`{"content": "` + strings.ReplaceAll(body, "\n", `\n`) + `"}`)
		if got.Kind != KindText {
			t.Fatalf("kind = %q, want text", got.Kind)
		}
		if lines := strings.Count(got.Text, "\n") + 1; lines != TextLineLimit {
			t.Errorf("text lines = %d, want %d", lines, TextLineLimit)
		}
		if got.MoreLines != 12-TextLineLimit {
			t.Errorf("moreLines = %d, want %d", got.MoreLines, 12-TextLineLimit)
		}
	})

	t.Run("endless line", func(t *testing.T) {
		// A document body converted to plain text can arrive as one huge line; it
		// must not form an endless row, and the collapsed outcome stays one line.
		content := strings.Repeat("x", 50000)
		got := Parse(`{"id": "d1", "filename": "dump.txt", "content": "` + content + `", "ok": true}`)
		if got.Kind != KindText {
			t.Fatalf("kind = %q, want text", got.Kind)
		}
		if n := len([]rune(got.Text)); n != LineWidth+1 {
			t.Errorf("text runes = %d, want %d", n, LineWidth+1)
		}
		if !strings.HasSuffix(got.Text, "…") {
			t.Errorf("text does not mark its cut")
		}
		if got.MoreCharacters != 50000-LineWidth {
			t.Errorf("moreCharacters = %d, want %d", got.MoreCharacters, 50000-LineWidth)
		}
		if n := len([]rune(got.Outcome)); n != outcomeTextWidth+1 {
			t.Errorf("outcome runes = %d, want %d", n, outcomeTextWidth+1)
		}
	})
}

// TestParseRawCaps: the raw disclosure is bounded and says what it left out.
func TestParseRawCaps(t *testing.T) {
	text := strings.Repeat("a line\n", 60)
	got := Parse(text)
	if got.Kind != "" {
		t.Errorf("kind = %q, want no body for non-JSON", got.Kind)
	}
	if lines := strings.Count(got.Raw, "\n") + 1; lines != RawLineLimit {
		t.Errorf("raw lines = %d, want %d", lines, RawLineLimit)
	}
	if got.RawMoreLines != 60-RawLineLimit {
		t.Errorf("rawMoreLines = %d, want %d", got.RawMoreLines, 60-RawLineLimit)
	}
}

// TestParseOversized: above the parse limit a payload is not parsed at all — a
// result that large is a wall no presentation fixes — but it still says it was
// cut rather than pretending to be complete.
func TestParseOversized(t *testing.T) {
	raw := `{"content": "` + strings.Repeat("x", ParseLimit) + `"}`

	got := Parse(raw)
	if got.Kind != "" {
		t.Errorf("kind = %q, want no body above the parse limit", got.Kind)
	}
	if got.RawMoreCharacters == 0 {
		t.Errorf("oversized raw does not report its cut: %+v", got)
	}
	if got.RawMoreLines != 0 {
		t.Errorf("rawMoreLines = %d, want 0 for a one-line payload", got.RawMoreLines)
	}
	if len([]rune(got.Raw)) > RawLineLimit*(LineWidth+1)+RawLineLimit {
		t.Errorf("raw is not bounded: %d runes", len([]rune(got.Raw)))
	}
}

// TestParseMultiByteLimit: the limit counts characters, so text just over the
// limit in bytes but under it in characters is still parsed.
func TestParseMultiByteLimit(t *testing.T) {
	// Three-byte runes: 70 000 of them is 210 000 bytes but 70 000 characters.
	got := Parse(`{"name": "` + strings.Repeat("日", 70000) + `"}`)
	if got.Kind != KindFields {
		t.Errorf("kind = %q, want a field list (payload is under the character limit)", got.Kind)
	}
}

// BenchmarkParseOversized keeps the refusal path cheap: it runs on a hot page.
func BenchmarkParseOversized(b *testing.B) {
	raw := `{"content": "` + strings.Repeat("x", ParseLimit) + `"}`
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		Parse(raw)
	}
}

// BenchmarkParseEnvelopeSearch is the common case: a list of records under the
// caps.
func BenchmarkParseEnvelopeSearch(b *testing.B) {
	raw := `{"ok": true, "data": {"data": [{"object": {"id": "e1", "name": "Alice", "properties": {"description": "Engineer"}}, "score": 0.91}], "total": 1}}`
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		Parse(raw)
	}
}
