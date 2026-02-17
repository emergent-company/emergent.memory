package email

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/emergent-company/emergent/pkg/logger"
)

// Worker processes email jobs from the queue.
// It follows the same pattern as NestJS workers:
// - Polling-based with configurable interval
// - Graceful shutdown waiting for current batch
// - Stale job recovery on startup
// - Metrics tracking
type Worker struct {
	jobs      *JobsService
	sender    Sender
	templates *TemplateService
	cfg       *Config
	log       *slog.Logger
	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	mu        sync.Mutex
	wg        sync.WaitGroup

	// Metrics
	processedCount int64
	successCount   int64
	failureCount   int64
	metricsMu      sync.RWMutex
}

// Sender is the interface for sending emails
type Sender interface {
	Send(ctx context.Context, opts SendOptions) (*SendResult, error)
}

// SendOptions contains options for sending an email
type SendOptions struct {
	To      string
	ToName  string
	Subject string
	HTML    string
	Text    string
}

// SendResult contains the result of sending an email
type SendResult struct {
	Success   bool
	MessageID string
	Error     string
}

// NewWorker creates a new email worker
func NewWorker(jobs *JobsService, sender Sender, templates *TemplateService, cfg *Config, log *slog.Logger) *Worker {
	return &Worker{
		jobs:      jobs,
		sender:    sender,
		templates: templates,
		cfg:       cfg,
		log:       log.With(logger.Scope("email.worker")),
	}
}

// Start begins the worker's polling loop
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Check if email is enabled
	if !w.cfg.Enabled {
		w.log.Info("email worker not started (EMAIL_ENABLED=false)")
		w.mu.Unlock()
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	// Recover stale jobs on startup
	go w.recoverStaleJobsOnStartup(ctx)

	w.log.Info("email worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker, waiting for current batch to complete
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for email worker to stop...")

	// Wait for worker to stop or context to be cancelled
	select {
	case <-w.stoppedCh:
		w.log.Info("email worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("email worker stop timeout, forcing shutdown")
	}

	return nil
}

// recoverStaleJobsOnStartup recovers stale jobs on startup
func (w *Worker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverStaleJobs(ctx, 10)
	if err != nil {
		w.log.Warn("failed to recover stale jobs on startup",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale email jobs on startup",
			slog.Int("count", recovered))
	}
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.stoppedCh)

	ticker := time.NewTicker(w.cfg.WorkerInterval())
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.processBatch(ctx); err != nil {
				w.log.Warn("process batch failed", slog.String("error", err.Error()))
			}
		}
	}
}

// processBatch processes a batch of email jobs
func (w *Worker) processBatch(ctx context.Context) error {
	// Check if we should stop
	select {
	case <-w.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	jobs, err := w.jobs.Dequeue(ctx, w.cfg.WorkerBatchSize)
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			w.log.Warn("process job failed",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// processJob processes a single email job
func (w *Worker) processJob(ctx context.Context, job *EmailJob) error {
	startTime := time.Now()

	// Render the template
	templateContext := make(TemplateContext)
	if job.TemplateData != nil {
		for k, v := range job.TemplateData {
			templateContext[k] = v
		}
	}
	
	// Add common fields if not present
	if _, ok := templateContext["title"]; !ok {
		templateContext["title"] = job.Subject
	}
	if _, ok := templateContext["previewText"]; !ok {
		templateContext["previewText"] = job.Subject
	}
	if job.ToName != nil {
		templateContext["recipientName"] = *job.ToName
	}

	var htmlContent, textContent string

	// Check if template exists
	if w.templates.HasTemplate(job.TemplateName) {
		result, err := w.templates.Render(job.TemplateName, templateContext, "default")
		if err != nil {
			w.log.Warn("template render failed, using fallback",
				slog.String("template", job.TemplateName),
				slog.String("error", err.Error()))
			htmlContent = w.generateFallbackHTML(job, templateContext)
			textContent = w.generateFallbackText(job, templateContext)
		} else {
			htmlContent = result.HTML
			textContent = result.Text
		}
	} else {
		// No template found, use fallback
		w.log.Debug("template not found, using fallback",
			slog.String("template", job.TemplateName))
		htmlContent = w.generateFallbackHTML(job, templateContext)
		textContent = w.generateFallbackText(job, templateContext)
	}

	// Send the email
	toName := ""
	if job.ToName != nil {
		toName = *job.ToName
	}

	result, err := w.sender.Send(ctx, SendOptions{
		To:      job.ToEmail,
		ToName:  toName,
		Subject: job.Subject,
		HTML:    htmlContent,
		Text:    textContent,
	})

	if err != nil {
		// Sender error
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

	if !result.Success {
		// Mailgun returned an error
		sendErr := &sendError{message: result.Error}
		if markErr := w.jobs.MarkFailed(ctx, job.ID, sendErr); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return sendErr
	}

	// Mark as sent
	if err := w.jobs.MarkSent(ctx, job.ID, result.MessageID); err != nil {
		w.log.Error("failed to mark job as sent",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
		return err
	}

	durationMs := time.Since(startTime).Milliseconds()
	w.log.Debug("email sent",
		slog.String("job_id", job.ID),
		slog.String("to_email", job.ToEmail),
		slog.String("template", job.TemplateName),
		slog.String("message_id", result.MessageID),
		slog.Int64("duration_ms", durationMs))

	w.incrementSuccess()
	return nil
}

// generateFallbackHTML creates a simple HTML email when template is not available
func (w *Worker) generateFallbackHTML(job *EmailJob, ctx TemplateContext) string {
	recipientName := ""
	if name, ok := ctx["recipientName"].(string); ok {
		recipientName = name
	}
	
	greeting := "Hello"
	if recipientName != "" {
		greeting = "Hello " + recipientName
	}

	message := ""
	if msg, ok := ctx["message"].(string); ok {
		message = msg
	}

	ctaUrl := ""
	ctaText := ""
	if url, ok := ctx["ctaUrl"].(string); ok {
		ctaUrl = url
	}
	if text, ok := ctx["ctaText"].(string); ok {
		ctaText = text
	}
	if ctaText == "" {
		ctaText = "Click Here"
	}

	html := `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>` + job.Subject + `</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; line-height: 1.6; color: #374151; margin: 0; padding: 20px; background-color: #f3f4f6;">
  <div style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; padding: 32px;">
    <p style="font-size: 16px; margin-bottom: 16px;">` + greeting + `,</p>
    <p style="margin-bottom: 16px;">` + message + `</p>`

	if ctaUrl != "" {
		html += `
    <p style="margin-bottom: 24px;">
      <a href="` + ctaUrl + `" style="display: inline-block; background-color: #4F46E5; color: #ffffff; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600;">` + ctaText + `</a>
    </p>`
	}

	html += `
    <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0 16px;">
    <p style="font-size: 12px; color: #6b7280;">
      This email was sent by Emergent.
    </p>
  </div>
</body>
</html>`

	return html
}

// generateFallbackText creates plain text email when template is not available
func (w *Worker) generateFallbackText(job *EmailJob, ctx TemplateContext) string {
	recipientName := ""
	if name, ok := ctx["recipientName"].(string); ok {
		recipientName = name
	}

	greeting := "Hello"
	if recipientName != "" {
		greeting = "Hello " + recipientName
	}

	message := ""
	if msg, ok := ctx["message"].(string); ok {
		message = msg
	}

	ctaUrl := ""
	if url, ok := ctx["ctaUrl"].(string); ok {
		ctaUrl = url
	}

	text := greeting + ",\n\n" + message

	if ctaUrl != "" {
		text += "\n\nLink: " + ctaUrl
	}

	text += "\n\n---\nThis email was sent by Emergent."

	return text
}

// incrementSuccess increments both processed and success counters
func (w *Worker) incrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

// incrementFailure increments both processed and failure counters
func (w *Worker) incrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// Metrics returns current worker metrics
func (w *Worker) Metrics() WorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()

	return WorkerMetrics{
		Processed: w.processedCount,
		Succeeded: w.successCount,
		Failed:    w.failureCount,
	}
}

// WorkerMetrics contains worker metrics
type WorkerMetrics struct {
	Processed int64 `json:"processed"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

// IsRunning returns whether the worker is currently running
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// sendError is a simple error type for send failures
type sendError struct {
	message string
}

func (e *sendError) Error() string {
	return e.message
}
