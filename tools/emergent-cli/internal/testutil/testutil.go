// Package testutil provides testing utilities for emergent-cli.
package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// CreateTempConfig creates a temporary config file with the given content.
// The file is automatically cleaned up when the test finishes.
func CreateTempConfig(t *testing.T, content string) string {
	t.Helper()

	// Create temp file
	tmpFile, err := os.CreateTemp("", "emergent-config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp config: %v", err)
	}

	path := tmpFile.Name()

	// Write content
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(path)
		t.Fatalf("failed to write config content: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(path)
		t.Fatalf("failed to close temp config: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		os.Remove(path)
	})

	return path
}

// SetEnv sets an environment variable for the duration of the test.
// The original value is restored when the test finishes.
func SetEnv(t *testing.T, key, value string) {
	t.Helper()

	original, exists := os.LookupEnv(key)

	// Set new value
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("failed to set env var %s: %v", key, err)
	}

	// Register cleanup to restore original
	t.Cleanup(func() {
		if exists {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	})
}

// OutputCapture captures stdout and stderr for testing.
type OutputCapture struct {
	originalStdout *os.File
	originalStderr *os.File
	stdoutReader   *os.File
	stderrReader   *os.File
	stdoutWriter   *os.File
	stderrWriter   *os.File
	stdoutBuf      *bytes.Buffer
	stderrBuf      *bytes.Buffer
	wg             sync.WaitGroup
}

// CaptureOutput starts capturing stdout and stderr.
// Call Restore() when done to restore original stdout/stderr.
func CaptureOutput() *OutputCapture {
	capture := &OutputCapture{
		originalStdout: os.Stdout,
		originalStderr: os.Stderr,
		stdoutBuf:      &bytes.Buffer{},
		stderrBuf:      &bytes.Buffer{},
	}

	// Create pipes for stdout
	capture.stdoutReader, capture.stdoutWriter, _ = os.Pipe()

	// Create pipes for stderr
	capture.stderrReader, capture.stderrWriter, _ = os.Pipe()

	// Replace stdout/stderr
	os.Stdout = capture.stdoutWriter
	os.Stderr = capture.stderrWriter

	// Start goroutines to read from pipes
	capture.wg.Add(2)
	go func() {
		defer capture.wg.Done()
		io.Copy(capture.stdoutBuf, capture.stdoutReader)
	}()
	go func() {
		defer capture.wg.Done()
		io.Copy(capture.stderrBuf, capture.stderrReader)
	}()

	return capture
}

// Read returns the captured stdout and stderr content.
func (c *OutputCapture) Read() (stdout, stderr string, err error) {
	// Close writers to signal EOF to readers
	c.stdoutWriter.Close()
	c.stderrWriter.Close()

	// Wait for goroutines to finish reading
	c.wg.Wait()

	// Give a tiny bit of time for any buffered writes
	time.Sleep(10 * time.Millisecond)

	stdout = c.stdoutBuf.String()
	stderr = c.stderrBuf.String()

	return stdout, stderr, nil
}

// Restore restores stdout and stderr to their original values.
func (c *OutputCapture) Restore() {
	if c.stdoutWriter != nil {
		c.stdoutWriter.Close()
	}
	if c.stderrWriter != nil {
		c.stderrWriter.Close()
	}
	if c.stdoutReader != nil {
		c.stdoutReader.Close()
	}
	if c.stderrReader != nil {
		c.stderrReader.Close()
	}

	os.Stdout = c.originalStdout
	os.Stderr = c.originalStderr
}

// CreateTestConfig generates a YAML config file with default settings.
// The file is automatically cleaned up when the test finishes.
func CreateTestConfig(serverURL, email string) string {
	content := fmt.Sprintf(`server_url: %s
email: %s
output_format: table
timeout: 30s
`, serverURL, email)

	tmpFile, err := os.CreateTemp("", "emergent-config-*.yaml")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp config: %v", err))
	}

	path := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(path)
		panic(fmt.Sprintf("failed to write config: %v", err))
	}

	tmpFile.Close()

	return path
}

// WithConfigFile creates a temporary config file and sets the EMERGENT_CONFIG env var.
// Both the file and env var are cleaned up when the test finishes.
func WithConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := CreateTempConfig(t, content)

	SetEnv(t, "EMERGENT_CONFIG", path)

	return path
}
