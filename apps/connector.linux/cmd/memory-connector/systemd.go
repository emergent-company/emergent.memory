package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

// systemdUnitName is the user unit managed by install/uninstall.
const systemdUnitName = "memory-connector.service"

// commandRunner abstracts external command execution so install/uninstall can
// be tested with a fake recorder.
type commandRunner interface {
	Run(name string, args ...string) error
}

// execRunner runs commands with os/exec. The child environment is filled in so
// the systemd user bus is reachable from a non-login shell (fresh ssh, cron).
// Output is folded into the error so a failed systemctl call reports why.
type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = systemdChildEnv(os.Environ(), os.Getuid())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return nil
}

// systemdChildEnv returns the environment for systemctl/loginctl child
// processes. When XDG_RUNTIME_DIR or DBUS_SESSION_BUS_ADDRESS are absent (as in
// a non-login shell) it fills them in so the per-user systemd bus is found;
// existing values in base are preserved. It is pure so it can be unit-tested.
func systemdChildEnv(base []string, uid int) []string {
	env := append([]string(nil), base...)
	runtimeDir := envValue(env, "XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/user/" + strconv.Itoa(uid)
		env = setEnv(env, "XDG_RUNTIME_DIR", runtimeDir)
	}
	if envValue(env, "DBUS_SESSION_BUS_ADDRESS") == "" {
		env = setEnv(env, "DBUS_SESSION_BUS_ADDRESS", "unix:path="+runtimeDir+"/bus")
	}
	return env
}

// envValue returns the last value for key in env, or "" when absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

// setEnv replaces key in place or appends it, mirroring exec.Cmd env semantics
// (last value wins).
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// busFailureHint returns an actionable hint when err looks like an unreachable
// systemd user bus, or "" otherwise.
func busFailureHint(err error, uid int) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Failed to connect to bus"),
		strings.Contains(msg, "XDG_RUNTIME_DIR"),
		strings.Contains(msg, "DBUS_SESSION_BUS_ADDRESS"),
		strings.Contains(msg, "No such file or directory"):
		return fmt.Sprintf("\nmemory-connector install: hint: the systemd user bus is unreachable.\n"+
			"memory-connector install: ensure a user session / lingering is active with: loginctl enable-linger %d", uid)
	default:
		return ""
	}
}

// systemdUnitPath returns the systemd user-unit path, honoring
// XDG_CONFIG_HOME ($XDG_CONFIG_HOME/systemd/user/...) and otherwise falling
// back to ~/.config/systemd/user/....
func systemdUnitPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user", systemdUnitName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("cannot determine home directory for the systemd user unit")
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

// renderUnit builds the systemd user-unit body. binPath and configPath are
// expected to be absolute.
func renderUnit(binPath, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Memory connector
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=%s daemon --config %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, binPath, configPath)
}

// runInstall installs and starts the systemd user unit on Linux.
func runInstall(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		_, _ = fmt.Fprintln(stderr, "memory-connector install: only supported on Linux")
		return 1
	}
	return runInstallWith(args, stdout, stderr, execRunner{})
}

// runInstallWith is the platform-independent install implementation, taking an
// injectable command runner for tests.
func runInstallWith(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	binPath := fs.String("bin", "", "path to the memory-connector binary (default: this executable)")
	noLinger := fs.Bool("no-linger", false, "do not run loginctl enable-linger")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	bin := *binPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector install: resolve executable: %v\n", err)
			return 1
		}
		bin = exe
	}
	absBin, err := filepath.Abs(bin)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: resolve binary path: %v\n", err)
		return 1
	}
	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: resolve config path: %v\n", err)
		return 1
	}
	unitPath, err := systemdUnitPath()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: create unit dir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(unitPath, []byte(renderUnit(absBin, absConfig)), 0o644); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: write unit %s: %v\n", unitPath, err)
		return 1
	}

	// Best-effort linger so the user service survives logout and starts at
	// boot. Without it the unit only runs while a login session is active.
	uid := os.Getuid()
	if !*noLinger {
		if err := runner.Run("loginctl", "enable-linger", strconv.Itoa(uid)); err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector install: warning: could not enable linger for user %d: %v\n", uid, err)
			_, _ = fmt.Fprintf(stderr, "memory-connector install: warning: the service will only run while you are logged in; enable it later with: loginctl enable-linger %d\n", uid)
		} else {
			_, _ = fmt.Fprintf(stdout, "memory-connector install: enabled linger for user %d (service runs without an active login)\n", uid)
		}
	}

	if err := runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: %v%s\n", err, busFailureHint(err, uid))
		return 1
	}
	if err := runner.Run("systemctl", "--user", "enable", "--now", systemdUnitName); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector install: %v%s\n", err, busFailureHint(err, uid))
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector install: unit written to %s and enabled\n", unitPath)
	return 0
}

// runUninstall stops, disables, and removes the systemd user unit on Linux.
func runUninstall(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		_, _ = fmt.Fprintln(stderr, "memory-connector uninstall: only supported on Linux")
		return 1
	}
	return runUninstallWith(args, stdout, stderr, execRunner{})
}

// runUninstallWith is the platform-independent uninstall implementation,
// taking an injectable command runner for tests. It is idempotent: a missing
// unit or an already-stopped service is not an error.
func runUninstallWith(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector uninstall: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	_ = configPath // accepted for symmetry with install; not needed to remove the unit

	unitPath, err := systemdUnitPath()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector uninstall: %v\n", err)
		return 1
	}
	// Best-effort: the service may not be loaded or may already be stopped.
	_ = runner.Run("systemctl", "--user", "disable", "--now", systemdUnitName)
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_, _ = fmt.Fprintf(stderr, "memory-connector uninstall: remove unit %s: %v\n", unitPath, err)
		return 1
	}
	if err := runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector uninstall: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector uninstall: unit removed from %s\n", unitPath)
	return 0
}
