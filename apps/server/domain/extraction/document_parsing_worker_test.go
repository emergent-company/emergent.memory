package extraction

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// docxTableResult mimics what Xberg returns for a DOCX with one table: Content
// carries the flattened cell text (column separators lost), while Tables holds
// the same rows as proper markdown pipe rows (issue #241).
func docxTableResult() (string, []xberg.ExtractedTable) {
	content := strings.Join([]string{
		"Session overview",
		"",
		"Date\tGuest\tTitle\tFormat\tMin",
		"2024-11-01\tAlice\tSome Title\tLive - moderation\t5",
	}, "\n")
	tables := []xberg.ExtractedTable{{
		Cells: [][]string{
			{"Date", "Guest", "Title", "Format", "Min"},
			{"2024-11-01", "Alice", "Some Title", "Live - moderation", "5"},
		},
		Markdown: "| Date | Guest | Title | Format | Min |\n" +
			"| --- | --- | --- | --- | --- |\n" +
			"| 2024-11-01 | Alice | Some Title | Live - moderation | 5 |",
	}}
	return content, tables
}

// The reported bug: the same DOCX table row appears twice, once run together
// without separators and once as a pipe row. After assembly it must appear
// exactly once, and that copy must keep its column separators.
func TestComposeXbergContentDOCXTableRowAppearsOnceWithPipes(t *testing.T) {
	content, tables := docxTableResult()

	got := composeXbergContent(content, tables)

	if n := strings.Count(got, "2024-11-01"); n != 1 {
		t.Fatalf("table row appears %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "Live - moderation"); n != 1 {
		t.Fatalf("format cell appears %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "| 2024-11-01 | Alice | Some Title | Live - moderation | 5 |") {
		t.Fatalf("pipe row with separators missing:\n%s", got)
	}
	if strings.Contains(got, "2024-11-01 Alice Some Title Live - moderation 5") {
		t.Fatalf("separator-less duplicate row still present:\n%s", got)
	}
	// The run-together label the issue reports (a title followed directly by
	// the format label) must not survive as a single line.
	if strings.Contains(got, "Some Title Live - moderation") {
		t.Fatalf("title and label still run together:\n%s", got)
	}
	if !strings.Contains(got, "Session overview") {
		t.Fatalf("non-table prose was dropped:\n%s", got)
	}
}

// A table that is genuinely absent from Content (the table-heavy PDF case)
// must still be appended, or PDFs lose their tables entirely.
func TestComposeXbergContentKeepsTableAbsentFromContent(t *testing.T) {
	content := "Quarterly report\n\nRevenue grew 12% year over year."
	tables := []xberg.ExtractedTable{{
		Markdown: "| period | revenue |\n| --- | --- |\n| Q1 | 100 |",
	}}

	got := composeXbergContent(content, tables)

	if !strings.Contains(got, "Quarterly report") || !strings.Contains(got, "Revenue grew 12%") {
		t.Fatalf("plain content was dropped:\n%s", got)
	}
	if !strings.Contains(got, "| Q1 | 100 |") {
		t.Fatalf("table absent from content was not appended:\n%s", got)
	}
	if n := strings.Count(got, "Q1"); n != 1 {
		t.Fatalf("table row appears %d times, want 1:\n%s", n, got)
	}
}

// If Content already carries the table with pipes, appending it again would
// create the duplicate the issue reports, just with separators on both copies.
func TestComposeXbergContentDoesNotDuplicateExistingPipeTable(t *testing.T) {
	table := "| a | b |\n| --- | --- |\n| 1 | 2 |"
	content := "intro\n\n" + table
	tables := []xberg.ExtractedTable{{Markdown: table}}

	got := composeXbergContent(content, tables)

	if n := strings.Count(got, "| 1 | 2 |"); n != 1 {
		t.Fatalf("existing pipe table duplicated %d times:\n%s", n, got)
	}
	if n := strings.Count(got, "intro"); n != 1 {
		t.Fatalf("intro appears %d times, want 1:\n%s", n, got)
	}
}

// Two copies of the same table in Tables must not both be appended.
func TestComposeXbergContentDeduplicatesRepeatedTableEntries(t *testing.T) {
	table := "| a | b |\n| --- | --- |\n| 1 | 2 |"
	tables := []xberg.ExtractedTable{{Markdown: table}, {Markdown: table}}

	got := composeXbergContent("intro", tables)

	if n := strings.Count(got, "| 1 | 2 |"); n != 1 {
		t.Fatalf("repeated table entry appended %d times:\n%s", n, got)
	}
}

func TestComposeXbergContentWithoutTablesReturnsContentUnchanged(t *testing.T) {
	content := "just text\n\nwith a blank line"
	if got := composeXbergContent(content, nil); got != content {
		t.Fatalf("content changed without tables:\n got=%q\nwant=%q", got, content)
	}
	if got := composeXbergContent(content, []xberg.ExtractedTable{{Markdown: "  "}}); got != content {
		t.Fatalf("content changed with empty table markdown:\n got=%q\nwant=%q", got, content)
	}
}

// Removal is conservative: prose lines that merely resemble table rows, or
// single-column rows (indistinguishable from prose), are left alone.
func TestComposeXbergContentKeepsUnrelatedProse(t *testing.T) {
	content := "The value is 5\n\n| a | b |\n| --- | --- |\n| 1 | 2 |"
	tables := []xberg.ExtractedTable{{
		Markdown: "| a | b |\n| --- | --- |\n| 1 | 2 |",
	}}

	got := composeXbergContent(content, tables)

	if !strings.Contains(got, "The value is 5") {
		t.Fatalf("unrelated prose was removed:\n%s", got)
	}
	if n := strings.Count(got, "| 1 | 2 |"); n != 1 {
		t.Fatalf("pipe table appears %d times, want 1:\n%s", n, got)
	}
}

func TestComposeXbergContentDoesNotStripInsideFencedCode(t *testing.T) {
	content := "```\n2024-11-01\tAlice\tSome Title\tLive - moderation\t5\n```"
	tables := []xberg.ExtractedTable{{
		Markdown: "| Date | Guest | Title | Format | Min |\n" +
			"| --- | --- | --- | --- | --- |\n" +
			"| 2024-11-01 | Alice | Some Title | Live - moderation | 5 |",
	}}

	got := composeXbergContent(content, tables)

	if !strings.Contains(got, "2024-11-01\tAlice") {
		t.Fatalf("fenced code line was removed:\n%s", got)
	}
}
