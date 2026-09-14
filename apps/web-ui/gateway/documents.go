package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// documentItem is the client-facing document shape (mirrors memoryItem): name
// is derived from memory's `filename` field so list and detail views share one
// consistent field.
type documentItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MimeType         string `json:"mimeType"`
	SourceType       string `json:"sourceType"`
	Chunks           int    `json:"chunks"`
	EmbeddedChunks   int    `json:"embeddedChunks"`
	ExtractionStatus string `json:"extractionStatus"`
	ObjectsCreated   int    `json:"objectsCreated"`
	CreatedAt        string `json:"createdAt"`
}

func documentFromParts(d Document) documentItem {
	return documentItem{
		ID:               d.ID,
		Name:             d.Filename,
		MimeType:         d.MimeType,
		SourceType:       d.SourceType,
		Chunks:           d.Chunks,
		EmbeddedChunks:   d.EmbeddedChunks,
		ExtractionStatus: d.ExtractionStatus,
		ObjectsCreated:   d.ObjectsCreated,
		CreatedAt:        d.CreatedAt,
	}
}

// UploadResult is the outcome of an upload: the created (or deduplicated)
// document id, plus the dedup signal so callers can distinguish a newly
// created document from an identical-content one the server collapsed.
type UploadResult struct {
	DocumentID         string
	IsDuplicate        bool
	ExistingDocumentID string
}

// documentError maps memory API errors onto gateway status codes: unknown
// document ids are 404, everything else is 502.
func documentError(c echo.Context, err error) error {
	msg := err.Error()
	if isMemoryNotFound(err) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
}

// maxUploadSize caps single document uploads at 10 MB (mirrors the memory
// service's per-file limit for text ingest).
const maxUploadSize = 10 * 1024 * 1024

// listDocuments implements GET /api/documents.
func (s *Server) listDocuments(c echo.Context) error {
	docs, cursor, err := s.memory.ListDocuments(c.Request().Context(), c.QueryParam("cursor"))
	if err != nil {
		return documentError(c, err)
	}
	items := make([]documentItem, 0, len(docs))
	for _, d := range docs {
		items = append(items, documentFromParts(d))
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": items, "nextCursor": cursor})
}

// getDocument implements GET /api/documents/:id.
func (s *Server) getDocument(c echo.Context) error {
	d, err := s.memory.GetDocument(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, documentFromParts(*d))
}

// listDocumentChunks implements GET /api/documents/:id/chunks.
func (s *Server) listDocumentChunks(c echo.Context) error {
	chunks, err := s.memory.ListChunks(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	if chunks == nil {
		chunks = []Chunk{}
	}
	return c.JSON(http.StatusOK, map[string]any{"chunks": chunks})
}

// uploadDocument implements POST /api/documents (multipart single-file upload).
func (s *Server) uploadDocument(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is required"})
	}
	if file.Size > maxUploadSize {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "file exceeds 10 MB limit"})
	}
	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read file"})
	}
	defer func() { _ = src.Close() }()

	result, err := s.memory.UploadDocument(c.Request().Context(), file.Filename, src)
	if err != nil {
		return documentError(c, err)
	}

	status := http.StatusCreated
	if result.IsDuplicate {
		status = http.StatusOK
	}
	resp := map[string]any{
		"id":          result.DocumentID,
		"isDuplicate": result.IsDuplicate,
	}
	if result.ExistingDocumentID != "" {
		resp["existingDocumentId"] = result.ExistingDocumentID
	}
	return c.JSON(status, resp)
}

// triggerExtraction implements POST /api/documents/:id/extract.
func (s *Server) triggerExtraction(c echo.Context) error {
	jobID, err := s.memory.CreateExtractionJob(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"jobId": jobID})
}

// --- extraction results ---

// extractionResults holds what a document's most recent extraction produced:
// the summary counts plus the extracted objects and relationships. Summary is
// nil when no completed extraction exists yet.
type extractionResults struct {
	Summary       *ExtractionSummary
	Objects       []GraphObject
	Relationships []GraphRelationship
}

// loadExtractionResults assembles the extraction results for a document. A
// document with no completed extraction yields a zero-value result (no error);
// individual fetch failures degrade gracefully to the data already gathered.
func (s *Server) loadExtractionResults(ctx context.Context, documentID string) extractionResults {
	var res extractionResults
	summary, err := s.memory.GetExtractionSummary(ctx, documentID)
	if err != nil || summary == nil {
		return res
	}
	res.Summary = summary
	if len(summary.ObjectIDs) == 0 {
		return res
	}

	// Extracted objects land on a staging branch (invisible to the default
	// main-branch search), so discover the branch from the first object, then
	// list by id within that branch.
	first, err := s.memory.GetGraphObject(ctx, summary.ObjectIDs[0])
	if err != nil || first == nil {
		return res
	}
	branchID := first.BranchID

	objects, err := s.memory.ListGraphObjects(ctx, branchID, "", summary.ObjectIDs)
	if err != nil {
		return res
	}
	res.Objects = objects
	res.Relationships = s.collectExtractionRelationships(ctx, branchID, objects)
	return res
}

// collectExtractionRelationships lists the relationships on the extraction
// branch and keeps those that touch at least one extracted object (memory has
// no direct extraction-job filter on relationships).
func (s *Server) collectExtractionRelationships(ctx context.Context, branchID string, objects []GraphObject) []GraphRelationship {
	rels, err := s.memory.ListGraphRelationships(ctx, branchID)
	captureError(err)
	if len(rels) == 0 || len(objects) == 0 {
		return nil
	}
	idSet := map[string]bool{}
	for _, o := range objects {
		if o.CanonicalID != "" {
			idSet[o.CanonicalID] = true
		}
		if o.ID != "" {
			idSet[o.ID] = true
		}
	}
	out := make([]GraphRelationship, 0, len(rels))
	for _, r := range rels {
		if idSet[r.SrcID] || idSet[r.DstID] {
			out = append(out, r)
		}
	}
	return out
}

// graphObjectLabel returns a display label for an object using only fields
// common to all types: its key, else the type + short id. It deliberately does
// NOT fall back to type-specific properties (name/title/summary/content), which
// can be arbitrarily long and are not present on every object type.
func graphObjectLabel(o GraphObject) string {
	if o.Key != "" {
		return o.Key
	}
	if o.Type != "" {
		return o.Type + " " + shortID(o.ID)
	}
	return shortID(o.ID)
}

// graphObjectLabelByID resolves a relationship endpoint id to its object label
// (by canonical id or version id), falling back to a shortened id.
func graphObjectLabelByID(objects []GraphObject, id string) string {
	for _, o := range objects {
		if o.CanonicalID == id || o.ID == id {
			return graphObjectLabel(o)
		}
	}
	return shortID(id)
}

// relationshipTypeLabel humanizes a relationship type for display
// (snake_case → words).
func relationshipTypeLabel(t string) string {
	return strings.ReplaceAll(t, "_", " ")
}
