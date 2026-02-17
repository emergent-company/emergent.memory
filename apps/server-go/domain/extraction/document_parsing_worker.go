package extraction

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/emergent-company/emergent/domain/chunking"
	"github.com/emergent-company/emergent/domain/documents"
	"github.com/emergent-company/emergent/internal/storage"
	"github.com/emergent-company/emergent/pkg/kreuzberg"
	"github.com/emergent-company/emergent/pkg/logger"
)

// DocumentParsingWorker processes document parsing jobs.
// It polls for pending jobs, downloads documents from storage,
// sends them to Kreuzberg for text extraction, and stores the results.
type DocumentParsingWorker struct {
	log             *slog.Logger
	jobsService     *DocumentParsingJobsService
	documentsRepo   *documents.Repository
	chunkingService *chunking.Service
	kreuzbergClient *kreuzberg.Client
	storageService  *storage.Service

	// Polling configuration
	interval  time.Duration
	batchSize int

	// Shutdown control
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

// DocumentParsingWorkerConfig contains configuration for the worker
type DocumentParsingWorkerConfig struct {
	Interval  time.Duration
	BatchSize int
}

// NewDocumentParsingWorker creates a new document parsing worker
func NewDocumentParsingWorker(
	jobsService *DocumentParsingJobsService,
	documentsRepo *documents.Repository,
	chunkingService *chunking.Service,
	kreuzbergClient *kreuzberg.Client,
	storageService *storage.Service,
	cfg *DocumentParsingWorkerConfig,
	log *slog.Logger,
) *DocumentParsingWorker {
	interval := 5 * time.Second
	batchSize := 5
	if cfg != nil {
		if cfg.Interval > 0 {
			interval = cfg.Interval
		}
		if cfg.BatchSize > 0 {
			batchSize = cfg.BatchSize
		}
	}

	return &DocumentParsingWorker{
		log:             log.With(logger.Scope("document.parsing.worker")),
		jobsService:     jobsService,
		documentsRepo:   documentsRepo,
		chunkingService: chunkingService,
		kreuzbergClient: kreuzbergClient,
		storageService:  storageService,
		interval:        interval,
		batchSize:       batchSize,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
}

// Start begins the worker polling loop
func (w *DocumentParsingWorker) Start() {
	w.log.Info("starting document parsing worker",
		slog.Duration("interval", w.interval),
		slog.Int("batch_size", w.batchSize),
	)

	// Recover any stale jobs on startup
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	recovered, err := w.jobsService.RecoverStaleJobs(ctx, 0)
	cancel()
	if err != nil {
		w.log.Error("failed to recover stale jobs", logger.Error(err))
	} else if recovered > 0 {
		w.log.Info("recovered stale document parsing jobs", slog.Int("count", recovered))
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	defer close(w.doneCh)

	for {
		select {
		case <-w.stopCh:
			w.log.Info("document parsing worker stopped")
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

// Stop gracefully stops the worker
func (w *DocumentParsingWorker) Stop() {
	w.stopOnce.Do(func() {
		w.log.Info("stopping document parsing worker...")
		close(w.stopCh)

		// Wait for completion with timeout
		select {
		case <-w.doneCh:
			w.log.Info("document parsing worker stopped gracefully")
		case <-time.After(30 * time.Second):
			w.log.Warn("document parsing worker stop timed out")
		}
	})
}

// poll dequeues and processes a batch of jobs
func (w *DocumentParsingWorker) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	jobs, err := w.jobsService.Dequeue(ctx, w.batchSize)
	if err != nil {
		w.log.Error("failed to dequeue jobs", logger.Error(err))
		return
	}

	if len(jobs) == 0 {
		return
	}

	w.log.Debug("processing document parsing jobs", slog.Int("count", len(jobs)))

	for _, job := range jobs {
		select {
		case <-w.stopCh:
			return
		default:
			w.processJob(ctx, job)
		}
	}
}

// processJob handles a single document parsing job
func (w *DocumentParsingWorker) processJob(ctx context.Context, job *DocumentParsingJob) {
	startTime := time.Now()
	jobLog := w.log.With(
		slog.String("job_id", job.ID),
		slog.String("source_type", job.SourceType),
		slog.String("filename", ptrToString(job.SourceFilename)),
	)

	jobLog.Info("processing document parsing job")

	// Determine how to process the document
	mimeType := ptrToString(job.MimeType)
	filename := ptrToString(job.SourceFilename)
	storageKey := ptrToString(job.StorageKey)

	// Check if storage key is available
	if storageKey == "" {
		err := fmt.Errorf("no storage key for document parsing job")
		jobLog.Error("job missing storage key", logger.Error(err))
		w.markFailed(ctx, job, err)
		return
	}

	// Check processing path
	isEmail := kreuzberg.IsEmailFile(mimeType, filename)
	useKreuzberg := !isEmail && kreuzberg.ShouldUseKreuzberg(mimeType, filename)

	var parsedContent string
	var extractionMethod string
	var err error

	if isEmail {
		// Email files - use native email parser (not yet implemented in Go)
		// For now, mark as failed with clear message
		err = fmt.Errorf("email parsing not yet implemented in Go server")
		jobLog.Warn("email parsing not implemented", slog.String("mime_type", mimeType))
		w.markFailed(ctx, job, err)
		return
	} else if useKreuzberg {
		// Binary document - use Kreuzberg for extraction
		parsedContent, err = w.extractWithKreuzberg(ctx, storageKey, filename, mimeType)
		extractionMethod = "kreuzberg"
	} else {
		// Plain text - read directly from storage
		parsedContent, err = w.extractPlainText(ctx, storageKey)
		extractionMethod = "plain_text"
	}

	if err != nil {
		jobLog.Error("document extraction failed",
			slog.String("method", extractionMethod),
			logger.Error(err),
		)
		w.markFailed(ctx, job, err)
		return
	}

	// Mark job as completed
	result := MarkCompletedResult{
		ParsedContent: parsedContent,
		DocumentID:    job.DocumentID,
		Metadata: map[string]interface{}{
			"extractionMethod": extractionMethod,
			"processingTimeMs": time.Since(startTime).Milliseconds(),
		},
	}

	if err := w.jobsService.MarkCompleted(ctx, job.ID, result); err != nil {
		jobLog.Error("failed to mark job completed", logger.Error(err))
		return
	}

	if job.DocumentID != nil {
		if err := w.documentsRepo.UpdateContentAndStatus(ctx, *job.DocumentID, parsedContent, "completed"); err != nil {
			jobLog.Error("failed to update document content", logger.Error(err))
		}

		chunkResult, err := w.chunkingService.RecreateChunks(ctx, job.ProjectID, *job.DocumentID)
		if err != nil {
			jobLog.Error("failed to create chunks", logger.Error(err))
		} else {
			jobLog.Info("created chunks",
				slog.Int("chunks", chunkResult.Summary.NewChunks),
				slog.String("strategy", chunkResult.Summary.Strategy))
		}
	}

	jobLog.Info("document parsing completed",
		slog.String("method", extractionMethod),
		slog.Int("content_length", len(parsedContent)),
		slog.Duration("duration", time.Since(startTime)),
	)
}

// extractWithKreuzberg downloads a file and sends it to Kreuzberg for extraction
func (w *DocumentParsingWorker) extractWithKreuzberg(ctx context.Context, storageKey, filename, mimeType string) (string, error) {
	content, err := w.downloadFile(ctx, storageKey)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}

	// Enable OCR with auto-detection: Kreuzberg will analyze text quality
	// and automatically fallback to OCR if any page has poor/no text.
	// This is optimal for mixed PDFs (some pages scanned, some digital).
	opts := &kreuzberg.ExtractOptions{
		OCRBackend:  "tesseract",
		OCRLanguage: "eng",
		ForceOCR:    false, // Let Kreuzberg auto-detect when OCR is needed
	}
	result, err := w.kreuzbergClient.ExtractText(ctx, content, filename, mimeType, opts)
	if err != nil {
		return "", fmt.Errorf("kreuzberg extraction: %w", err)
	}

	return result.Content, nil
}

// extractPlainText downloads a plain text file directly
func (w *DocumentParsingWorker) extractPlainText(ctx context.Context, storageKey string) (string, error) {
	content, err := w.downloadFile(ctx, storageKey)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}

	return string(content), nil
}

// downloadFile downloads a file from storage
func (w *DocumentParsingWorker) downloadFile(ctx context.Context, storageKey string) ([]byte, error) {
	if !w.storageService.Enabled() {
		return nil, fmt.Errorf("storage service not enabled")
	}

	reader, err := w.storageService.Download(ctx, storageKey)
	if err != nil {
		return nil, fmt.Errorf("download from storage: %w", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read file content: %w", err)
	}

	return content, nil
}

// markFailed marks a job as failed
func (w *DocumentParsingWorker) markFailed(ctx context.Context, job *DocumentParsingJob, err error) {
	if markErr := w.jobsService.MarkFailed(ctx, job.ID, err); markErr != nil {
		w.log.Error("failed to mark job as failed",
			slog.String("job_id", job.ID),
			logger.Error(markErr),
		)
	}
}

// JobsService returns the underlying jobs service for testing/management
func (w *DocumentParsingWorker) JobsService() *DocumentParsingJobsService {
	return w.jobsService
}
