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
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/uptrace/bun"
)

// Creator handles creating backup archives.
//
// This package implements the backup exporter + creator lane; the importer and
// restorer live in importer.go and restorer.go.
type Creator struct {
	db       *bun.DB
	storage  *storage.Service
	exporter *Exporter
	repo     *Repository
	log      *slog.Logger
}

// NewCreator creates a new backup creator.
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

// CreateBackupOptions contains options for creating a backup.
type CreateBackupOptions struct {
	BackupID       string
	ProjectID      string
	ProjectName    string
	OrganizationID string
	IncludeChat    bool
	IncludeJournal bool
	IncludeDeleted bool
}

// zipCreationResult carries the ZIP artifact outcome from the worker goroutine.
type zipCreationResult struct {
	checksums Checksums
	stats     *BackupStats
	err       error
}

// CreateBackup creates a full backup and uploads it to MinIO.
func (c *Creator) CreateBackup(ctx context.Context, opts CreateBackupOptions) error {
	c.log.Info("starting backup creation",
		slog.String("backup_id", opts.BackupID),
		slog.String("project_id", opts.ProjectID),
	)

	// Create a pipe for streaming ZIP directly to MinIO
	pr, pw := io.Pipe()

	// Track errors from goroutine
	errChan := make(chan zipCreationResult, 1)

	// Start ZIP creation in goroutine
	go func() {
		defer pw.Close()
		checksums, stats, err := c.createZIPArchive(ctx, pw, opts)
		errChan <- zipCreationResult{checksums: checksums, stats: stats, err: err}
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
	zipRes := <-errChan
	if zipRes.err != nil {
		// Clean up uploaded file
		_ = c.storage.Delete(ctx, storageKey)
		return fmt.Errorf("create ZIP: %w", zipRes.err)
	}

	c.log.Info("backup uploaded successfully",
		slog.String("backup_id", opts.BackupID),
		slog.Int64("size_bytes", result.Size),
	)

	// Update backup record with final size, checksums, and status
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
	backup.ManifestChecksum = &zipRes.checksums.Manifest
	backup.ContentChecksum = &zipRes.checksums.Database
	if zipRes.stats != nil {
		backup.Stats = backupStatsToMap(zipRes.stats)
	}
	now := time.Now()
	backup.CompletedAt = &now

	if err := c.repo.Update(ctx, backup); err != nil {
		return fmt.Errorf("update backup: %w", err)
	}

	return nil
}

// createZIPArchive creates the ZIP archive structure and returns the checksums
// recorded in the manifest (so they can be persisted on the backup record)
// together with the per-table contents stats (so they can be surfaced on the
// backup detail page).
func (c *Creator) createZIPArchive(ctx context.Context, w io.Writer, opts CreateBackupOptions) (Checksums, *BackupStats, error) {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	exportOpts := ExportOptions{
		ProjectID:      opts.ProjectID,
		IncludeChat:    opts.IncludeChat,
		IncludeJournal: opts.IncludeJournal,
		IncludeDeleted: opts.IncludeDeleted,
	}

	// 1. Export project configuration
	if err := c.exportProjectConfig(ctx, zipWriter, opts.ProjectID); err != nil {
		return Checksums{}, nil, fmt.Errorf("export project config: %w", err)
	}

	// 2. Export database tables as NDJSON (checksum covers the concatenated NDJSON bytes)
	dbHash := sha256.New()
	stats, err := c.exportDatabaseTables(ctx, zipWriter, exportOpts, dbHash)
	if err != nil {
		return Checksums{}, nil, err
	}

	// 3. Export files from MinIO (checksum covers the concatenated file payloads)
	filesHash := sha256.New()
	files, totalSize, err := c.exportFiles(ctx, zipWriter, opts.ProjectID, filesHash)
	if err != nil {
		return Checksums{}, nil, fmt.Errorf("export files: %w", err)
	}
	stats.Files = len(files)
	stats.TotalSizeBytes = totalSize

	// 4. Create manifest (self-checksum computed over the marshaled manifest)
	dbChecksum := hex.EncodeToString(dbHash.Sum(nil))
	filesChecksum := hex.EncodeToString(filesHash.Sum(nil))
	manifestChecksum, err := c.createManifest(ctx, zipWriter, opts, stats, files, dbChecksum, filesChecksum)
	if err != nil {
		return Checksums{}, nil, err
	}

	c.log.Info("ZIP archive created",
		slog.String("backup_id", opts.BackupID),
		slog.Int("documents", stats.Documents),
		slog.Int("files", len(files)),
	)

	return Checksums{
		Manifest: manifestChecksum,
		Database: dbChecksum,
		Files:    filesChecksum,
	}, stats, nil
}

// exportProjectConfig exports project configuration to project/config.json.
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

// exportDatabaseTables exports all enabled database tables as NDJSON,
// streaming every table's bytes through dbHash for the database checksum.
//
// archive/zip flushes (closes) the PREVIOUS entry every time Create() is
// called, so each table's zip entry must be created and fully written BEFORE
// the next entry is created. A write to a previously created entry fails with
// "zip: write to closed file".
func (c *Creator) exportDatabaseTables(ctx context.Context, zipWriter *zip.Writer, opts ExportOptions, dbHash io.Writer) (*BackupStats, error) {
	stats := &BackupStats{}

	for _, cfg := range enabledExportTables(opts) {
		// Step 1: open this table's zip entry.
		f, err := zipWriter.Create(fmt.Sprintf("database/%s.ndjson", cfg.name))
		if err != nil {
			return stats, fmt.Errorf("create %s file: %w", cfg.name, err)
		}

		// Step 2: export the table immediately (derived branch_lineage has no
		// backing table query).
		w := io.MultiWriter(f, dbHash)
		if cfg.derived {
			count, exportErr := c.exporter.exportBranchLineage(ctx, w, opts.ProjectID)
			if exportErr != nil {
				return stats, exportErr
			}
			setTableStat(stats, cfg.name, count)
			continue
		}
		count, exportErr := c.exporter.exportTable(ctx, cfg, w, opts)
		if exportErr != nil {
			return stats, exportErr
		}
		setTableStat(stats, cfg.name, count)
	}

	return stats, nil
}

// setTableStat records a table's row count on the manifest BackupStats.
// Only the original eight tables are surfaced in the manifest contents.
func setTableStat(stats *BackupStats, table string, count int) {
	switch table {
	case "documents":
		stats.Documents = count
	case "chunks":
		stats.Chunks = count
	case "graph_objects":
		stats.GraphObjects = count
	case "graph_relationships":
		stats.GraphRelationships = count
	case "chat_conversations":
		stats.ChatConversations = count
	case "chat_messages":
		stats.ChatMessages = count
	case "object_extraction_jobs":
		stats.ExtractionJobs = count
	case "project_memberships":
		stats.ProjectMemberships = count
	}
}

// fileDoc is the minimal kb.documents shape needed to export raw files.
type fileDoc struct {
	ID         string  `bun:"id"`
	Filename   *string `bun:"filename"`
	StorageKey *string `bun:"storage_key"`
	MimeType   *string `bun:"mime_type"`
}

// exportFiles exports files from MinIO to the ZIP archive. ZIP entries are
// keyed by the path-sanitized storage_key (never filename), and the returned
// map records storage_key → {filename, mime_type} for the manifest. Each
// payload is streamed through filesHash for the files checksum.
func (c *Creator) exportFiles(ctx context.Context, zipWriter *zip.Writer, projectID string, filesHash io.Writer) (map[string]FileEntry, int64, error) {
	query := c.db.NewSelect().
		Table("kb.documents").
		Column("id", "filename", "storage_key", "mime_type").
		Where("project_id = ?", projectID).
		Where("storage_key IS NOT NULL")

	// Only documents without soft-delete tombstones are exported. Guard on the
	// live schema because kb.documents does not carry deleted_at everywhere.
	cols, err := c.exporter.tableColumns(ctx, "kb", "documents")
	if err == nil {
		for _, col := range cols {
			if col.Name == "deleted_at" {
				query = query.Where("deleted_at IS NULL")
				break
			}
		}
	} else {
		c.log.Warn("failed to resolve kb.documents columns, skipping deleted_at filter",
			slog.Any("error", err))
	}

	var documents []fileDoc
	if err := query.Scan(ctx, &documents); err != nil {
		return nil, 0, fmt.Errorf("query documents: %w", err)
	}

	files := make(map[string]FileEntry)
	usedZipPaths := make(map[string]bool)
	var totalSize int64

	for _, doc := range documents {
		if doc.StorageKey == nil {
			continue
		}
		// A storage_key may be shared by several document rows; export it once.
		if _, seen := files[*doc.StorageKey]; seen {
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

		// ZIP entries are keyed by path-sanitized storage_key, deduped against
		// accidental sanitize collisions so no file overwrites another.
		zipPath := "files/" + uniqueZipPath(*doc.StorageKey, usedZipPaths)
		f, err := zipWriter.Create(zipPath)
		if err != nil {
			reader.Close()
			return files, totalSize, fmt.Errorf("create file in zip: %w", err)
		}

		// Stream file into ZIP while feeding the files checksum.
		size, err := io.Copy(io.MultiWriter(f, filesHash), reader)
		reader.Close()

		if err != nil {
			c.log.Warn("failed to copy file to zip",
				slog.String("document_id", doc.ID),
				slog.Any("error", err),
			)
			continue
		}

		filename := "unnamed"
		if doc.Filename != nil {
			filename = *doc.Filename
		}
		files[*doc.StorageKey] = FileEntry{
			Filename: filename,
			MimeType: doc.MimeType,
		}
		totalSize += size

		// Check for cancellation
		select {
		case <-ctx.Done():
			return files, totalSize, ctx.Err()
		default:
		}
	}

	c.log.Debug("files exported",
		slog.Int("count", len(files)),
		slog.Int64("total_bytes", totalSize),
	)

	return files, totalSize, nil
}

// sanitizeFileKey renders a storage_key as a safe ZIP path segment by
// replacing '/' and any other unsafe characters with '_'.
func sanitizeFileKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// uniqueZipPath returns a sanitized path segment that has not been used yet,
// appending a short keyed suffix on collision.
func uniqueZipPath(storageKey string, used map[string]bool) string {
	base := sanitizeFileKey(storageKey)
	if base == "" {
		base = "file"
	}
	if !used[base] {
		used[base] = true
		return base
	}
	sum := sha256.Sum256([]byte(storageKey))
	suffix := hex.EncodeToString(sum[:4])
	candidate := fmt.Sprintf("%s-%s", base, suffix)
	for used[candidate] {
		sum = sha256.Sum256([]byte(candidate))
		suffix = hex.EncodeToString(sum[:4])
		candidate = fmt.Sprintf("%s-%s", base, suffix)
	}
	used[candidate] = true
	return candidate
}

// createManifest creates manifest.json and returns the manifest checksum.
//
// Checksum contract (the importer MUST reproduce it): manifest.checksums
// holds sha256 over the concatenated database/*.ndjson bytes
// (Checksums.Database), the concatenated files/* payloads (Checksums.Files),
// and the compact-JSON marshaling of the manifest with Checksums.Manifest
// cleared (Checksums.Manifest).
func (c *Creator) createManifest(ctx context.Context, zipWriter *zip.Writer, opts CreateBackupOptions, stats *BackupStats, files map[string]FileEntry, dbChecksum, filesChecksum string) (string, error) {
	manifest := Manifest{
		Version:       "1.0.0",
		SchemaVersion: "20260211_000000",
		CreatedAt:     time.Now(),
		BackupType:    BackupTypeFull,
		Project: ProjectInfo{
			ID:             opts.ProjectID,
			Name:           opts.ProjectName,
			OrganizationID: opts.OrganizationID,
		},
		Contents: *stats,
		Files:    files,
		Checksums: Checksums{
			Manifest: "",
			Database: dbChecksum,
			Files:    filesChecksum,
		},
		Metadata: map[string]any{
			"server_version":  "2.0.0",
			"go_version":      "1.24",
			"include_chat":    opts.IncludeChat,
			"include_journal": opts.IncludeJournal,
		},
	}

	// Manifest checksum: sha256 over the marshaled manifest with the manifest
	// checksum field itself cleared, so the value is not self-referential.
	payload := manifest
	payload.Checksums.Manifest = ""
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal manifest for checksum: %w", err)
	}
	hash := sha256.Sum256(payloadBytes)
	manifestChecksum := hex.EncodeToString(hash[:])
	manifest.Checksums.Manifest = manifestChecksum

	// Create manifest file
	f, err := zipWriter.Create("manifest.json")
	if err != nil {
		return "", fmt.Errorf("create manifest file: %w", err)
	}

	// Write JSON
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return "", fmt.Errorf("encode manifest: %w", err)
	}

	c.log.Debug("manifest created",
		slog.String("checksum", manifestChecksum),
	)

	return manifestChecksum, nil
}
