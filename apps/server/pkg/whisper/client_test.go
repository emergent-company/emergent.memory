package whisper

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func newTestClient() *Client {
	return &Client{
		timeout:                 time.Minute,
		enabled:                 false,
		language:                "",
		maxFileSizeMB:           500,
		largeFileThresholdBytes: 50 * 1024 * 1024,
		audioBytesPerSecond:     16000,
		timeoutSafetyFactor:     2.0,
	}
}

func TestNewClientDisabledDefaults(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewClient(&config.Config{}, log)
	if c.IsEnabled() {
		t.Fatal("zero config must produce a disabled client")
	}
}

func TestIsEnabled(t *testing.T) {
	c := newTestClient()
	if c.IsEnabled() {
		t.Fatal("disabled client must report IsEnabled() == false")
	}
	c.enabled = true
	if !c.IsEnabled() {
		t.Fatal("enabled client must report IsEnabled() == true")
	}
}

func TestMaxFileSizeBytes(t *testing.T) {
	c := newTestClient()
	if got, want := c.MaxFileSizeBytes(), int64(500*1024*1024); got != want {
		t.Fatalf("MaxFileSizeBytes() = %d, want %d", got, want)
	}
}

func TestLargeFileThreshold(t *testing.T) {
	c := newTestClient()
	threshold := int64(50 * 1024 * 1024)
	if got := c.LargeFileThresholdBytes(); got != threshold {
		t.Fatalf("LargeFileThresholdBytes() = %d, want %d", got, threshold)
	}
	if c.IsLargeFile(threshold - 1) {
		t.Fatal("size below threshold must not be large")
	}
	if !c.IsLargeFile(threshold) {
		t.Fatal("size at threshold must be large")
	}
}

func TestTimeoutForSize(t *testing.T) {
	c := newTestClient()

	if got := c.TimeoutForSize(0); got != time.Minute {
		t.Fatalf("TimeoutForSize(0) = %s, want base timeout %s", got, time.Minute)
	}

	// 1 byte -> estimate below base timeout, so base timeout is returned.
	if got := c.TimeoutForSize(1); got != time.Minute {
		t.Fatalf("TimeoutForSize(1) = %s, want base timeout %s", got, time.Minute)
	}

	// 1 MiB -> estimate (1MiB/16000 * 2.0) = ~131s exceeds base timeout.
	size := int64(1024 * 1024)
	want := time.Duration(float64(size) / 16000 * 2.0 * float64(time.Second))
	if got := c.TimeoutForSize(size); got != want {
		t.Fatalf("TimeoutForSize(1MiB) = %s, want %s", got, want)
	}

	// No bitrate configured -> always base timeout.
	noRate := *c
	noRate.audioBytesPerSecond = 0
	if got := noRate.TimeoutForSize(size); got != time.Minute {
		t.Fatalf("TimeoutForSize with no bitrate = %s, want base timeout %s", got, time.Minute)
	}
}

func TestHealthCheckDisabled(t *testing.T) {
	c := newTestClient()
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("disabled HealthCheck must return nil, got %v", err)
	}
}

func TestTranscribeDisabled(t *testing.T) {
	c := newTestClient()
	if _, err := c.Transcribe(context.Background(), []byte("x"), "a.wav", "audio/wav", "", 0); err == nil {
		t.Fatal("disabled Transcribe must return an error")
	}
}
