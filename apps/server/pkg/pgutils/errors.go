package pgutils

import (
	"strings"
)

// PostgreSQL error codes
// See: https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	// Class 23 — Integrity Constraint Violation
	CodeUniqueViolation     = "23505"
	CodeForeignKeyViolation = "23503"
	CodeNotNullViolation    = "23502"
	CodeCheckViolation      = "23514"
)

// IsUniqueViolation checks if the error is a PostgreSQL unique constraint violation (23505).
func IsUniqueViolation(err error) bool {
	return containsErrorCode(err, CodeUniqueViolation)
}

// IsForeignKeyViolation checks if the error is a PostgreSQL foreign key violation (23503).
func IsForeignKeyViolation(err error) bool {
	return containsErrorCode(err, CodeForeignKeyViolation)
}

// IsNotNullViolation checks if the error is a PostgreSQL not-null constraint violation (23502).
func IsNotNullViolation(err error) bool {
	return containsErrorCode(err, CodeNotNullViolation)
}

// IsCheckViolation checks if the error is a PostgreSQL check constraint violation (23514).
func IsCheckViolation(err error) bool {
	return containsErrorCode(err, CodeCheckViolation)
}

// containsErrorCode checks if the error message contains a PostgreSQL error code.
func containsErrorCode(err error, code string) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return len(errStr) > 0 && (strings.Contains(errStr, code) || strings.Contains(errStr, "SQLSTATE "+code))
}
