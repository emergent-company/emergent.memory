package installer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The installer's own writer quotes a password that contains a "#" (or starts
// with a quote), and the previous prefix scan handed those bytes — quotes and
// inline comment included — to pg_upgrade, which then failed to authenticate
// against its own database. The reader goes through the shared env-file scanner
// now, so it sees the value the service sees (#2170).
func TestReadPgCredsFromEnvUnquotesAQuotedPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	content := "POSTGRES_USER=emergent\nPOSTGRES_PASSWORD='p@ss #1'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	user, password := readPgCredsFromEnv(path)
	if user != "emergent" {
		t.Errorf("user = %q, want emergent", user)
	}
	if password != "p@ss #1" {
		t.Fatalf("password = %q, want the unquoted value the service reads", password)
	}
}

// The old scan also kept a trailing inline comment and missed an "export "
// prefix, so both are pinned here.
func TestReadPgCredsFromEnvReadsTheStatementNotTheLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	content := "export POSTGRES_USER=emergent # the app role\nPOSTGRES_PASSWORD=plain\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	user, password := readPgCredsFromEnv(path)
	if user != "emergent" {
		t.Errorf("user = %q, want emergent without the comment", user)
	}
	if password != "plain" {
		t.Errorf("password = %q, want plain", password)
	}
}

// The pgautoupgrade image's default is ("postgres", ""), which is what both a
// missing file and an empty assignment must fall back to.
func TestReadPgCredsFromEnvDefaults(t *testing.T) {
	user, password := readPgCredsFromEnv(filepath.Join(t.TempDir(), "missing.env"))
	if user != "postgres" || password != "" {
		t.Errorf("missing file = (%q, %q), want (postgres, \"\")", user, password)
	}

	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("POSTGRES_USER=\nPOSTGRES_PASSWORD=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, password = readPgCredsFromEnv(path)
	if user != "postgres" {
		t.Errorf("empty POSTGRES_USER = %q, want the default postgres", user)
	}
	if password != "" {
		t.Errorf("empty POSTGRES_PASSWORD = %q, want empty", password)
	}
}

// refreshCollationVersions builds `ALTER DATABASE <name> REFRESH COLLATION
// VERSION` from catalog output, so the name must survive quoting rather than
// becoming SQL. Postgres allows spaces, quotes and keywords in database names.
func TestQuoteIdent(t *testing.T) {
	cases := map[string]string{
		"emergent":  `"emergent"`,
		"postgres":  `"postgres"`,
		"app db":    `"app db"`,
		`we"ird`:    `"we""ird"`,
		"select":    `"select"`,
		"a.b; DROP": `"a.b; DROP"`,
	}
	for in, want := range cases {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
}

// --- Postgres upgrade helpers (#2225) ----------------------------------------

// warnedAbout reports whether a captured warning contains substr. The failure
// report speaks only through Warn, so this is the whole surface the wording test
// needs; recordingOutput itself lives in installer_configdir_test.go.
func warnedAbout(output *recordingOutput, substr string) bool {
	for _, warning := range output.warns {
		if strings.Contains(warning, substr) {
			return true
		}
	}
	return false
}

// The failure report must match what recovery actually did. It used to claim
// "has been restored" whenever the volume was not on the target major, including
// the run where restorePreviousCluster moved nothing because pg_upgrade failed
// before it created old/ (#2225).
func TestReportUpgradeFailureWording(t *testing.T) {
	cause := errors.New("pg_upgrade exited 1")
	onTarget := strconv.Itoa(PostgresMajorVersion)
	older := strconv.Itoa(PostgresMajorVersion - 1)

	cases := []struct {
		name         string
		major        int
		versionErr   error
		restored     bool
		wantOnTarget bool
		want         []string
		deny         []string
	}{
		{
			name:         "data already reached the target major",
			major:        PostgresMajorVersion,
			wantOnTarget: true,
			want:         []string{"already on PostgreSQL " + onTarget, "Re-run `memory upgrade`"},
			deny:         []string{"has been restored"},
		},
		{
			name:     "the previous cluster was put back",
			major:    PostgresMajorVersion - 1,
			restored: true,
			want:     []string{"has been restored", "PostgreSQL " + older + " will start as before"},
			deny:     []string{"failed before it touched"},
		},
		{
			name:  "there was nothing to restore",
			major: PostgresMajorVersion - 1,
			want:  []string{"intact", "failed before it touched", "PostgreSQL " + older + " will start as before"},
			deny:  []string{"has been restored"},
		},
		{
			name:       "the version cannot be read",
			versionErr: errors.New("volume gone"),
			want:       []string{"Could not read the data volume's version"},
			deny:       []string{"has been restored", "will start as before"},
		},
		{
			name:       "the version cannot be read but the cluster was restored",
			versionErr: errors.New("volume gone"),
			restored:   true,
			want:       []string{"Could not read the data volume's version", "has been restored"},
			deny:       []string{"will start as before"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := &recordingOutput{}
			installer := &Installer{output: output}

			gotOnTarget, err := installer.reportUpgradeFailure(tc.major, tc.versionErr, tc.restored, cause)
			if err == nil {
				t.Fatal("a failed upgrade must return an error")
			}
			if !errors.Is(err, cause) {
				t.Errorf("error %v does not wrap the cause", err)
			}
			if gotOnTarget != tc.wantOnTarget {
				t.Errorf("reported on-target = %v, want %v", gotOnTarget, tc.wantOnTarget)
			}
			for _, want := range tc.want {
				if !warnedAbout(output, want) {
					t.Errorf("failure report missing %q; printed:\n%s", want, strings.Join(output.warns, "\n"))
				}
			}
			for _, deny := range tc.deny {
				if warnedAbout(output, deny) {
					t.Errorf("failure report claims %q, which this run did not do; printed:\n%s", deny, strings.Join(output.warns, "\n"))
				}
			}
		})
	}
}

// The derived image is what makes pg_upgrade see the pgvector library; the
// package name carries the major, so a half-bump would drop it (#2225).
func TestUpgradeDockerfileAddsTheTargetPgvectorPackage(t *testing.T) {
	dockerfile := upgradeDockerfile(PostgresMajorVersion)

	if !strings.Contains(dockerfile, "FROM "+pgUpgradeImage) {
		t.Errorf("upgrade Dockerfile does not build on %q:\n%s", pgUpgradeImage, dockerfile)
	}
	if want := fmt.Sprintf("postgresql-%d-pgvector", PostgresMajorVersion); !strings.Contains(dockerfile, want) {
		t.Errorf("upgrade Dockerfile does not install %s, and pg_upgrade aborts on a missing extension library:\n%s", want, dockerfile)
	}
}

// requireDocker skips when there is no usable daemon. The helpers under test read
// and move a real volume — where PG_VERSION sits and what the old/ layout looks
// like is the entire contract — so a mocked filesystem would assert nothing.
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}
	if err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skip("docker daemon not reachable")
	}
}

// pgTestVolume creates a throwaway volume and removes it whatever the test does.
func pgTestVolume(t *testing.T) string {
	t.Helper()
	volume := fmt.Sprintf("memory-pg-test-%d", time.Now().UnixNano())
	if out, err := exec.Command("docker", "volume", "create", volume).CombinedOutput(); err != nil {
		t.Fatalf("create volume %s: %v: %s", volume, err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", volume).Run() })
	return volume
}

// runInVolume runs a shell snippet with the volume mounted at /data, using the
// same alpine image the production helpers use.
func runInVolume(t *testing.T, volume, script string) string {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "-v", volume+":/data", "alpine:3.21", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("run in volume %s: %v: %s", volume, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func TestVolumePgVersionReadsTheVolume(t *testing.T) {
	requireDocker(t)
	volume := pgTestVolume(t)
	runInVolume(t, volume, "echo 17 > /data/PG_VERSION")

	major, err := volumePgVersion(volume)
	if err != nil {
		t.Fatalf("volumePgVersion: %v", err)
	}
	if major != 17 {
		t.Errorf("volumePgVersion = %d, want 17", major)
	}

	// A directory with no cluster must be an error, not version 0: the caller
	// decides whether the upgrade happened from this value.
	runInVolume(t, volume, "rm -f /data/PG_VERSION")
	if _, err := volumePgVersion(volume); err == nil {
		t.Error("volumePgVersion on a volume without PG_VERSION returned no error")
	}
}

func TestRestorePreviousClusterPutsTheOldClusterBack(t *testing.T) {
	requireDocker(t)
	volume := pgTestVolume(t)
	runInVolume(t, volume, `
set -e
mkdir -p /data/old/pgdata /data/new
echo 17 > /data/old/PG_VERSION
echo base > /data/old/pgdata/base.tar
echo 18 > /data/new/PG_VERSION
echo lock > /data/upgrade_in_progress.lock
`)

	installer := &Installer{output: &recordingOutput{}}
	restored, err := installer.restorePreviousCluster(volume)
	if err != nil {
		t.Fatalf("restorePreviousCluster: %v", err)
	}
	if !restored {
		t.Fatal("restorePreviousCluster moved an old/ cluster but reported that it restored nothing")
	}

	root := runInVolume(t, volume, `
set -e
cat /data/PG_VERSION
cat /data/pgdata/base.tar
[ ! -e /data/old ] && echo no-old
[ ! -e /data/new ] && echo no-new
[ ! -e /data/upgrade_in_progress.lock ] && echo no-lock
`)
	for _, want := range []string{"17", "base", "no-old", "no-new", "no-lock"} {
		if !strings.Contains(root, want) {
			t.Errorf("restored volume is missing %q; got:\n%s", want, root)
		}
	}

	// A second call is a no-op: nothing is left to restore.
	restored, err = installer.restorePreviousCluster(volume)
	if err != nil {
		t.Fatalf("second restorePreviousCluster: %v", err)
	}
	if restored {
		t.Error("a second restore reported restoring a cluster that was already back")
	}
}

// PG_VERSION moves last, so a restore that cannot leave one at the root must fail
// rather than report success: the caller's message says the data is back, and the
// previous image would refuse to open a root without it (#2225).
//
// The failure must also be distinguishable from "the move itself failed", because
// the advice differs: the files may already have been moved to the root (#2399).
func TestRestorePreviousClusterFailsWhenTheOldClusterHasNoPgVersion(t *testing.T) {
	requireDocker(t)
	volume := pgTestVolume(t)
	runInVolume(t, volume, "mkdir -p /data/old/pgdata && echo base > /data/old/pgdata/base.tar")

	installer := &Installer{output: &recordingOutput{}}
	_, err := installer.restorePreviousCluster(volume)
	if err == nil {
		t.Fatal("a restore with no PG_VERSION in old/ returned success")
	}
	if !errors.Is(err, errRestoreIncomplete) {
		t.Errorf("error %v is not errRestoreIncomplete, so the caller cannot tell a partial restore from one that moved nothing", err)
	}
}

// An incomplete restore still runs the move loop, so every entry of old/ lands at
// the volume root before the PG_VERSION check fails. Asserting the layout is what
// keeps the advice from drifting away from the script (#2399).
func TestIncompleteRestoreLeavesTheOldFilesAtTheVolumeRoot(t *testing.T) {
	requireDocker(t)
	volume := pgTestVolume(t)
	runInVolume(t, volume, `set -e
mkdir -p /data/old/pgdata
echo base > /data/old/pgdata/base.tar
echo conf > /data/old/postgresql.conf
`)

	installer := &Installer{output: &recordingOutput{}}
	_, err := installer.restorePreviousCluster(volume)
	if !errors.Is(err, errRestoreIncomplete) {
		t.Fatalf("restore error = %v, want errRestoreIncomplete", err)
	}

	layout := runInVolume(t, volume, `set -e
[ -f /data/postgresql.conf ] && echo conf-at-root
[ -f /data/pgdata/base.tar ] && echo base-at-root
[ -d /data/old ] && echo old-present
[ -d /data/old ] && echo "old-entries=$(ls -A /data/old | wc -l)"
`)
	for _, want := range []string{"conf-at-root", "base-at-root", "old-entries=0"} {
		if !strings.Contains(layout, want) {
			t.Errorf("volume layout is missing %q; got:\n%s", want, layout)
		}
	}
}

// The advice printed when recovery fails is acted on, so it has to match what
// actually happened: a failed move leaves the data in old/, while an old/
// directory with no PG_VERSION may already have been drained into the root
// (#2399).
func TestRestoreFailureAdviceMatchesTheFailure(t *testing.T) {
	partial := fmt.Errorf("%w: exit status 1", errRestoreIncomplete)
	headline, advice := restoreFailureAdvice(partial)

	for _, text := range []string{headline, advice} {
		if strings.Contains(text, "still in the volume's old/ directory and could not be moved back") {
			t.Errorf("a partial restore is described as a move that failed: %q", text)
		}
	}
	if !strings.Contains(headline, "PG_VERSION") {
		t.Errorf("the partial-restore headline does not say why it could not be completed: %q", headline)
	}
	// The loop moves every entry except PG_VERSION before the check fails, so the
	// data is at the ROOT and old/ is empty. Telling the operator to look in old/
	// is the exact mistake #2399 exists to remove.
	if strings.Contains(advice, "are in the volume's old/ directory") {
		t.Errorf("the partial-restore advice points at a directory the attempt drained: %q", advice)
	}
	if !strings.Contains(advice, "volume root") {
		t.Errorf("the partial-restore advice does not say where the files actually are: %q", advice)
	}

	headline, advice = restoreFailureAdvice(errors.New("docker: no such volume"))
	if !strings.Contains(headline, "still in the volume's old/ directory") {
		t.Errorf("a failed move no longer tells the operator where the data is: %q", headline)
	}
	if strings.Contains(headline, "PG_VERSION") {
		t.Errorf("a failed move is blamed on a missing PG_VERSION: %q", headline)
	}
	if advice == "" {
		t.Error("a failed move produced no advice")
	}
}
