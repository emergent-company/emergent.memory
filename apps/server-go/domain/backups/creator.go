package backups

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/emergent/emergent-core/internal/storage"
	"github.com/uptrace/bun"
)

// Creator handles creating backup archives
type Creator struct {
	db       *bun.DB
	storage  *storage.Service
	exporter *Exporter
	repo     *Repository
	log      *slog.Logger
}

// NewCreator creates a new backup creator
func NewCreator(
	db *bun.DB,
	storage *storage.Service,
	repo *Repository,
	log *slog.Logger,
) *Creator {
	return &Creator{
		db:       db,
		storage:  storage,
		exporter: NewExporter(db, log),
		repo:     repo,
		log:      log.With(slog.String("component", "backups.creator")),
	}
}

// CreateBackupOptions contains options for creating a backup
type CreateBackupOptions struct {
	BackupID       string
	ProjectID      string
	ProjectName    string
	OrganizationID string
	IncludeChat    bool
	IncludeDeleted bool
}

// CreateBackup creates a full backup and uploads it to MinIO
func (c *Creator) CreateBackup(ctx context.Context, opts CreateBackupOptions) error {
	c.log.Info("starting backup creation",
		slog.String("backup_id", opts.BackupID),
		slog.String("project_id", opts.ProjectID),
	)

	// Create a pipe for streaming ZIP directly to MinIO
	pr, pw := io.Pipe()

	// Track errors from goroutine
	errChan := make(chan error, 1)

	// Start ZIP creation in goroutine
	go func() {
		defer pw.Close()
		err := c.createZIPArchive(ctx, pw, opts)
		errChan <- err
	}()

	// Upload to MinIO while ZIP is being created
	storageKey := GenerateStorageKey(opts.OrganizationID, opts.BackupID)

	// We don't know the size yet, so we'll update it after upload
	uploadOpts := storage.UploadOptions{
		ContentType: "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="backup-%s-%s.zip"`,
			opts.ProjectName, time.Now().Format("2006-01-02")),
	}

	c.log.Debug("uploading backup to storage",
		slog.String("storage_key", storageKey),
	)

	// Upload with unknown size (MinIO will handle streaming)
	result, err := c.storage.Upload(ctx, storageKey, pr, -1, uploadOpts)
	if err != nil {
		c.log.Error("failed to upload backup",
			slog.String("backup_id", opts.BackupID),
			slog.Any("error", err),
		)
		return fmt.Errorf("upload backup: %w", err)
	}

	// Wait for ZIP creation to complete
	zipErr := <-errChan
	if zipErr != nil {
		// Clean up uploaded file
		_ = c.storage.Delete(ctx, storageKey)
		return fmt.Errorf("create ZIP: %w", zipErr)
	}

	c.log.Info("backup uploaded successfully",
		slog.String("backup_id", opts.BackupID),
		slog.Int64("size_bytes", result.Size),
	)

	// Update backup record with final size
	backup, err := c.repo.GetByID(ctx, opts.OrganizationID, opts.BackupID)
	if err != nil {
		return fmt.Errorf("get backup: %w", err)
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}

	backup.SizeBytes = result.Size
	backup.Status = BackupStatusReady
	backup.Progress = 100
	now := time.Now()
	backup.CompletedAt = &now

	if err := c.repo.Update(ctx, backup); err != nil {
		return fmt.Errorf("update backup: %w", err)
	}

	return nil
}

// createZIPArchive creates the ZIP archive structure
func (c *Creator) createZIPArchive(ctx context.Context, w io.Writer, opts CreateBackupOptions) error {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	exportOpts := ExportOptions{
		ProjectID:      opts.ProjectID,
		IncludeChat:    opts.IncludeChat,
		IncludeDeleted: opts.IncludeDeleted,
	}

	// 1. Export project configuration
	if err := c.exportProjectConfig(ctx, zipWriter, opts.ProjectID); err != nil {
		return fmt.Errorf("export project config: %w", err)
	}

	// 2. Export database tables as NDJSON
	stats, err := c.exportDatabaseTables(ctx, zipWriter, exportOpts)
	if err != nil {
		return fmt.Errorf("export database: %w", err)
	}

	// 3. Export files from MinIO
	fileCount, totalSize, err := c.exportFiles(ctx, zipWriter, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("export files: %w", err)
	}

	stats.Files = fileCount
	stats.TotalSizeBytes = totalSize

	// 4. Create manifest
	if err := c.createManifest(ctx, zipWriter, opts, stats); err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	c.log.Info("ZIP archive created",
		slog.String("backup_id", opts.BackupID),
		slog.Int("documents", stats.Documents),
		slog.Int("files", fileCount),
	)

	return nil
}

// exportProjectConfig exports project configuration to project/config.json
func (c *Creator) exportProjectConfig(ctx context.Context, zipWriter *zip.Writer, projectID string) error {
	// Query project
	var project map[string]any
	err := c.db.NewSelect().
		Table("kb.projects").
		Where("id = ?", projectID).
		Scan(ctx, &project)

	if err != nil {
		return fmt.Errorf("query project: %w", err)
	}

	// Create file in ZIP
	f, err := zipWriter.Create("project/config.json")
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}

	// Write JSON
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(project); err != nil {
		return fmt.Errorf("encode project: %w", err)
	}

	return nil
}

// exportDatabaseTables exports all database tables as NDJSON
func (c *Creator) exportDatabaseTables(ctx context.Context, zipWriter *zip.Writer, opts ExportOptions) (*BackupStats, error) {
	// Create writers for each table
	writers := make(map[string]io.Writer)

	tables := []string{
		"documents",
		"chunks",
		"graph_objects",
		"graph_relationships",
		"chat_conversations",
		"chat_messages",
		"extraction_jobs",
		"project_memberships",
	}

	for _, table := range tables {
		f, err := zipWriter.Create(fmt.Sprintf("database/%s.ndjson", table))
		if err != nil {
			return nil, fmt.Errorf("create %s file: %w", table, err)
		}
		writers[table] = f
	}

	// Export all tables
	result, err := c.exporter.ExportAll(ctx, writers, opts)
	if err != nil {
		return nil, err
	}

	return &BackupStats{
		Documents:          result.Documents,
		Chunks:             result.Chunks,
		GraphObjects:       result.GraphObjects,
		GraphRelationships: result.GraphRelationships,
		ChatConversations:  result.ChatConversations,
		ChatMessages:       result.ChatMessages,
		ExtractionJobs:     result.ExtractionJobs,
		ProjectMemberships: result.ProjectMemberships,
	}, nil
}

// exportFiles exports files from MinIO to the ZIP archive
func (c *Creator) exportFiles(ctx context.Context, zipWriter *zip.Writer, projectID string) (int, int64, error) {
	// Query all documents with storage keys
	var documents []struct {
		ID         string  `bun:"id"`
		Filename   *string `bun:"filename"`
		StorageKey *string `bun:"storage_key"`
		MimeType   *string `bun:"mime_type"`
	}

	err := c.db.NewSelect().
		Table("kb.documents").
		Column("id", "filename", "storage_key", "mime_type").
		Where("project_id = ?", projectID).
		Where("storage_key IS NOT NULL").
		Where("deleted_at IS NULL").
		Scan(ctx, &documents)

	if err != nil {
		return 0, 0, fmt.Errorf("query documents: %w", err)
	}

	var totalSize int64
	count := 0

	for _, doc := range documents {
		if doc.StorageKey == nil {
			continue
		}

		// Download file from MinIO
		reader, err := c.storage.Download(ctx, *doc.StorageKey)
		if err != nil {
			c.log.Warn("failed to download file, skipping",
				slog.String("document_id", doc.ID),
				slog.String("storage_key", *doc.StorageKey),
				slog.Any("error", err),
			)
			continue
		}

		// Create file in ZIP
		filename := "unnamed"
		if doc.Filename != nil {
			filename = *doc.Filename
		}

		zipPath := fmt.Sprintf("files/%s", filename)
		f, err := zipWriter.Create(zipPath)
		if err != nil {
			reader.Close()
			return count, totalSize, fmt.Errorf("create file in zip: %w", err)
		}

		// Stream file into ZIP
		size, err := io.Copy(f, reader)
		reader.Close()

		if err != nil {
			c.log.Warn("failed to copy file to zip",
				slog.String("document_id", doc.ID),
				slog.Any("error", err),
			)
			continue
		}

		totalSize += size
		count++

		// Check for cancellation
		select {
		case <-ctx.Done():
			return count, totalSize, ctx.Err()
		default:
		}
	}

	c.log.Debug("files exported",
		slog.Int("count", count),
		slog.Int64("total_bytes", totalSize),
	)

	return count, totalSize, nil
}

// createManifest creates the manifest.json file
func (c *Creator) createManifest(ctx context.Context, zipWriter *zip.Writer, opts CreateBackupOptions, stats *BackupStats) error {
	manifest := Manifest{
		Version:       "1.0.0",
		SchemaVersion: "20260211_000000", // Today's date
		CreatedAt:     time.Now(),
		BackupType:    BackupTypeFull,
		Project: ProjectInfo{
			ID:             opts.ProjectID,
			Name:           opts.ProjectName,
			OrganizationID: opts.OrganizationID,
		},
		Contents: *stats,
		Checksums: Checksums{
			// TODO: Calculate actual checksums
			Manifest: "",
			Database: "",
			Files:    "",
		},
		Metadata: map[string]any{
			"server_version": "2.0.0",
			"go_version":     "1.24",
		},
	}

	// Create manifest file
	f, err := zipWriter.Create("manifest.json")
	if err != nil {
		return fmt.Errorf("create manifest file: %w", err)
	}

	// Write JSON
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	// Calculate manifest checksum
	manifestJSON, _ := json.Marshal(manifest)
	hash := sha256.Sum256(manifestJSON)
	checksumHex := hex.EncodeToString(hash[:])

	c.log.Debug("manifest created",
		slog.String("checksum", checksumHex),
	)

	return nil
}
