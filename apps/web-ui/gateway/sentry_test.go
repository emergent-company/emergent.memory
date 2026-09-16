package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestNormalizePathTemplate(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "uuid segment",
			path: "/api/chat/1d47dbf5-c4fd-4d28-8e3a-f9fa122d56bb/history",
			want: "/api/chat/{id}/history",
		},
		{
			name: "long hex segment",
			path: "/api/objects/0123456789abcdef0123",
			want: "/api/objects/{id}",
		},
		{
			name: "numeric segment",
			path: "/api/backups/42/download",
			want: "/api/backups/{id}/download",
		},
		{
			name: "mixed segments",
			path: "/api/chat/1d47dbf5-c4fd-4d28-8e3a-f9fa122d56bb/messages/7",
			want: "/api/chat/{id}/messages/{id}",
		},
		{
			name: "version segment with a letter is kept",
			path: "/api/v2",
			want: "/api/v2",
		},
		{
			name: "version segment path with a letter is kept",
			path: "/api/v2/objects",
			want: "/api/v2/objects",
		},
		{
			name: "all-digit segment is collapsed",
			path: "/api/2",
			want: "/api/{id}",
		},
		{
			name: "already templated",
			path: "/api/chat/{id}/history",
			want: "/api/chat/{id}/history",
		},
		{
			name: "plain path",
			path: "/api/health",
			want: "/api/health",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "root path",
			path: "/",
			want: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePathTemplate(tt.path); got != tt.want {
				t.Fatalf("normalizePathTemplate(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMemoryErrorDisposition(t *testing.T) {
	tests := []struct {
		status      int
		wantCapture bool
		wantLevel   sentry.Level
	}{
		{status: 0, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusBadRequest, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusNotFound, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusConflict, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusUnprocessableEntity, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusTooManyRequests, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusUnauthorized, wantCapture: true, wantLevel: sentry.LevelWarning},
		{status: http.StatusForbidden, wantCapture: true, wantLevel: sentry.LevelWarning},
		{status: http.StatusInternalServerError, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusBadGateway, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusServiceUnavailable, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusGatewayTimeout, wantCapture: true, wantLevel: sentry.LevelError},
	}

	for _, tt := range tests {
		name := http.StatusText(tt.status)
		if name == "" {
			name = "transport error"
		}
		t.Run(name, func(t *testing.T) {
			capture, level := memoryErrorDisposition(tt.status)
			if capture != tt.wantCapture {
				t.Fatalf("memoryErrorDisposition(%d) capture = %v, want %v", tt.status, capture, tt.wantCapture)
			}
			if level != tt.wantLevel {
				t.Fatalf("memoryErrorDisposition(%d) level = %v, want %v", tt.status, level, tt.wantLevel)
			}
		})
	}
}

// TestCaptureHelpersNoClientWithNoDSN is a smoke test: with no Sentry client
// configured both helpers must be safe no-ops (and never panic).
func TestCaptureHelpersNoClientWithNoDSN(t *testing.T) {
	sentry.CurrentHub().BindClient(nil)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	captureError(errors.New("boom"))
	captureMemoryError(http.MethodGet, "/api/chat/123/history", http.StatusInternalServerError, errors.New("boom"))
	capturePollFailure(errors.New("boom"))
}
