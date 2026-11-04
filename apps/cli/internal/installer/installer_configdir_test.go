package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// recordingOutput captures what a best-effort tighten says, so a warning (or a
// disclosure of a mode change) can be asserted instead of only the exit status.
type recordingOutput struct {
	infos []string
	warns []string
}

func (o *recordingOutput) Info(format string, args ...interface{}) {
	o.infos = append(o.infos, fmt.Sprintf(format, args...))
}
func (o *recordingOutput) Success(string, ...interface{}) {}
func (o *recordingOutput) Error(string, ...interface{})   {}
func (o *recordingOutput) Step(string, ...interface{})    {}

func (o *recordingOutput) Warn(format string, args ...interface{}) {
	o.warns = append(o.warns, fmt.Sprintf(format, args...))
}

// fakeInstall creates the minimal tree IsInstalled accepts, with config/ at
// mode, so directory behaviour can be exercised without Docker.
func fakeInstall(t *testing.T, configDirMode os.FileMode) string {
	t.Helper()

	installDir := t.TempDir()
	dockerDir := filepath.Join(installDir, "docker")
	if err := os.MkdirAll(dockerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := GetDockerComposeTemplateWithVersion("latest")
	if err := os.WriteFile(filepath.Join(dockerDir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(installDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDir, configDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env.local"), []byte("STANDALONE_API_KEY=fake\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return installDir
}

// blockDocker puts a failing `docker` first on PATH so Upgrade returns at the
// pull step instead of touching a real daemon. The config-directory work
// happens before any container is touched, which is what these tests assert.
func blockDocker(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	script := filepath.Join(binDir, "docker")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
}

// A read-only or network filesystem cannot express a directory mode, and we may
// not own a root-installed one. Neither is a reason to fail an install: the
// credential file inside is already 0600, so the directory mode exposes a
// filename at most. EACCES is different — it means we lack permission on our own
// directory, which is worth surfacing (#2119 item 4).
func TestTightenTolerated(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "EPERM is tolerated (not our directory)", err: syscall.EPERM, want: true},
		{name: "EROFS is tolerated (read-only filesystem)", err: syscall.EROFS, want: true},
		{name: "ENOTSUP is tolerated (filesystem cannot express the mode)", err: syscall.ENOTSUP, want: true},
		{name: "wrapped EPERM is tolerated", err: fmt.Errorf("chmod: %w", syscall.EPERM), want: true},
		{name: "EACCES is surfaced (we lack permission on our own directory)", err: syscall.EACCES, want: false},
		{name: "EIO is surfaced (real I/O failure)", err: syscall.EIO, want: false},
		{name: "ENOENT is surfaced", err: syscall.ENOENT, want: false},
		{name: "plain error is surfaced", err: errors.New("boom"), want: false},
		{name: "nil is not a tolerable failure", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tightenTolerated(tt.err); got != tt.want {
				t.Errorf("tightenTolerated(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ENOTSUP and EOPNOTSUPP are the same value on Linux, which is why the predicate
// names only one of them. Asserted unconditionally rather than behind a
// platform skip: on a platform where the constants split, a failure here is the
// correct signal that tightenTolerated needs the second constant.
func TestTightenToleratedCoversEOPNOTSUPP(t *testing.T) {
	if !tightenTolerated(syscall.EOPNOTSUPP) {
		t.Error("EOPNOTSUPP must be tolerated; it is ENOTSUP on Linux")
	}
}

// The predicate is what the chmod error path branches on. Swapping chmodPath
// exercises it without needing a real read-only mount.
func TestTightenExistingDirChmodFailures(t *testing.T) {
	tests := []struct {
		name    string
		chmodUp error
		wantErr bool
	}{
		{name: "EPERM leaves the directory alone", chmodUp: syscall.EPERM},
		{name: "EROFS leaves the directory alone", chmodUp: syscall.EROFS},
		{name: "ENOTSUP leaves the directory alone", chmodUp: syscall.ENOTSUP},
		{name: "EACCES fails", chmodUp: syscall.EACCES, wantErr: true},
		{name: "EIO fails", chmodUp: syscall.EIO, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}

			restore := swapChmod(func(string, os.FileMode) error { return tt.chmodUp })
			defer restore()

			err := tightenExistingDir(dir, 0o700)
			if tt.wantErr && err == nil {
				t.Fatalf("tightenExistingDir with %v: expected an error, got nil", tt.chmodUp)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("tightenExistingDir with %v: unexpected error %v", tt.chmodUp, err)
			}
		})
	}
}

// An already-tight directory is left untouched: no chmod call at all, which is
// what makes the Upgrade path idempotent.
func TestTightenExistingDirSkipsAlreadyTight(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	calls := 0
	restore := swapChmod(func(string, os.FileMode) error {
		calls++
		return nil
	})
	defer restore()

	if err := tightenExistingDir(dir, 0o700); err != nil {
		t.Fatalf("tightenExistingDir failed: %v", err)
	}
	if calls != 0 {
		t.Errorf("chmod called %d times on an already-tight directory, want 0", calls)
	}
}

// A missing directory has nothing to tighten. Upgrade calls this on installs
// that may predate config/, and must not fail over it.
func TestTightenExistingDirMissingIsNoop(t *testing.T) {
	if err := tightenExistingDir(filepath.Join(t.TempDir(), "config"), 0o700); err != nil {
		t.Fatalf("tightenExistingDir on a missing directory: unexpected error %v", err)
	}
}

// Upgrade used to leave a pre-existing install at 0755 forever: Install bails
// out when an install exists, so only CreateDirectories ever tightened config/
// (#2119 item 3).
func TestUpgradeTightensConfigDirectory(t *testing.T) {
	installDir := fakeInstall(t, 0o755)
	blockDocker(t)

	inst := New(Config{InstallDir: installDir})
	// Upgrade fails at the blocked docker pull; the directory work happened
	// first, which is the behaviour under test.
	err := inst.Upgrade("v0.0.1")
	if err != nil && strings.Contains(err.Error(), "permission") {
		t.Fatalf("upgrade failed on the directory tighten: %v", err)
	}

	info, statErr := os.Stat(filepath.Join(installDir, "config"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("config directory mode = %o after upgrade, want 700", got)
	}
}

// A second upgrade over an already-tightened directory is a no-op: it neither
// fails nor warns.
func TestUpgradeTightenIsIdempotent(t *testing.T) {
	installDir := fakeInstall(t, 0o755)
	blockDocker(t)

	inst := New(Config{InstallDir: installDir})
	out := &recordingOutput{}
	inst.SetOutput(out)

	for run := 1; run <= 2; run++ {
		_ = inst.Upgrade("v0.0.1")

		info, err := os.Stat(filepath.Join(installDir, "config"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("run %d: config directory mode = %o, want 700", run, got)
		}
	}

	for _, warn := range out.warns {
		if strings.Contains(warn, "tighten") {
			t.Errorf("second run warned about tightening an already-tight directory: %s", warn)
		}
	}
}

// The tighten is unconditional for any --dir, unlike the default-path rule for
// .env.local, so an explicit installation directory is narrowed with a line
// saying so rather than silently (#2119 item 3).
func TestUpgradeDisclosesConfigDirTighten(t *testing.T) {
	installDir := fakeInstall(t, 0o755)
	blockDocker(t)

	inst := New(Config{InstallDir: installDir})
	out := &recordingOutput{}
	inst.SetOutput(out)

	_ = inst.Upgrade("v0.0.1")

	configDir := filepath.Join(installDir, "config")
	disclosed := 0
	for _, info := range out.infos {
		if strings.Contains(info, "Tightened permissions on "+configDir) {
			disclosed++
		}
	}
	if disclosed != 1 {
		t.Fatalf("upgrade disclosed the directory tighten %d times, want 1 (infos: %v)", disclosed, out.infos)
	}

	// Already at 0700: the second run changes nothing, so it says nothing.
	out.infos = nil
	_ = inst.Upgrade("v0.0.1")
	for _, info := range out.infos {
		if strings.Contains(info, "Tightened permissions on") {
			t.Errorf("second upgrade disclosed a tighten that did not happen: %s", info)
		}
	}
}

// A filesystem that cannot express the mode must not fail an otherwise fine
// upgrade: the tighten warns at most (#2119 items 3 and 4).
func TestUpgradeToleratesUntightenableConfigDirectory(t *testing.T) {
	tests := []struct {
		name     string
		chmodUp  error
		wantWarn bool
	}{
		{name: "EROFS is tolerated silently", chmodUp: syscall.EROFS},
		{name: "EPERM is tolerated silently", chmodUp: syscall.EPERM},
		{name: "EACCES is surfaced as a warning", chmodUp: syscall.EACCES, wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installDir := fakeInstall(t, 0o755)
			blockDocker(t)

			restore := swapChmod(func(string, os.FileMode) error { return tt.chmodUp })
			defer restore()

			inst := New(Config{InstallDir: installDir})
			out := &recordingOutput{}
			inst.SetOutput(out)

			err := inst.Upgrade("v0.0.1")
			// The blocked docker pull fails the upgrade; a permissions error
			// from the tighten must not be what surfaces.
			if err != nil && strings.Contains(err.Error(), "permission") {
				t.Fatalf("upgrade failed on the directory tighten: %v", err)
			}

			warned := false
			for _, warn := range out.warns {
				if strings.Contains(warn, "Could not tighten permissions") {
					warned = true
				}
			}
			if warned != tt.wantWarn {
				t.Errorf("tighten warning = %v, want %v (warnings: %v)", warned, tt.wantWarn, out.warns)
			}

			info, statErr := os.Stat(filepath.Join(installDir, "config"))
			if statErr != nil {
				t.Fatal(statErr)
			}
			if got := info.Mode().Perm(); got != 0o755 {
				t.Errorf("config directory mode = %o, want it left at 755 because chmod failed", got)
			}
		})
	}
}

// swapChmod replaces the chmod seam and returns its restore function.
func swapChmod(fn func(string, os.FileMode) error) func() {
	original := chmodPath
	chmodPath = fn
	return func() { chmodPath = original }
}
