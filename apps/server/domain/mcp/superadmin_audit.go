package mcp

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SuperadminAuditWriter records an operator action for the audit trail. The DB
// implementation inserts into kb.audit_log; the write is best-effort and never
// fails the tool call it records.
type SuperadminAuditWriter interface {
	Write(ctx context.Context, entry SuperadminAuditEntry) error
}

// SuperadminAuditEntry is the normalized audit record for one operator action.
// The field set maps onto kb.audit_log columns (no migration; the table exists
// in the baseline).
type SuperadminAuditEntry struct {
	EventType    string         `json:"event_type"`
	Outcome      string         `json:"outcome"` // "success" | "error"
	UserID       string         `json:"user_id"`
	UserEmail    string         `json:"user_email"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	Action       string         `json:"action"`
	Endpoint     string         `json:"endpoint"`
	HTTPMethod   string         `json:"http_method"`
	StatusCode   int            `json:"status_code"`
	ErrorMessage string         `json:"error_message"`
	IPAddress    string         `json:"ip_address"`
	UserAgent    string         `json:"user_agent"`
	RequestID    string         `json:"request_id"`
	Details      map[string]any `json:"details"`
	Duration     time.Duration  `json:"-"` // persisted as details.duration_ms
}

// auditLogRow is the bun model for kb.audit_log.
type auditLogRow struct {
	bun.BaseModel `bun:"table:kb.audit_log"`

	EventType    string         `bun:"event_type,notnull"`
	Outcome      string         `bun:"outcome,notnull"`
	UserID       *string        `bun:"user_id,type:uuid"`
	UserEmail    string         `bun:"user_email"`
	ResourceType string         `bun:"resource_type"`
	ResourceID   string         `bun:"resource_id"`
	Action       string         `bun:"action,notnull"`
	Endpoint     string         `bun:"endpoint,notnull"`
	HTTPMethod   string         `bun:"http_method,notnull"`
	StatusCode   *int           `bun:"status_code"`
	ErrorMessage string         `bun:"error_message"`
	IPAddress    string         `bun:"ip_address"`
	UserAgent    string         `bun:"user_agent"`
	RequestID    string         `bun:"request_id"`
	Details      map[string]any `bun:"details,type:jsonb"`
}

// auditLogWriter is the DB-backed SuperadminAuditWriter.
type auditLogWriter struct {
	db  bun.IDB
	log *slog.Logger
}

// NewSuperadminAuditWriter returns the DB-backed audit writer for the operator
// MCP surface. Wired via fx under the MCP + SuperAdmin feature flags.
func NewSuperadminAuditWriter(db bun.IDB, log *slog.Logger) SuperadminAuditWriter {
	return &auditLogWriter{db: db, log: log.With(logger.Scope("mcp.superadmin.audit"))}
}

// Write persists a redacted audit record to kb.audit_log.
func (w *auditLogWriter) Write(ctx context.Context, e SuperadminAuditEntry) error {
	details := RedactSecrets(e.Details)
	if details == nil {
		details = map[string]any{}
	}
	details["duration_ms"] = e.Duration.Milliseconds()
	row := &auditLogRow{
		EventType:    e.EventType,
		Outcome:      e.Outcome,
		UserEmail:    e.UserEmail,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Action:       e.Action,
		Endpoint:     e.Endpoint,
		HTTPMethod:   e.HTTPMethod,
		ErrorMessage: RedactString(e.ErrorMessage),
		IPAddress:    e.IPAddress,
		UserAgent:    e.UserAgent,
		RequestID:    e.RequestID,
		Details:      details,
	}
	if e.UserID != "" {
		row.UserID = &e.UserID
	}
	if e.StatusCode != 0 {
		row.StatusCode = &e.StatusCode
	}
	_, err := w.db.NewInsert().Model(row).Exec(ctx)
	return err
}

// auditSuperadmin emits a structured slog line and, best-effort, persists the
// audit record. A store failure is logged and never returned — auditing must not
// become the availability bottleneck for recovery actions.
func (s *Service) auditSuperadmin(ctx context.Context, e SuperadminAuditEntry) {
	attrs := []any{
		slog.String("event_type", e.EventType),
		slog.String("outcome", e.Outcome),
		slog.String("user_id", e.UserID),
		slog.String("user_email", e.UserEmail),
		slog.String("action", e.Action),
		slog.String("endpoint", e.Endpoint),
		slog.String("http_method", e.HTTPMethod),
		slog.Int("status_code", e.StatusCode),
		slog.Duration("duration", e.Duration),
	}
	if e.ErrorMessage != "" {
		attrs = append(attrs, slog.String("error", RedactString(e.ErrorMessage)))
	}
	if e.IPAddress != "" {
		attrs = append(attrs, slog.String("ip_address", e.IPAddress))
	}
	if e.UserAgent != "" {
		attrs = append(attrs, slog.String("user_agent", e.UserAgent))
	}
	if e.RequestID != "" {
		attrs = append(attrs, slog.String("request_id", e.RequestID))
	}
	s.log.Info("superadmin audit", attrs...)

	if s.superadminAuditWriter == nil {
		return
	}
	if err := s.superadminAuditWriter.Write(ctx, e); err != nil {
		s.log.Warn("superadmin audit write failed", logger.Error(err), slog.String("action", e.Action))
	}
}

// RedactSecrets returns a deep copy of details with credential-bearing values
// replaced by "[REDACTED]". It redacts any key whose name carries a credential
// marker (api_key, service_account_json, token, password, secret, credential,
// authorization, bearer) — matching the provider tool args (api_key,
// service_account_json) and service-token mint (token). The returned map is safe
// to store in the audit trail or emit to logs.
func RedactSecrets(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	out := make(map[string]any, len(details))
	for k, v := range details {
		out[k] = redactSecretValue(k, v)
	}
	return out
}

func redactSecretValue(key string, v any) any {
	if isSecretKey(key) {
		return "[REDACTED]"
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k2, v2 := range t {
			out[k2] = redactSecretValue(k2, v2)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, v2 := range t {
			out[i] = redactSecretValue(key, v2)
		}
		return out
	default:
		return v
	}
}

// isSecretKey reports whether a map key names a credential-bearing field.
func isSecretKey(key string) bool {
	k := strings.ToLower(key)
	switch {
	case strings.Contains(k, "token"):
		return true
	case strings.Contains(k, "password"):
		return true
	case strings.Contains(k, "secret"):
		return true
	case strings.Contains(k, "api_key"):
		return true
	case strings.Contains(k, "apikey"):
		return true
	case strings.Contains(k, "service_account"):
		return true
	case strings.Contains(k, "credential"):
		return true
	case strings.Contains(k, "authorization"):
		return true
	case strings.Contains(k, "bearer"):
		return true
	}
	return false
}

// secretValueInStringRE matches a credential-bearing `key=value` / `key: value`
// pair inside a free-form string (e.g. an error message). Group 1 captures the
// key + operator + spacing so the replacement keeps the key and swaps only the
// value for "[REDACTED]".
var secretValueInStringRE = regexp.MustCompile(`(?i)((?:api[_-]?key|apikey|service[_-]?account[_-]?json|token|password|passwd|secret|credential|authorization|bearer)\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)

// RedactString redacts credential-bearing values that may appear in a free-form
// string (such as an error message), reusing the same secret-key vocabulary as
// RedactSecrets. Strings without such patterns are returned unchanged, so
// redaction never corrupts the semantics of an ordinary error.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	return secretValueInStringRE.ReplaceAllString(s, `${1}[REDACTED]`)
}
