package adk

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIsRetryableProviderStatus(t *testing.T) {
	tests := map[int]bool{
		200: false,
		400: false,
		401: false,
		403: false,
		404: false,
		422: false,
		429: true,
		500: true,
		502: true,
		503: true,
		504: true,
	}
	for code, want := range tests {
		if got := isRetryableProviderStatus(code); got != want {
			t.Errorf("isRetryableProviderStatus(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"empty", "", 0, false},
		{"garbage", "soon", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"seconds with spaces", " 7 ", 7 * time.Second, true},
		{"zero", "0", 0, true},
		{"negative is zero", "-3", 0, true},
		{"http date", now.Add(12 * time.Second).Format(http.TimeFormat), 12 * time.Second, true},
		{"past http date is zero", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value, now)
			if ok != tt.wantOK {
				t.Fatalf("parseRetryAfter(%q) ok = %v, want %v", tt.value, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestProviderRetryDelay(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		{"header wins", 0, "2", 2 * time.Second},
		{"header is capped", 0, "3600", providerRetryMax},
		{"backoff first attempt", 0, "", providerRetryBase},
		{"backoff doubles", 1, "", 2 * providerRetryBase},
		{"unparseable header falls back to backoff", 1, "later", 2 * providerRetryBase},
		{"backoff is capped", 5, "", providerRetryMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerRetryDelay(tt.attempt, tt.retryAfter); got != tt.want {
				t.Errorf("providerRetryDelay(%d, %q) = %v, want %v", tt.attempt, tt.retryAfter, got, tt.want)
			}
		})
	}
}

func TestSleepWithContext(t *testing.T) {
	if !sleepWithContext(context.Background(), 0) {
		t.Error("a zero wait should complete immediately")
	}
	if !sleepWithContext(context.Background(), time.Millisecond) {
		t.Error("a short wait should complete")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepWithContext(ctx, time.Hour) {
		t.Error("a cancelled context should end the wait")
	}
}
