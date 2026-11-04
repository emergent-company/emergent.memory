package appletools

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeRunner implements Runner for tests; it never touches osascript.
type fakeRunner struct {
	available bool
	out       string
	err       error

	gotScripts  []string
	gotTimeouts []time.Duration
}

func (f *fakeRunner) Available() bool { return f.available }

func (f *fakeRunner) Run(_ context.Context, script string, timeout time.Duration) (string, error) {
	f.gotScripts = append(f.gotScripts, script)
	f.gotTimeouts = append(f.gotTimeouts, timeout)
	return f.out, f.err
}

func TestOSAppleScriptRunnerAvailableMatchesLookPath(t *testing.T) {
	_, lookErr := exec.LookPath("osascript")
	want := lookErr == nil
	if got := (OSAppleScriptRunner{}).Available(); got != want {
		t.Errorf("Available() = %v, want %v (osascript present?)", got, want)
	}
}

func TestOSAppleScriptRunnerUnavailableRun(t *testing.T) {
	if (OSAppleScriptRunner{}).Available() {
		t.Skip("osascript present on this host; cannot exercise unavailable path")
	}
	_, err := (OSAppleScriptRunner{}).Run(context.Background(), "return 1", 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Run() error = %v, want ErrUnavailable", err)
	}
}

func TestIsAuthError(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{name: "stderr -1743", out: "execution error: Not authorized (-1743)", want: true},
		{name: "not allowed assistive access", out: "osascript is not allowed assistive access", want: true},
		{name: "not authorized to send Apple events", out: "not authorized to send Apple events to Notes", want: true},
		{name: "unrelated script error", out: "execution error: Can't get folder \"x\" (-1728)", want: false},
		{name: "clean output", out: "[{\"name\":\"n\"}]", want: false},
		{name: "empty output", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAuthError([]byte(tc.out)); got != tc.want {
				t.Errorf("isAuthError(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

func TestDefaultScriptTimeout(t *testing.T) {
	if DefaultScriptTimeout != 45*time.Second {
		t.Errorf("DefaultScriptTimeout = %v, want 45s", DefaultScriptTimeout)
	}
}

func TestMapRunnerError(t *testing.T) {
	auth := mapRunnerError(ErrNotAuthorized)
	if !errors.Is(auth, ErrNotAuthorized) {
		t.Error("mapped auth error must wrap ErrNotAuthorized")
	}
	if !strings.Contains(auth.Error(), "System Settings") || !strings.Contains(auth.Error(), "Privacy & Security") {
		t.Errorf("mapped auth error %q should advise granting Automation permission", auth)
	}

	unavail := mapRunnerError(ErrUnavailable)
	if !errors.Is(unavail, ErrUnavailable) {
		t.Error("mapped unavailable error must wrap ErrUnavailable")
	}
	if !strings.Contains(unavail.Error(), "macOS") {
		t.Errorf("mapped unavailable error %q should mention macOS", unavail)
	}

	other := errors.New("osascript: exit status 1: boom")
	if mapped := mapRunnerError(other); mapped != other {
		t.Errorf("mapRunnerError(other) = %v, want passthrough", mapped)
	}
}
