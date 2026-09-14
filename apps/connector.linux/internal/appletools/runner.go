// Package appletools provides Apple Notes and Reminders tools backed by the
// osascript AppleScript runner. Tools are registered at runtime only when
// AppleScript is available (macOS); there are no build tags.
package appletools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrNotAuthorized reports that osascript lacks Automation permission for this
// host (AppleScript error -1743 / "not allowed assistive access").
var ErrNotAuthorized = errors.New("AppleScript not authorized")

// ErrUnavailable reports that osascript is not present on this host (non-macOS).
var ErrUnavailable = errors.New("osascript unavailable")

// DefaultScriptTimeout bounds a single AppleScript run when the caller passes
// a zero timeout. Generous: the first call can launch the target app (Notes /
// Reminders) and a full-text search over a large library easily exceeds a few
// seconds.
const DefaultScriptTimeout = 45 * time.Second

// Runner executes AppleScript snippets. The real implementation shells out to
// /usr/bin/osascript; tests inject a fake so no real AppleScript runs in CI.
type Runner interface {
	// Run executes script and returns its stdout. A timeout <= 0 uses
	// DefaultScriptTimeout. Errors are wrapped and describe the osascript
	// failure; not-authorized runs return ErrNotAuthorized.
	Run(ctx context.Context, script string, timeout time.Duration) (string, error)
	// Available reports whether AppleScript can run on this host (osascript
	// present on PATH).
	Available() bool
}

// OSAppleScriptRunner executes scripts with /usr/bin/osascript.
type OSAppleScriptRunner struct{}

// Available reports whether osascript is on PATH.
func (OSAppleScriptRunner) Available() bool {
	_, err := exec.LookPath("osascript")
	return err == nil
}

// Run executes script with osascript, bounded by timeout.
func (OSAppleScriptRunner) Run(ctx context.Context, script string, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", fmt.Errorf("%w: osascript not found on PATH", ErrUnavailable)
	}
	if timeout <= 0 {
		timeout = DefaultScriptTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := exec.CommandContext(runCtx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("osascript: %w", runCtx.Err())
		}
		if isAuthError(out) {
			return "", fmt.Errorf("%w: %s", ErrNotAuthorized, firstLine(out))
		}
		return "", fmt.Errorf("osascript: %w: %s", err, firstLine(out))
	}
	return string(out), nil
}

// isAuthError matches AppleScript error -1743 from osascript's stderr. The
// error is reported as text ("not allowed assistive access", "not authorized
// to send Apple events", or the "-1743" number) — the process exit status
// cannot carry it, since Unix exit codes are 8-bit.
func isAuthError(out []byte) bool {
	s := string(out)
	return strings.Contains(s, "-1743") ||
		strings.Contains(s, "not allowed assistive access") ||
		strings.Contains(s, "not authorized to send Apple events")
}

// firstLine returns the first non-empty line of b, trimmed.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
