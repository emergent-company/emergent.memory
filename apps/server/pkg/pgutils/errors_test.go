package pgutils

import (
	"errors"
	"fmt"
	"testing"
)

func TestContainsErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			code: CodeUniqueViolation,
			want: false,
		},
		{
			name: "error contains code directly",
			err:  errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"),
			code: CodeUniqueViolation,
			want: true,
		},
		{
			name: "error contains SQLSTATE prefix",
			err:  errors.New("pq: SQLSTATE 23505 duplicate key"),
			code: CodeUniqueViolation,
			want: true,
		},
		{
			name: "error does not contain code",
			err:  errors.New("some other error"),
			code: CodeUniqueViolation,
			want: false,
		},
		{
			name: "empty error message",
			err:  errors.New(""),
			code: CodeUniqueViolation,
			want: false,
		},
		{
			name: "code in middle of message",
			err:  errors.New("Error 23505 occurred"),
			code: CodeUniqueViolation,
			want: true,
		},
		{
			name: "different code in message",
			err:  errors.New("SQLSTATE 23503 foreign key violation"),
			code: CodeUniqueViolation,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsErrorCode(tt.err, tt.code)
			if got != tt.want {
				t.Errorf("containsErrorCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "unique violation with SQLSTATE",
			err:  errors.New("ERROR: duplicate key value (SQLSTATE 23505)"),
			want: true,
		},
		{
			name: "unique violation code only",
			err:  errors.New("constraint violation 23505"),
			want: true,
		},
		{
			name: "foreign key violation - not unique",
			err:  errors.New("SQLSTATE 23503"),
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "postgres driver error format",
			err:  fmt.Errorf("pq: duplicate key value violates unique constraint \"users_email_key\" (SQLSTATE 23505)"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUniqueViolation(tt.err)
			if got != tt.want {
				t.Errorf("IsUniqueViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "foreign key violation with SQLSTATE",
			err:  errors.New("ERROR: insert or update violates foreign key constraint (SQLSTATE 23503)"),
			want: true,
		},
		{
			name: "foreign key violation code only",
			err:  errors.New("violation 23503"),
			want: true,
		},
		{
			name: "unique violation - not foreign key",
			err:  errors.New("SQLSTATE 23505"),
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("timeout"),
			want: false,
		},
		{
			name: "postgres driver error format",
			err:  fmt.Errorf("pq: insert or update on table \"orders\" violates foreign key constraint \"orders_user_id_fkey\" (SQLSTATE 23503)"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsForeignKeyViolation(tt.err)
			if got != tt.want {
				t.Errorf("IsForeignKeyViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNotNullViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not null violation with SQLSTATE",
			err:  errors.New("ERROR: null value in column violates not-null constraint (SQLSTATE 23502)"),
			want: true,
		},
		{
			name: "not null violation code only",
			err:  errors.New("23502"),
			want: true,
		},
		{
			name: "unique violation - not null",
			err:  errors.New("SQLSTATE 23505"),
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("network error"),
			want: false,
		},
		{
			name: "postgres driver error format",
			err:  fmt.Errorf("pq: null value in column \"email\" of relation \"users\" violates not-null constraint (SQLSTATE 23502)"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNotNullViolation(tt.err)
			if got != tt.want {
				t.Errorf("IsNotNullViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCheckViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "check violation with SQLSTATE",
			err:  errors.New("ERROR: new row violates check constraint (SQLSTATE 23514)"),
			want: true,
		},
		{
			name: "check violation code only",
			err:  errors.New("23514"),
			want: true,
		},
		{
			name: "unique violation - not check",
			err:  errors.New("SQLSTATE 23505"),
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("disk full"),
			want: false,
		},
		{
			name: "postgres driver error format",
			err:  fmt.Errorf("pq: new row for relation \"products\" violates check constraint \"products_price_check\" (SQLSTATE 23514)"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCheckViolation(tt.err)
			if got != tt.want {
				t.Errorf("IsCheckViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorCodeConstants(t *testing.T) {
	// Verify the constants match PostgreSQL documentation
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"CodeUniqueViolation", CodeUniqueViolation, "23505"},
		{"CodeForeignKeyViolation", CodeForeignKeyViolation, "23503"},
		{"CodeNotNullViolation", CodeNotNullViolation, "23502"},
		{"CodeCheckViolation", CodeCheckViolation, "23514"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}
