package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// fakeRunner records Run calls and optionally fails selected calls.
type fakeRunner struct {
	calls [][]string
	fail  func(name string, args []string) error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.fail != nil {
		return f.fail(name, args)
	}
	return nil
}

func TestRenderUnitGolden(t *testing.T) {
	got := renderUnit("/usr/local/bin/memory-connector", "/home/u/.config/memory-connector.yml")
	want := `[Unit]
Description=Memory connector
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/memory-connector daemon --config /home/u/.config/memory-connector.yml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`
	if got != want {
		t.Errorf("renderUnit() =\n%s\nwant:\n%s", got, want)
	}
}

func TestSystemdUnitPathXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := systemdUnitPath()
	if err != nil {
		t.Fatalf("systemdUnitPath: %v", err)
	}
	want := filepath.Join(dir, "systemd", "user", systemdUnitName)
	if got != want {
		t.Errorf("systemdUnitPath() = %q, want %q", got, want)
	}
}

func TestSystemdUnitPathHomeFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", dir)
	got, err := systemdUnitPath()
	if err != nil {
		t.Fatalf("systemdUnitPath: %v", err)
	}
	want := filepath.Join(dir, ".config", "systemd", "user", systemdUnitName)
	if got != want {
		t.Errorf("systemdUnitPath() = %q, want %q", got, want)
	}
}

func TestInstallWritesUnitAndRunsSystemctl(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "config.yml")
	binPath := filepath.Join(dir, "memory-connector")
	runner := &fakeRunner{}

	code, _, stderr := runInstallWithCapture(t, []string{"--config", cfgPath, "--bin", binPath}, runner)
	if code != 0 {
		t.Fatalf("runInstallWith code = %d, want 0 (stderr: %s)", code, stderr)
	}

	unitPath := filepath.Join(dir, "systemd", "user", systemdUnitName)
	got, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if string(got) != renderUnit(binPath, cfgPath) {
		t.Errorf("unit contents =\n%s\nwant:\n%s", got, renderUnit(binPath, cfgPath))
	}

	wantCalls := [][]string{
		{"loginctl", "enable-linger", uidArg()},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdUnitName},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Errorf("systemctl calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestInstallNoLingerSkipsLinger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	runner := &fakeRunner{}

	code, stdout, stderr := runInstallWithCapture(t, []string{
		"--config", filepath.Join(dir, "config.yml"),
		"--bin", filepath.Join(dir, "memory-connector"),
		"--no-linger",
	}, runner)
	if code != 0 {
		t.Fatalf("runInstallWith code = %d, want 0 (stderr: %s)", code, stderr)
	}
	wantCalls := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdUnitName},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", runner.calls, wantCalls)
	}
	if strings.Contains(stdout, "linger") {
		t.Errorf("stdout = %q, want no linger note with --no-linger", stdout)
	}
}

func TestInstallLingerFailureNonFatal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	runner := &fakeRunner{fail: func(name string, args []string) error {
		if name == "loginctl" {
			return errors.New("Failed to connect to bus: No such file or directory")
		}
		return nil
	}}

	code, _, stderr := runInstallWithCapture(t, []string{
		"--config", filepath.Join(dir, "config.yml"),
		"--bin", filepath.Join(dir, "memory-connector"),
	}, runner)
	if code != 0 {
		t.Fatalf("runInstallWith code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "warning: could not enable linger") {
		t.Errorf("stderr = %q, want a linger warning", stderr)
	}
	wantCalls := [][]string{
		{"loginctl", "enable-linger", uidArg()},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdUnitName},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestSystemdChildEnvFillsDefaults(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/u"}
	got := systemdChildEnv(base, 1000)
	want := map[string]string{
		"XDG_RUNTIME_DIR":          "/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus",
	}
	for key, value := range want {
		if v := envValue(got, key); v != value {
			t.Errorf("envValue(%q) = %q, want %q", key, v, value)
		}
	}
	if envValue(got, "PATH") != "/usr/bin" {
		t.Errorf("base env not preserved: %v", got)
	}
}

func TestSystemdChildEnvPreservesExisting(t *testing.T) {
	base := []string{
		"XDG_RUNTIME_DIR=/custom/run",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/custom/bus",
	}
	got := systemdChildEnv(base, 1000)
	if envValue(got, "XDG_RUNTIME_DIR") != "/custom/run" {
		t.Errorf("XDG_RUNTIME_DIR = %q, want existing value", envValue(got, "XDG_RUNTIME_DIR"))
	}
	if envValue(got, "DBUS_SESSION_BUS_ADDRESS") != "unix:path=/custom/bus" {
		t.Errorf("DBUS_SESSION_BUS_ADDRESS = %q, want existing value", envValue(got, "DBUS_SESSION_BUS_ADDRESS"))
	}
}

func TestSystemdChildEnvDerivesDBusFromExistingRuntimeDir(t *testing.T) {
	base := []string{"XDG_RUNTIME_DIR=/run/user/42"}
	got := systemdChildEnv(base, 1000)
	if envValue(got, "DBUS_SESSION_BUS_ADDRESS") != "unix:path=/run/user/42/bus" {
		t.Errorf("DBUS_SESSION_BUS_ADDRESS = %q, want unix:path=/run/user/42/bus", envValue(got, "DBUS_SESSION_BUS_ADDRESS"))
	}
}

// uidArg returns the current uid as the string install passes to loginctl.
func uidArg() string { return strconv.Itoa(os.Getuid()) }

func TestInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	args := []string{"--config", filepath.Join(dir, "config.yml"), "--bin", filepath.Join(dir, "memory-connector")}
	runner := &fakeRunner{}

	for i := 0; i < 2; i++ {
		code, _, stderr := runInstallWithCapture(t, args, runner)
		if code != 0 {
			t.Fatalf("runInstallWith run %d code = %d, want 0 (stderr: %s)", i+1, code, stderr)
		}
	}
	wantCalls := [][]string{
		{"loginctl", "enable-linger", uidArg()},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdUnitName},
		{"loginctl", "enable-linger", uidArg()},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdUnitName},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Errorf("systemctl calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestInstallPropagatesSystemctlFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	runner := &fakeRunner{fail: func(string, []string) error { return errors.New("systemctl boom") }}
	code, _, stderr := runInstallWithCapture(t,
		[]string{"--config", filepath.Join(dir, "config.yml"), "--bin", filepath.Join(dir, "memory-connector")}, runner)
	if code != 1 {
		t.Fatalf("runInstallWith code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "memory-connector install:") {
		t.Errorf("stderr = %q, want an install-prefixed error", stderr)
	}
}

func TestInstallFlagErrors(t *testing.T) {
	runner := &fakeRunner{}
	for _, args := range [][]string{{"--bogus"}, {"extra"}} {
		code, _, _ := runInstallWithCapture(t, args, runner)
		if code != 2 {
			t.Errorf("runInstallWith(%v) code = %d, want 2", args, code)
		}
	}
}

func TestUninstallRemovesUnitAndRunsSystemctl(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	unitPath := filepath.Join(dir, "systemd", "user", systemdUnitName)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		t.Fatalf("mkdir unit dir: %v", err)
	}
	if err := os.WriteFile(unitPath, []byte("unit"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	runner := &fakeRunner{}

	code, _, stderr := runUninstallWithCapture(t, []string{"--config", filepath.Join(dir, "config.yml")}, runner)
	if code != 0 {
		t.Fatalf("runUninstallWith code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unit file still present after uninstall (stat err = %v)", err)
	}

	wantCalls := [][]string{
		{"systemctl", "--user", "disable", "--now", systemdUnitName},
		{"systemctl", "--user", "daemon-reload"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Errorf("systemctl calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestUninstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Unit is already absent and systemctl disable fails, as on a second run.
	runner := &fakeRunner{fail: func(name string, args []string) error {
		if name == "systemctl" && len(args) > 0 && args[0] == "--user" && len(args) > 1 && args[1] == "disable" {
			return errors.New("Unit memory-connector.service not loaded.")
		}
		return nil
	}}
	args := []string{"--config", filepath.Join(dir, "config.yml")}

	for i := 0; i < 2; i++ {
		code, _, stderr := runUninstallWithCapture(t, args, runner)
		if code != 0 {
			t.Fatalf("runUninstallWith run %d code = %d, want 0 (stderr: %s)", i+1, code, stderr)
		}
	}
}

func TestUninstallFlagErrors(t *testing.T) {
	runner := &fakeRunner{}
	for _, args := range [][]string{{"--bogus"}, {"extra"}} {
		code, _, _ := runUninstallWithCapture(t, args, runner)
		if code != 2 {
			t.Errorf("runUninstallWith(%v) code = %d, want 2", args, code)
		}
	}
}

func runInstallWithCapture(t *testing.T, args []string, runner *fakeRunner) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = runInstallWith(args, &out, &errBuf, runner)
	return code, out.String(), errBuf.String()
}

func runUninstallWithCapture(t *testing.T, args []string, runner *fakeRunner) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = runUninstallWith(args, &out, &errBuf, runner)
	return code, out.String(), errBuf.String()
}
