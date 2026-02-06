package clickup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/domain/documents"
	"github.com/emergent/emergent-core/pkg/logger"
)

const (
	ProviderTypeClickUp = "clickup"
	SourceTypeClickUp   = "clickup-document"
)

// ProviderConfig contains the decrypted configuration for a provider
// Mirrors datasource.ProviderConfig to avoid import cycle
type ProviderConfig struct {
	IntegrationID string
	ProjectID     string
	Config        map[string]interface{}
	Metadata      map[string]interface{}
}

// SyncOptions contains options for a sync operation
// Mirrors datasource.SyncOptions to avoid import cycle
type SyncOptions struct {
	Limit           int
	FullSync        bool
	ConfigurationID string
	Custom          map[string]interface{}
}

// SyncResult contains the results of a sync operation
// Mirrors datasource.SyncResult to avoid import cycle
type SyncResult struct {
	TotalItems      int
	ProcessedItems  int
	SuccessfulItems int
	FailedItems     int
	SkippedItems    int
	DocumentIDs     []string
	Errors          []string
}

// Progress represents the current progress of a sync operation
// Mirrors datasource.Progress to avoid import cycle
type Progress struct {
	Phase           string
	TotalItems      int
	ProcessedItems  int
	SuccessfulItems int
	FailedItems     int
	SkippedItems    int
	Message         string
}

// ProgressCallback is called by providers to report sync progress
type ProgressCallback func(progress Progress)

// Provider implements the ClickUp data source provider for ClickUp Docs.
type Provider struct {
	client  *Client
	db      bun.IDB
	docRepo *documents.Repository
	log     *slog.Logger
}

// NewProvider creates a new ClickUp provider
func NewProvider(db bun.IDB, log *slog.Logger) *Provider {
	return &Provider{
		client:  NewClient(log),
		db:      db,
		docRepo: documents.NewRepository(db, log),
		log:     log.With(logger.Scope("clickup-provider")),
	}
}

// ProviderType returns the provider type identifier
func (p *Provider) ProviderType() string {
	return ProviderTypeClickUp
}

// TestConnection tests if the ClickUp API credentials are valid
func (p *Provider) TestConnection(ctx context.Context, config ProviderConfig) error {
	clickupConfig, err := p.parseConfig(config.Config)
	if err != nil {
		return err
	}

	// Try to get workspaces to verify credentials
	resp, err := p.client.GetWorkspaces(ctx, clickupConfig.APIToken)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	if len(resp.Teams) == 0 {
		return fmt.Errorf("no workspaces found - check API token permissions")
	}

	return nil
}

// Sync performs a full sync operation from ClickUp
func (p *Provider) Sync(ctx context.Context, config ProviderConfig, options SyncOptions, progressCB ProgressCallback) (*SyncResult, error) {
	clickupConfig, err := p.parseConfig(config.Config)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{
		DocumentIDs: []string{},
		Errors:      []string{},
	}

	// Validate workspace ID
	if clickupConfig.WorkspaceID == "" {
		result.Errors = append(result.Errors, "Workspace ID not configured. Please test connection first.")
		return result, fmt.Errorf("workspace ID not configured")
	}

	// Report starting phase
	if progressCB != nil {
		progressCB(Progress{
			Phase:   "discovering",
			Message: "Discovering ClickUp docs...",
		})
	}

	// Get spaces to sync
	spaceIDs, err := p.getSpaceIDs(ctx, clickupConfig)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	p.log.Info("discovered spaces to sync",
		slog.Int("space_count", len(spaceIDs)),
		slog.String("workspace_id", clickupConfig.WorkspaceID))

	// Collect all docs from spaces
	var allDocs []Doc
	for _, spaceID := range spaceIDs {
		docs, err := p.getDocsFromSpace(ctx, clickupConfig, spaceID)
		if err != nil {
			p.log.Warn("failed to get docs from space",
				logger.Error(err),
				slog.String("space_id", spaceID))
			continue
		}
		allDocs = append(allDocs, docs...)
	}

	result.TotalItems = len(allDocs)

	// Filter by lastSyncedAt for incremental sync
	if !options.FullSync && clickupConfig.LastSyncedAt > 0 {
		allDocs = p.filterByUpdatedSince(allDocs, clickupConfig.LastSyncedAt)
		p.log.Info("filtered to recently updated docs",
			slog.Int("filtered_count", len(allDocs)),
			slog.Int64("since", clickupConfig.LastSyncedAt))
	}

	// Apply limit if specified
	if options.Limit > 0 && len(allDocs) > options.Limit {
		allDocs = allDocs[:options.Limit]
	}

	// Report importing phase
	if progressCB != nil {
		progressCB(Progress{
			Phase:      "importing",
			TotalItems: len(allDocs),
			Message:    fmt.Sprintf("Importing %d docs...", len(allDocs)),
		})
	}

	// Import each doc
	for i, doc := range allDocs {
		select {
		case <-ctx.Done():
			result.Errors = append(result.Errors, "sync cancelled")
			return result, ctx.Err()
		default:
		}

		docID, skipped, err := p.importDoc(ctx, clickupConfig, doc, config.ProjectID, config.IntegrationID)
		result.ProcessedItems++

		if err != nil {
			result.FailedItems++
			result.Errors = append(result.Errors, fmt.Sprintf("doc %s: %s", doc.ID, err.Error()))
			p.log.Warn("failed to import doc",
				logger.Error(err),
				slog.String("doc_id", doc.ID),
				slog.String("doc_name", doc.Name))
		} else if skipped {
			result.SkippedItems++
		} else {
			result.SuccessfulItems++
			result.DocumentIDs = append(result.DocumentIDs, docID)
		}

		// Report progress
		if progressCB != nil && i%10 == 0 {
			progressCB(Progress{
				Phase:           "importing",
				TotalItems:      len(allDocs),
				ProcessedItems:  result.ProcessedItems,
				SuccessfulItems: result.SuccessfulItems,
				FailedItems:     result.FailedItems,
				SkippedItems:    result.SkippedItems,
				Message:         fmt.Sprintf("Importing %d/%d docs...", result.ProcessedItems, len(allDocs)),
			})
		}
	}

	// Report completion
	if progressCB != nil {
		progressCB(Progress{
			Phase:           "completed",
			TotalItems:      len(allDocs),
			ProcessedItems:  result.ProcessedItems,
			SuccessfulItems: result.SuccessfulItems,
			FailedItems:     result.FailedItems,
			SkippedItems:    result.SkippedItems,
			Message:         "Sync completed",
		})
	}

	p.log.Info("clickup sync completed",
		slog.Int("total", result.TotalItems),
		slog.Int("imported", result.SuccessfulItems),
		slog.Int("skipped", result.SkippedItems),
		slog.Int("failed", result.FailedItems))

	return result, nil
}

// ----------------------------------------------------------------------------
// Helper Methods
// ----------------------------------------------------------------------------

// parseConfig parses and validates the provider configuration
func (p *Provider) parseConfig(config map[string]interface{}) (*Config, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	var clickupConfig Config
	if err := json.Unmarshal(data, &clickupConfig); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if clickupConfig.APIToken == "" {
		return nil, fmt.Errorf("API token is required")
	}

	return &clickupConfig, nil
}

// getSpaceIDs returns the space IDs to sync
func (p *Provider) getSpaceIDs(ctx context.Context, config *Config) ([]string, error) {
	// If specific spaces are selected, use those
	if len(config.SelectedSpaces) > 0 {
		ids := make([]string, len(config.SelectedSpaces))
		for i, s := range config.SelectedSpaces {
			ids[i] = s.ID
		}
		return ids, nil
	}

	// Otherwise, get all spaces from the workspace
	resp, err := p.client.GetSpaces(ctx, config.APIToken, config.WorkspaceID, config.IncludeArchived)
	if err != nil {
		return nil, fmt.Errorf("get spaces: %w", err)
	}

	ids := make([]string, len(resp.Spaces))
	for i, s := range resp.Spaces {
		ids[i] = s.ID
	}

	return ids, nil
}

// getDocsFromSpace retrieves all docs from a space
func (p *Provider) getDocsFromSpace(ctx context.Context, config *Config, spaceID string) ([]Doc, error) {
	var allDocs []Doc
	cursor := ""

	for {
		resp, err := p.client.GetDocs(ctx, config.APIToken, config.WorkspaceID, cursor, spaceID, "SPACE")
		if err != nil {
			return nil, err
		}

		allDocs = append(allDocs, resp.Docs...)

		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return allDocs, nil
}

// filterByUpdatedSince filters docs to those updated after the given timestamp
func (p *Provider) filterByUpdatedSince(docs []Doc, sinceMs int64) []Doc {
	var filtered []Doc
	for _, doc := range docs {
		updatedMs, err := strconv.ParseInt(doc.DateUpdated, 10, 64)
		if err != nil {
			continue
		}
		if updatedMs > sinceMs {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

// importDoc imports a single ClickUp doc as a document
// Returns the document ID, whether it was skipped, and any error
func (p *Provider) importDoc(ctx context.Context, config *Config, doc Doc, projectID, integrationID string) (string, bool, error) {
	// Fetch pages for this doc
	pages, err := p.client.GetDocPages(ctx, config.APIToken, config.WorkspaceID, doc.ID)
	if err != nil {
		p.log.Warn("failed to fetch pages for doc",
			logger.Error(err),
			slog.String("doc_id", doc.ID))
		pages = nil // Continue without pages
	}

	// Check for existing document by clickupDocId
	existing, err := p.findExistingDoc(ctx, projectID, integrationID, doc.ID)
	if err != nil {
		return "", false, err
	}

	if existing != nil {
		// Check if doc was modified
		if meta, ok := existing.Metadata["clickupUpdatedAt"].(string); ok && meta == doc.DateUpdated {
			// Not modified, skip
			return existing.ID, true, nil
		}

		// Update existing document
		if err := p.updateDocument(ctx, existing, doc, pages, config, integrationID); err != nil {
			return "", false, err
		}
		return existing.ID, false, nil
	}

	// Create new document
	docID, err := p.createDocument(ctx, doc, pages, projectID, integrationID, config)
	if err != nil {
		return "", false, err
	}

	return docID, false, nil
}

// findExistingDoc finds an existing document by clickupDocId
func (p *Provider) findExistingDoc(ctx context.Context, projectID, integrationID, clickupDocID string) (*documents.Document, error) {
	var doc documents.Document
	err := p.db.NewSelect().
		Model(&doc).
		Where("project_id = ?", projectID).
		Where("data_source_integration_id = ?", integrationID).
		Where("metadata->>'clickupDocId' = ?", clickupDocID).
		Scan(ctx)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("find existing doc: %w", err)
	}

	return &doc, nil
}

// createDocument creates a new document from a ClickUp doc
func (p *Provider) createDocument(ctx context.Context, doc Doc, pages []Page, projectID, integrationID string, config *Config) (string, error) {
	// Build content from pages
	content := p.buildContent(doc.Name, pages)

	// Build metadata
	metadata := DocumentMetadata{
		ClickUpDocID:       doc.ID,
		ClickUpWorkspaceID: config.WorkspaceID,
		ClickUpCreatedAt:   doc.DateCreated,
		ClickUpUpdatedAt:   doc.DateUpdated,
		Archived:           doc.Archived,
		PageCount:          p.countPages(pages),
		Provider:           "clickup",
	}

	// Extract space ID if parent is a space (type 6)
	if doc.Parent.Type == 6 {
		metadata.ClickUpSpaceID = doc.Parent.ID
	}

	// Extract avatar
	if doc.Avatar != nil {
		metadata.Avatar = doc.Avatar.Value
	}

	metadataMap := make(map[string]any)
	metaJSON, _ := json.Marshal(metadata)
	json.Unmarshal(metaJSON, &metadataMap)

	mimeType := "text/markdown"
	sourceType := SourceTypeClickUp
	conversionStatus := "not_required"

	document := &documents.Document{
		ID:                      uuid.New().String(),
		ProjectID:               projectID,
		Filename:                &doc.Name,
		Content:                 &content,
		MimeType:                &mimeType,
		SourceType:              &sourceType,
		DataSourceIntegrationID: &integrationID,
		ConversionStatus:        &conversionStatus,
		Metadata:                metadataMap,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}

	if err := p.docRepo.Create(ctx, document); err != nil {
		return "", fmt.Errorf("create document: %w", err)
	}

	p.log.Debug("created document from ClickUp doc",
		slog.String("document_id", document.ID),
		slog.String("clickup_doc_id", doc.ID),
		slog.String("name", doc.Name))

	return document.ID, nil
}

// updateDocument updates an existing document from a ClickUp doc
func (p *Provider) updateDocument(ctx context.Context, existing *documents.Document, doc Doc, pages []Page, config *Config, integrationID string) error {
	// Build content from pages
	content := p.buildContent(doc.Name, pages)

	// Build metadata
	metadata := DocumentMetadata{
		ClickUpDocID:       doc.ID,
		ClickUpWorkspaceID: config.WorkspaceID,
		ClickUpCreatedAt:   doc.DateCreated,
		ClickUpUpdatedAt:   doc.DateUpdated,
		Archived:           doc.Archived,
		PageCount:          p.countPages(pages),
		Provider:           "clickup",
	}

	if doc.Parent.Type == 6 {
		metadata.ClickUpSpaceID = doc.Parent.ID
	}

	if doc.Avatar != nil {
		metadata.Avatar = doc.Avatar.Value
	}

	metadataMap := make(map[string]any)
	metaJSON, _ := json.Marshal(metadata)
	json.Unmarshal(metaJSON, &metadataMap)

	// Update fields
	existing.Filename = &doc.Name
	existing.Content = &content
	existing.Metadata = metadataMap
	existing.UpdatedAt = time.Now()

	_, err := p.db.NewUpdate().
		Model(existing).
		WherePK().
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}

	p.log.Debug("updated document from ClickUp doc",
		slog.String("document_id", existing.ID),
		slog.String("clickup_doc_id", doc.ID),
		slog.String("name", doc.Name))

	return nil
}

// buildContent combines doc name with page content into markdown
func (p *Provider) buildContent(docName string, pages []Page) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", docName))

	if len(pages) == 0 {
		sb.WriteString("[No content]\n")
	} else {
		p.appendPagesContent(&sb, pages, 2)
	}

	return sb.String()
}

// appendPagesContent recursively appends page content with proper heading levels
func (p *Provider) appendPagesContent(sb *strings.Builder, pages []Page, level int) {
	for _, page := range pages {
		if page.Name == "" {
			continue
		}

		// Add page heading
		headerPrefix := strings.Repeat("#", level)
		sb.WriteString(fmt.Sprintf("%s %s\n\n", headerPrefix, page.Name))

		// Add page content
		if page.Content != "" {
			sb.WriteString(page.Content)
			sb.WriteString("\n\n")
		}

		// Recursively add nested pages
		if len(page.Pages) > 0 {
			p.appendPagesContent(sb, page.Pages, level+1)
		}
	}
}

// countPages counts total pages including nested pages
func (p *Provider) countPages(pages []Page) int {
	count := 0
	for _, page := range pages {
		count++
		if len(page.Pages) > 0 {
			count += p.countPages(page.Pages)
		}
	}
	return count
}
