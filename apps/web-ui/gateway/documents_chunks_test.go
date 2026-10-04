package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// --- chunk editor / revision history render contract ---
//
// These tests pin the DOM contract documents-chunks.js relies on. Class strings
// are intentionally not asserted (the component-conventions policy); only ids,
// data-* hooks, aria-* wiring, and rendered copy are.

func chunkRowFixture(edited bool) Chunk {
	return Chunk{
		ID:              "c1",
		DocumentID:      "d1",
		Index:           0,
		Size:            5,
		HasEmbedding:    true,
		Text:            "hello world",
		Edited:          edited,
		CurrentRevision: 3,
	}
}

func TestRenderChunkRowContract(t *testing.T) {
	html := renderHTML(t, chunkRow(chunkRowFixture(true)))

	for _, want := range []string{
		// row identity + current revision (read by the JS rollback/save code)
		`data-chunk-row`, `data-chunk-id="c1"`, `data-chunk-index="0"`,
		`data-chunk-revision="3"`, `data-testid="chunk-row-0"`,
		// view/saved text and edited badge
		`data-chunk-text`, "hello world", `data-chunk-edited-badge`,
		// inline editor
		`id="chunk-editor-0"`, `data-chunk-editor`, `hidden`,
		`id="chunk-input-0"`, `name="text"`, `data-chunk-input`,
		`data-chunk-action="edit"`, `data-chunk-action="cancel-edit"`,
		`aria-controls="chunk-editor-0"`, `aria-expanded="false"`,
		`data-testid="chunk-edit-0"`, `data-testid="chunk-input-0"`,
		// history open/close
		`id="chunk-history-0"`, `data-chunk-history`,
		`data-chunk-action="history"`, `data-chunk-action="close-history"`,
		`aria-controls="chunk-history-0"`,
		// lazily-filled history list + diff region
		`data-chunk-history-heading`, `data-chunk-revisions-status`,
		`data-chunk-revisions`, `data-chunk-diff-region`, `data-chunk-diff`,
		// per-chunk aria-live status
		`data-chunk-status`, `aria-live="polite"`, `role="status"`,
		// metadata
		"chunk 0", "embedded", "5 chars",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chunk row missing %q", want)
		}
	}
}

// TestRenderChunkRowUneditedHidesBadge pins the omit-when-false state: an
// unedited chunk still renders the badge node (so JS can reveal it after a
// save) but with the hidden attribute set.
func TestRenderChunkRowUneditedHidesBadge(t *testing.T) {
	html := renderHTML(t, chunkRow(chunkRowFixture(false)))
	if !strings.Contains(html, `data-chunk-edited-badge`) {
		t.Fatal("edited badge node missing")
	}
	if !strings.Contains(html, `data-chunk-edited-badge hidden`) {
		t.Error("unedited chunk should render the edited badge hidden")
	}
}

func TestRenderChunkRollbackDialog(t *testing.T) {
	html := renderHTML(t, chunkRollbackDialog())
	for _, want := range []string{
		`id="chunk-rollback-confirm"`,
		`data-chunk-rollback-confirm`,
		`data-testid="chunk-rollback-confirm"`,
		`data-chunk-rollback-message`,
		`data-chunk-rollback-approve`, `data-testid="chunk-rollback-confirm-button"`,
		`data-chunk-rollback-dismiss`, `data-testid="chunk-rollback-cancel"`,
		"Roll back chunk", ">Roll back<", ">Cancel<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rollback dialog missing %q", want)
		}
	}
}

// TestRenderDocumentDetailChunkEditor pins the integration: the detail page
// renders each chunk row contract and exactly one shared rollback dialog.
func TestRenderDocumentDetailChunkEditor(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md", Chunks: 2}
	chunks := []Chunk{
		{ID: "c0", Index: 0, Text: "alpha", CurrentRevision: 1},
		{ID: "c1", Index: 1, Text: "beta", Edited: true, CurrentRevision: 2},
	}
	html := renderHTML(t, DocumentDetailPage(doc, chunks, extractionResults{}, nil, "", nil))

	for _, want := range []string{
		`data-testid="chunk-row-0"`, `data-testid="chunk-row-1"`,
		`data-chunk-id="c0"`, `data-chunk-id="c1"`,
		`id="chunk-editor-0"`, `id="chunk-editor-1"`,
		`data-chunk-action="edit"`, `data-chunk-action="history"`,
		`id="chunk-rollback-confirm"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("document detail missing %q", want)
		}
	}
	if n := strings.Count(html, `data-chunk-rollback-confirm`); n != 1 {
		t.Errorf("rollback dialog rendered %d times, want 1", n)
	}
}

// TestChunkEditorScriptLoaded guards that the hydration script ships in the
// app shell. Without it the server-rendered editor does nothing.
func TestChunkEditorScriptLoaded(t *testing.T) {
	body := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<p>hi</p>"))
		return err
	})
	html := renderPageShell(t, body)
	if !strings.Contains(html, "/js/documents-chunks.js") {
		t.Error("app shell does not load documents-chunks.js")
	}
}
