package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestParseMemoryErrorShapes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		raw        string
		wantStatus int
		wantText   string
	}{
		{
			name:       "code and message shape",
			status:     http.StatusConflict,
			raw:        `{"error":{"code":"conflict","message":"name taken"}}`,
			wantStatus: http.StatusConflict,
			wantText:   "memory 409 conflict: name taken",
		},
		{
			name:       "error string shape",
			status:     http.StatusNotFound,
			raw:        `{"error":"not_found"}`,
			wantStatus: http.StatusNotFound,
			wantText:   "memory 404: not_found",
		},
		{
			name:       "raw body shape",
			status:     http.StatusInternalServerError,
			raw:        "internal boom",
			wantStatus: http.StatusInternalServerError,
			wantText:   "memory 500: internal boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseMemoryError(tt.status, []byte(tt.raw))
			if got := memoryStatus(err); got != tt.wantStatus {
				t.Fatalf("memoryStatus = %d, want %d", got, tt.wantStatus)
			}
			if got := err.Error(); got != tt.wantText {
				t.Fatalf("Error() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestIsMemoryStatus(t *testing.T) {
	conflict := &memoryHTTPError{Status: http.StatusConflict, Code: "conflict", Message: "dup"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "matching status", err: conflict, want: true},
		{name: "non-matching status", err: conflict, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil error", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := http.StatusConflict
			if tt.name == "non-matching status" {
				status = http.StatusNotFound
			}
			if got := isMemoryStatus(tt.err, status); got != tt.want {
				t.Fatalf("isMemoryStatus(%v, %d) = %v, want %v", tt.err, status, got, tt.want)
			}
		})
	}
}

func TestIsMemoryNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typed 404",
			err:  &memoryHTTPError{Status: http.StatusNotFound, Message: "missing"},
			want: true,
		},
		{
			name: "plain not found text",
			err:  errors.New("memory 404: not found"),
			want: true,
		},
		{
			name: "typed 500",
			err:  &memoryHTTPError{Status: http.StatusInternalServerError, Message: "boom"},
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMemoryNotFound(tt.err); got != tt.want {
				t.Fatalf("isMemoryNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
