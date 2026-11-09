package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

// documentItem is the client-facing document shape: name is derived from
// memory's `filename` field so list and detail views share one consistent field.
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
	// Source provenance (see Document): populated for documents ingested from
	// a data source so clients can distinguish sourced documents and identify
	// ones marked deleted_from_source.
	SourceID         string `json:"sourceId,omitempty"`
	SourceExternalID string `json:"sourceExternalId,omitempty"`
	SourceRevisionID string `json:"sourceRevisionId,omitempty"`
	SourceStatus     string `json:"sourceStatus,omitempty"`
	SourceSyncedAt   string `json:"sourceSyncedAt,omitempty"`
	SourceDeletedAt  string `json:"sourceDeletedAt,omitempty"`
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
		SourceID:         d.SourceID,
		SourceExternalID: d.SourceExternalID,
		SourceRevisionID: d.SourceRevisionID,
		SourceStatus:     d.SourceStatus,
		SourceSyncedAt:   d.SourceSyncedAt,
		SourceDeletedAt:  d.SourceDeletedAt,
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
	page, err := s.memory.ListDocuments(c.Request().Context(), DocumentListParams{
		Limit:  100,
		Cursor: c.QueryParam("cursor"),
	})
	if err != nil {
		return documentError(c, err)
	}
	items := make([]documentItem, 0, len(page.Documents))
	for _, d := range page.Documents {
		items = append(items, documentFromParts(d))
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": items, "nextCursor": page.NextCursor})
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

// errNoGenerativeModel is the client-facing reason extraction is refused when
// the project has no resolvable generative model.
var errNoGenerativeModel = errors.New("No generative model is configured. Add a provider and set a default model in Settings \u2192 Providers before running extraction.") //nolint:staticcheck // user-facing guidance sentence; trailing punctuation is intentional

// extractionModelMissing reports whether extraction would fail for lack of a
// resolvable generative model. It mirrors chatModelWarnings' fail-open
// convention: a config fetch error is captured but treated as "not blocked", so
// a transient config outage never disables extraction.
func (s *Server) extractionModelMissing(ctx context.Context) bool {
	mc, err := s.memory.GetEffectiveModelConfig(ctx)
	if err != nil {
		captureError(err)
		return false
	}
	return mc != nil && mc.GenerativeModel == ""
}

// triggerExtraction implements POST /api/documents/:id/extract.
func (s *Server) triggerExtraction(c echo.Context) error {
	if s.extractionModelMissing(c.Request().Context()) {
		// Only disambiguate the document when blocked, keeping the happy path at
		// one memory call. Preserve the not-found contract: an unknown document
		// is 404 even when the project has no generative model.
		if _, err := s.memory.GetDocument(c.Request().Context(), c.Param("id")); err != nil {
			return documentError(c, err) // 404 for unknown, 502 otherwise
		}
		return c.JSON(http.StatusConflict, map[string]string{"error": errNoGenerativeModel.Error()})
	}
	jobID, err := s.memory.CreateExtractionJob(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"jobId": jobID})
}

// --- extraction assembly ---

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
var machineObjectKey = regexp.MustCompile(`^[a-z0-9_-]+-[0-9a-f]{12}$`)

func graphObjectLabel(o GraphObject) string {
	if o.Name != "" {
		return o.Name
	}
	// Machine keys (the old generated "<type>-<12 hex>") are never titles.
	if o.Key != "" && !machineObjectKey.MatchString(o.Key) {
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
