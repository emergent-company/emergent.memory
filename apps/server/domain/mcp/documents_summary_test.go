package mcp

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/documents"
)

func strptr(s string) *string { return &s }

// document-list used to return full document bodies: a 50-document page was
// measured at ~1 MB, which blew the caller's context. The projection must drop
// content by default and keep the metadata an agent needs to decide what to
// read next.
func TestDocumentSummary_OmitsContentByDefault(t *testing.T) {
	doc := &documents.Document{
		ID:       "doc-1",
		Content:  strptr("# a very large body"),
		Filename: strptr("technology/index.md"),
		Chunks:   12,
	}

	sum := documentSummary(doc, false)

	if _, ok := sum["content"]; ok {
		t.Fatalf("content must be opt-in, got %v", sum["content"])
	}
	if sum["filename"] != "technology/index.md" {
		t.Errorf("filename = %v", sum["filename"])
	}
	if sum["chunks"] != 12 {
		t.Errorf("chunks = %v", sum["chunks"])
	}
}

func TestDocumentSummary_IncludesContentWhenAsked(t *testing.T) {
	doc := &documents.Document{
		ID:      "doc-1",
		Content: strptr("# body"),
	}

	sum := documentSummary(doc, true)

	if sum["content"] != "# body" {
		t.Fatalf("content = %v, want the body", sum["content"])
	}
}
