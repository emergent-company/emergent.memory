package database

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestStatementTimeoutFromContext pins the context round-trip and the
// non-positive no-op contract.
func TestStatementTimeoutFromContext(t *testing.T) {
	if _, ok := statementTimeoutFromContext(context.Background()); ok {
		t.Fatal("bare context unexpectedly carries a statement timeout")
	}
	if _, ok := statementTimeoutFromContext(WithStatementTimeout(context.Background(), 0)); ok {
		t.Fatal("zero timeout should be ignored")
	}
	if _, ok := statementTimeoutFromContext(WithStatementTimeout(context.Background(), -time.Second)); ok {
		t.Fatal("negative timeout should be ignored")
	}

	ctx := WithStatementTimeout(context.Background(), 90*time.Second)
	got, ok := statementTimeoutFromContext(ctx)
	if !ok {
		t.Fatal("expected statement timeout in context")
	}
	if got != 90*time.Second {
		t.Fatalf("statement timeout = %s, want 90s", got)
	}
}

// TestBeginSafeTx_SetsStatementTimeout asserts the transaction issues a
// transaction-local set_config('statement_timeout', …, true) with the timeout
// in milliseconds. This is the regression guard for the bulk endpoint hardening
// (issue #1481).
func TestBeginSafeTx_SetsStatementTimeout(t *testing.T) {
	db, mock := newSQLMockDB(t)

	mock.ExpectBegin()
	// bun interpolates the bound argument into the emitted SQL; the value is a
	// digits-only millisecond count produced by us, so this is injection-safe.
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('statement_timeout', '60000', true)")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	ctx := WithStatementTimeout(context.Background(), 60*time.Second)
	tx, err := BeginSafeTx(ctx, db)
	if err != nil {
		t.Fatalf("BeginSafeTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestBeginSafeTx_NoStatementTimeoutWhenUnset proves ordinary transactions do
// not pay an extra round trip (and are not silently bounded).
func TestBeginSafeTx_NoStatementTimeoutWhenUnset(t *testing.T) {
	db, mock := newSQLMockDB(t)

	mock.ExpectBegin()
	mock.ExpectRollback()

	tx, err := BeginSafeTx(context.Background(), db)
	if err != nil {
		t.Fatalf("BeginSafeTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
