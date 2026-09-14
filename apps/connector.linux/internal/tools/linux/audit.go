package linux

import "log/slog"

// Audit outcomes for mutating tool calls.
const (
	outcomeSuccess = "success"
	outcomeDenied  = "denied"
	outcomeError   = "error"
)

// auditLogger emits exactly one structured record per mutating filesystem call.
// It never records file contents.
type auditLogger struct {
	logger *slog.Logger
}

func newAuditLogger(l *slog.Logger) *auditLogger {
	if l == nil {
		l = slog.Default()
	}
	return &auditLogger{logger: l}
}

// record logs one mutating call. err is only attached for failed calls and is
// an error message, never file content.
func (a *auditLogger) record(tool, outcome, root, path string, err error) {
	attrs := []any{
		"tool", tool,
		"root", root,
		"path", path,
		"outcome", outcome,
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	a.logger.Info("filesystem tool call", attrs...)
}
