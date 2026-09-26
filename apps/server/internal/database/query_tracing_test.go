package database

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// setupTestTracer installs an in-memory span recorder as the global tracer
// provider and restores the original provider on cleanup.
func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	orig := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(orig) })
	return rec
}

// newSQLMockDB builds a *bun.DB over go-sqlmock using the PostgreSQL dialect.
func newSQLMockDB(t *testing.T) (*bun.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { sqldb.Close() })
	return bun.NewDB(sqldb, pgdialect.New()), mock
}

// tracingEnabledConfig returns a config with OTel tracing enabled.
func tracingEnabledConfig() *config.Config {
	return &config.Config{
		Otel:     config.OtelConfig{ExporterEndpoint: "http://localhost:4318"},
		Database: config.DatabaseConfig{Database: "testdb"},
	}
}

// spanByStatus returns the span with the given status code, or nil.
func spanWithName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func attrValue(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, a := range s.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}
	return "", false
}

func TestTracingHook_Disabled_NoSpans(t *testing.T) {
	rec := setupTestTracer(t)
	db, mock := newSQLMockDB(t)

	// Tracing disabled: helper must NOT register the hook.
	addTracingHook(db, &config.Config{})

	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1")).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))

	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	rows.Close()

	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("expected 0 spans when tracing disabled, got %d", got)
	}
}

func TestTracingHook_SelectSpan_Attributes(t *testing.T) {
	rec := setupTestTracer(t)
	db, mock := newSQLMockDB(t)

	addTracingHook(db, tracingEnabledConfig())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1")).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))

	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	rows.Close()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span, got %d", len(spans))
	}

	span := spanWithName(spans, "SELECT")
	if span == nil {
		t.Fatalf("expected a span named SELECT, got names: %v", spanNames(spans))
	}

	if v, ok := attrValue(span, "db.system"); !ok || v != "postgresql" {
		t.Errorf("db.system = %q (present=%v), want %q", v, ok, "postgresql")
	}

	if v, ok := attrValue(span, "db.operation"); !ok || v != "SELECT" {
		t.Errorf("db.operation = %q (present=%v), want %q", v, ok, "SELECT")
	}

	if v, ok := attrValue(span, "db.statement"); !ok || v == "" {
		t.Errorf("db.statement present=%v value=%q, want non-empty", ok, v)
	}

	for _, key := range []string{"code.function", "code.filepath", "code.lineno"} {
		if _, ok := attrValue(span, key); !ok {
			t.Errorf("expected source-location attribute %q on span", key)
		}
	}
}

func TestTracingHook_Error_SetsStatusAndRecordsError(t *testing.T) {
	rec := setupTestTracer(t)
	db, mock := newSQLMockDB(t)

	addTracingHook(db, tracingEnabledConfig())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT broken")).
		WillReturnError(errors.New("boom"))

	_, err := db.QueryContext(context.Background(), "SELECT broken")
	if err == nil {
		t.Fatal("expected query to return an error")
	}

	spans := rec.Ended()
	span := spanWithName(spans, "SELECT")
	if span == nil {
		t.Fatalf("expected a span named SELECT, got names: %v", spanNames(spans))
	}

	if code := span.Status().Code.String(); code != "Error" {
		t.Errorf("span status code = %q, want %q", code, "Error")
	}

	hasException := false
	for _, e := range span.Events() {
		if e.Name == "exception" {
			hasException = true
			break
		}
	}
	if !hasException {
		t.Error("expected an exception event recorded on the span")
	}
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}

// TestTracingHook_StatementDoesNotLeakBoundValues is the regression guard for
// #929: the bunotel hook records db.statement in placeholder form (?) because
// WithFormattedQueries is not enabled. If that option is ever turned on, bun's
// formatted query (which interpolates bound arguments) would put token hashes
// and other secret values straight into trace attributes. This test runs a
// query with a sentinel bound argument and asserts the sentinel never appears
// in db.statement or any other span attribute.
func TestTracingHook_StatementDoesNotLeakBoundValues(t *testing.T) {
	const sentinel = "SENTINEL-SECRET-TOKEN-VALUE"

	rec := setupTestTracer(t)
	db, mock := newSQLMockDB(t)

	addTracingHook(db, tracingEnabledConfig())

	// bun formats the raw QueryContext query and interpolates the bound arg into
	// the string it hands to database/sql, so the mock must match the formatted
	// query (sentinel present). The span, in contrast, must record the
	// placeholder form.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM api_tokens WHERE token_hash = '" + sentinel + "'")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))

	rows, err := db.QueryContext(context.Background(), "SELECT * FROM api_tokens WHERE token_hash = ?", sentinel)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	// Prove the query actually succeeded — the span assertions below must not be
	// able to pass merely because the query failed early.
	if !rows.Next() {
		t.Fatal("expected at least one row")
	}
	var id int
	if err := rows.Scan(&id); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}

	spans := rec.Ended()
	span := spanWithName(spans, "SELECT")
	if span == nil {
		t.Fatalf("expected a span named SELECT, got names: %v", spanNames(spans))
	}

	stmt, ok := attrValue(span, "db.statement")
	if !ok {
		t.Fatal("db.statement attribute missing")
	}
	if !strings.Contains(stmt, "?") {
		t.Errorf("db.statement = %q, want placeholder form containing '?'", stmt)
	}
	if strings.Contains(stmt, sentinel) {
		t.Errorf("db.statement leaked the bound value: %q contains %q", stmt, sentinel)
	}

	for _, a := range span.Attributes() {
		if strings.Contains(a.Value.AsString(), sentinel) {
			t.Errorf("span attribute %q leaked the bound value: %q", a.Key, a.Value.AsString())
		}
	}
}
