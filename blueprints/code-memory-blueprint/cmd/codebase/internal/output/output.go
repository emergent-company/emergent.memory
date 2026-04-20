// Package output provides shared rendering helpers for table, JSON, and markdown output.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// Format constants.
const (
	FormatTable    = "table"
	FormatJSON     = "json"
	FormatMarkdown = "markdown"
	FormatTree     = "tree"
	FormatCSV      = "csv"
)

// Table renders a table to stdout with the given headers and rows.
func Table(headers []string, rows [][]string) {
	TableTo(os.Stdout, headers, rows)
}

// TableTo renders a table to the given writer.
func TableTo(w io.Writer, headers []string, rows [][]string) {
	t := tablewriter.NewWriter(w)
	h := make([]any, len(headers))
	for i, v := range headers {
		h[i] = v
	}
	t.Header(h...)
	t.Configure(func(cfg *tablewriter.Config) {
		cfg.Behavior.TrimSpace = tw.On
	})
	for _, row := range rows {
		t.Append(row)
	}
	t.Render()
}

// JSON prints v as indented JSON to stdout.
func JSON(v any) error {
	return JSONTo(os.Stdout, v)
}

// JSONTo prints v as indented JSON to the given writer.
func JSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Markdown renders a markdown table to stdout.
func Markdown(headers []string, rows [][]string) {
	MarkdownTo(os.Stdout, headers, rows)
}

// MarkdownTo renders a markdown table to the given writer.
func MarkdownTo(w io.Writer, headers []string, rows [][]string) {
	fmt.Fprintf(w, "| %s |\n", strings.Join(headers, " | "))
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	fmt.Fprintf(w, "| %s |\n", strings.Join(seps, " | "))
	for _, row := range rows {
		fmt.Fprintf(w, "| %s |\n", strings.Join(row, " | "))
	}
}

// Header prints a section header.
func Header(title string) {
	fmt.Printf("┌─ %s\n", title)
}

// OK prints a green checkmark line.
func OK(format string, args ...any) {
	color.Green("  ✓ "+format, args...)
}

// Warn prints a yellow warning line.
func Warn(format string, args ...any) {
	color.Yellow("  ⚠ "+format, args...)
}

// Fail prints a red failure line.
func Fail(format string, args ...any) {
	color.Red("  ✗ "+format, args...)
}

// Progress prints a progress line to stderr.
func Progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "→ "+format+"\n", args...)
}

// Progressf prints a sub-progress line to stderr.
func Progressf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "  "+format+"\n", args...)
}

// DryRunTag returns " [DRY RUN]" if dryRun is true.
func DryRunTag(dryRun bool) string {
	if dryRun {
		return " [DRY RUN]"
	}
	return ""
}

// Render dispatches to the correct renderer based on format.
func Render(format string, headers []string, rows [][]string, jsonVal any) error {
	switch format {
	case FormatJSON:
		return JSON(jsonVal)
	case FormatMarkdown:
		Markdown(headers, rows)
		return nil
	default:
		Table(headers, rows)
		return nil
	}
}

// Truncate truncates s to maxLen, appending "…" if needed.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// ShortID returns the first 8 chars of a UUID.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8] + "…"
	}
	return id
}
