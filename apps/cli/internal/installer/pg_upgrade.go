package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/envfile"
)

const (
	// pgUpgradeImage is the Docker image used to perform the in-place pg_upgrade.
	// It bundles the server binaries for every older major (9.5 through 17), so a
	// cluster on 16 or 17 upgrades to 18 in a single hop without an intermediate
	// step. Keep its distro in step with PostgresImage: pg_upgrade rebuilds every
	// index with this image's libc, so a mismatch would leave the data ordered by
	// a collation the runtime image does not have.
	pgUpgradeImage = "pgautoupgrade/pgautoupgrade:18-trixie"

	// pgVolumeBaseName is the logical volume name declared in docker-compose.yml.
	// Docker Compose prepends the project name to produce the real Docker volume name,
	// e.g. "<project>_postgres_data".
	pgVolumeBaseName = "postgres_data"
)

// pgTuningConfig holds the computed PostgreSQL memory/WAL tuning parameters
// derived from the host machine's available RAM.
type pgTuningConfig struct {
	SharedBuffers      string
	EffectiveCacheSize string
	MaintenanceWorkMem string
	WorkMem            string
	MaxWalSize         string
	CheckpointTimeout  string
	MaxParallelWorkers int
}

// resolvePostgresVolumeName returns the actual Docker volume name for the
// postgres data volume.  Docker Compose prefixes logical volume names with
// the project name.  The project name defaults to the directory that contains
// docker-compose.yml but can be overridden by COMPOSE_PROJECT_NAME or the
// top-level `name:` key in the compose file.
//
// Strategy (first match wins):
//  1. Ask `docker compose ls --format json` — find the project whose
//     ConfigFiles share a directory with composePath, then build
//     "<projectName>_postgres_data".  This works even when the project name
//     differs from the directory name (e.g. project "minimal" in dir "minimal").
//  2. Scan `docker volume ls` for any volume ending in "_postgres_data" — only
//     trusted when exactly one such volume exists.
//  3. Fall back to "<dir>_postgres_data" derived from the compose file path.
func resolvePostgresVolumeName(composePath string) string {
	// Strategy 1: find the running compose project that owns this compose file
	if name := volumeFromComposeLs(composePath); name != "" {
		return name
	}

	// Strategy 2: scan existing Docker volumes for exactly one "*_postgres_data"
	if name := volumeFromDockerLS(); name != "" {
		return name
	}

	// Strategy 3: derive from the directory name of the compose file
	dir := filepath.Base(filepath.Dir(composePath))
	return dir + "_" + pgVolumeBaseName
}

// volumeFromComposeLs uses `docker compose ls --all --format json` to find the
// compose project whose config files live in the same directory as composePath,
// then returns "<projectName>_postgres_data".
func volumeFromComposeLs(composePath string) string {
	composeDir := filepath.Dir(composePath)

	cmd := exec.Command("docker", "compose", "ls", "--all", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	type composeProject struct {
		Name        string `json:"Name"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	var projects []composeProject
	if err := json.Unmarshal(out, &projects); err != nil {
		return ""
	}

	for _, p := range projects {
		// ConfigFiles is a comma-separated list of absolute paths.
		for _, cfgFile := range strings.Split(p.ConfigFiles, ",") {
			cfgFile = strings.TrimSpace(cfgFile)
			if filepath.Dir(cfgFile) == composeDir {
				return p.Name + "_" + pgVolumeBaseName
			}
		}
	}
	return ""
}

// volumeFromDockerLS scans `docker volume ls` for a volume whose name ends
// with "_postgres_data". Only returns a result when exactly one such volume
// exists (to avoid ambiguity in multi-project environments).
func volumeFromDockerLS() string {
	cmd := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var matches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, "_"+pgVolumeBaseName) {
			matches = append(matches, line)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// detectPostgresVersion reads the PG_VERSION file from the postgres data volume
// by running a short-lived alpine container. Returns the major version integer
// (e.g. 16), the resolved volume name, or 0 if the volume does not exist.
// composePath is used to derive the correct volume name dynamically.
func detectPostgresVersion(composePath string) (int, string, error) {
	volumeName := resolvePostgresVolumeName(composePath)

	cmd := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/data:ro",
		"alpine:3.21",
		"cat", "/data/PG_VERSION",
	)
	out, err := cmd.Output()
	if err != nil {
		// Volume may not exist yet (fresh install path) — that's fine.
		return 0, volumeName, nil
	}
	versionStr := strings.TrimSpace(string(out))
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return 0, volumeName, fmt.Errorf("unexpected PG_VERSION content %q: %w", versionStr, err)
	}
	return version, volumeName, nil
}

// RunPostgresUpgrade performs an in-place major-version upgrade of the
// postgres data volume to PostgresMajorVersion using pgautoupgrade.
//
// It:
//  1. Stops the db service so no writes occur during upgrade
//  2. Pulls the pgautoupgrade image and builds the derived image that carries
//     the pgvector extension (see buildUpgradeImage)
//  3. Runs the upgrade container against the data volume
//     — passes POSTGRES_USER/PASSWORD so pg_upgrade uses the correct superuser
//     — pins PGDATA to the volume's legacy layout (see PGDATA in the compose
//     template: the 18 image defaults elsewhere and the mount would be ignored)
//  4. Verifies the volume really is on the target major, and puts the previous
//     cluster back if the upgrade failed (see restorePreviousCluster)
//  5. Streams progress to the output writer
//
// The caller (Upgrade) is responsible for starting services back up afterwards.
//
// The bool reports whether the volume ended up on PostgresMajorVersion, which is
// what tells the caller which compose file can start it: after a failure the old
// major means the previous image works, while a failure *after* the upgrade step
// (for example during the container's final reindex) leaves 18 data that the old
// image refuses to open.
func (i *Installer) RunPostgresUpgrade(docker *DockerManager, volumeName string) (bool, error) {
	i.output.Step("Pulling PostgreSQL upgrade image (%s)...", pgUpgradeImage)
	pullCmd := exec.Command("docker", "pull", pgUpgradeImage)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return false, fmt.Errorf("failed to pull upgrade image: %w", err)
	}
	i.output.Success("Base upgrade image ready")

	// The upstream image ships server binaries but no extensions, while
	// pg_upgrade aborts when a library the old cluster uses is missing from the
	// new installation. Memory's database always has pgvector, so the upgrade
	// runs from a derived image that adds it. Built before the database stops:
	// a registry or apt failure then costs no downtime.
	i.output.Step("Preparing the upgrade image (this may take a minute)...")
	upgradeImage, err := i.buildUpgradeImage()
	if err != nil {
		return false, fmt.Errorf("prepare upgrade image: %w", err)
	}
	i.output.Success("Upgrade image ready")

	// Stop only the db service to avoid disrupting seaweedfs/xberg
	// unnecessarily, and only once the upgrade can actually run.
	i.output.Step("Stopping database for upgrade...")
	stopCmd := exec.Command("docker", "compose",
		"-f", docker.composePath(),
		"--env-file", docker.envPath(),
		"stop", "db",
	)
	stopCmd.Dir = filepath.Dir(docker.composePath())
	if out, err := stopCmd.CombinedOutput(); err != nil {
		// Non-fatal: container might already be stopped.
		i.output.Warn("Could not stop db container (may already be stopped): %s", strings.TrimSpace(string(out)))
	} else {
		i.output.Success("Database stopped")
	}

	i.output.Step("Running pg_upgrade (this may take a minute)...")
	i.output.Info("Data volume: %s", volumeName)

	// Remove any stale lock file left by a previously interrupted upgrade
	// attempt.  pgautoupgrade refuses to start if upgrade_in_progress.lock
	// exists at the data-directory root, even if the previous run failed
	// midway.  We remove it unconditionally here — if a genuine concurrent
	// upgrade were running we would have failed to stop the db container above.
	i.output.Info("Clearing any stale upgrade lock file...")
	rmLockCmd := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/pgdata",
		"alpine:3.21",
		"sh", "-c", "rm -f /pgdata/upgrade_in_progress.lock && echo 'lock cleared'",
	)
	if out, err := rmLockCmd.CombinedOutput(); err != nil {
		i.output.Warn("Could not clear lock file (continuing anyway): %s", strings.TrimSpace(string(out)))
	}

	// Read POSTGRES_USER and POSTGRES_PASSWORD from .env.local so pg_upgrade
	// uses the correct superuser. The pgautoupgrade image defaults to "postgres"
	// which fails when the installation uses a custom superuser (e.g. "emergent").
	pgUser, pgPass := readPgCredsFromEnv(docker.envPath())
	i.output.Info("PostgreSQL superuser: %s", pgUser)

	upgradeCmd := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/var/lib/postgresql/data",
		"-e", "PGAUTO_ONESHOT=yes",
		// The upgrade image is built on the 18 layout, where the default data
		// directory is /var/lib/postgresql/18/docker. Point it at the mount, or
		// the attached volume is ignored and the upgrade runs on an empty
		// cluster — which the image reports as success (exit 0).
		"-e", "PGDATA=/var/lib/postgresql/data",
		"-e", "POSTGRES_USER="+pgUser,
		"-e", "POSTGRES_PASSWORD="+pgPass,
		upgradeImage,
	)
	upgradeCmd.Stdout = os.Stdout
	upgradeCmd.Stderr = os.Stderr

	if err := upgradeCmd.Run(); err != nil {
		restored, restoreErr := i.restorePreviousCluster(volumeName)
		if restoreErr != nil {
			headline, advice := restoreFailureAdvice(restoreErr)
			i.output.Error(headline, restoreErr)
			i.output.Error("%s", advice)
			return false, fmt.Errorf("pg_upgrade failed: %w", err)
		}
		// Read the version here rather than in the reporter: what the failure
		// message can honestly say depends on what recovery moved *and* on what
		// the volume now holds, and only this edge observes both.
		major, versionErr := volumePgVersion(volumeName)
		return i.reportUpgradeFailure(major, versionErr, restored, err)
	}

	// A container that exits 0 has not necessarily upgraded anything: with a
	// PGDATA that holds no cluster it prints "No existing Postgres data
	// directory found" and exits successfully. The volume is the source of
	// truth, so check it before declaring success.
	upgradedMajor, err := volumePgVersion(volumeName)
	if err != nil {
		restored, restoreErr := i.restorePreviousCluster(volumeName)
		switch {
		case restoreErr != nil:
			i.output.Error("Could not restore the previous cluster: %v", restoreErr)
			_, advice := restoreFailureAdvice(restoreErr)
			i.output.Error("%s", advice)
		case restored:
			i.output.Warn("The previous data has been restored.")
		}
		return false, fmt.Errorf("verify upgraded volume: %w", err)
	}
	if upgradedMajor != PostgresMajorVersion {
		restored, restoreErr := i.restorePreviousCluster(volumeName)
		if restoreErr != nil {
			i.output.Error("Could not restore the previous cluster: %v", restoreErr)
			_, advice := restoreFailureAdvice(restoreErr)
			i.output.Error("%s", advice)
			return false, fmt.Errorf("pg_upgrade left the volume on PostgreSQL %d instead of %d", upgradedMajor, PostgresMajorVersion)
		}
		if restored {
			i.output.Warn("The previous data has been restored.")
		}
		return false, fmt.Errorf("pg_upgrade did not run: the volume is still on PostgreSQL %d, not %d", upgradedMajor, PostgresMajorVersion)
	}

	i.output.Success("pg_upgrade completed — database upgraded to PostgreSQL %d", PostgresMajorVersion)
	return true, nil
}

// reportUpgradeFailure is the tail of the failure path: it says what state the
// volume is in and what to do next. It takes what recovery actually did
// (restored) and the version observed afterwards, because this path is reached
// both when the previous cluster was put back and when there was nothing to put
// back — pg_upgrade can fail before it creates old/ — and the message must not
// claim a restore in the second case. It also covers a container that exits
// non-zero *after* the upgrade step (its final reindex), where the data is
// already on the new major and telling the user their old data is back would be
// wrong in the other direction.
func (i *Installer) reportUpgradeFailure(major int, versionErr error, restored bool, cause error) (bool, error) {
	switch {
	case versionErr != nil:
		i.output.Warn("Could not read the data volume's version after the failure: %v", versionErr)
		if restored {
			i.output.Warn("The previous data has been restored.")
		}
	case major == PostgresMajorVersion:
		i.output.Warn("The data volume is already on PostgreSQL %d; the container failed after the upgrade step.", major)
		i.output.Warn("Re-run `memory upgrade` to finish — the server is left on the new version.")
		return true, fmt.Errorf("pg_upgrade failed after the data reached PostgreSQL %d: %w", major, cause)
	case restored:
		i.output.Warn("Your original data is intact and has been restored — PostgreSQL %d will start as before.", major)
	default:
		// recovery moved nothing, so claiming a restore would be false: the
		// failure happened before pg_upgrade touched the cluster.
		i.output.Warn("Your original data is intact — the upgrade failed before it touched the volume, so PostgreSQL %d will start as before.", major)
	}
	return false, fmt.Errorf("pg_upgrade failed: %w", cause)
}

// pgUpgradeLocalImage is the tag the derived upgrade image is built under. It
// is rebuilt on every upgrade so a new target major cannot reuse a stale image.
const pgUpgradeLocalImage = "emergent-memory-pg-upgrade"

// buildUpgradeImage derives the upgrade image from pgUpgradeImage and adds the
// pgvector extension for the target major.
//
// pg_upgrade's "Checking for presence of required libraries" step fails when a
// loadable library the old cluster references is missing from the new
// installation, and the upstream image ships no extensions at all. Every
// Memory database has pgvector (deploy/self-hosted/init.sql creates it), so
// upgrading without this step fails on a real install with
//
//	could not load library "$libdir/vector"
//
// The package comes from the PGDG repository the base image is already
// configured with, so the build needs registry and apt access — the same
// network the image pull above needs.
func (i *Installer) buildUpgradeImage() (string, error) {
	tag := fmt.Sprintf("%s:%d", pgUpgradeLocalImage, PostgresMajorVersion)

	dir, err := os.MkdirTemp("", "memory-pg-upgrade-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(upgradeDockerfile(PostgresMajorVersion)), 0o644); err != nil {
		return "", err
	}

	cmd := exec.Command("docker", "build", "-t", tag, "-f", dockerfilePath, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build %s from %s: %w (needs access to the image registry and the PGDG apt repository)", tag, pgUpgradeImage, err)
	}
	return tag, nil
}

// upgradeDockerfile is the derived upgrade image: the pgautoupgrade base plus the
// pgvector package for the target major. Split out of buildUpgradeImage so the
// package and base can be asserted without a registry build (#2225).
func upgradeDockerfile(major int) string {
	return fmt.Sprintf(`FROM %s
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-%d-pgvector \
 && rm -rf /var/lib/apt/lists/*
`, pgUpgradeImage, major)
}

// volumePgVersion reads PG_VERSION from the data volume.
func volumePgVersion(volumeName string) (int, error) {
	out, err := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/data:ro",
		"alpine:3.21",
		"cat", "/data/PG_VERSION",
	).Output()
	if err != nil {
		return 0, fmt.Errorf("read PG_VERSION from %s: %w", volumeName, err)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// errRestoreIncomplete reports a restore that could not leave a usable cluster at
// the volume root: the script moves PG_VERSION last and refuses to report success
// without one, which is what an old/ directory that was never a full cluster
// looks like.
var errRestoreIncomplete = errors.New("restore incomplete")

// restoreFailureAdvice is what the operator is told when recovery itself failed.
// It separates the two cases, because the advice is acted on: when the move
// itself failed the data is untouched in old/; when old/ held no PG_VERSION the
// files may already have been moved to the root by the failed attempt, so telling
// them the cluster "is still in old/" would send them looking in the wrong place.
func restoreFailureAdvice(restoreErr error) (headline, advice string) {
	if errors.Is(restoreErr, errRestoreIncomplete) {
		return "The volume's old/ directory did not hold a complete cluster (no PG_VERSION), so the pre-upgrade data could not be made startable at the volume root: %v",
			"Do not start the server. The attempt moved the pre-upgrade files to the volume root, but the root has no PG_VERSION, so PostgreSQL will not start; the volume's old/ directory is now empty."
	}
	return "The pre-upgrade cluster is still in the volume's old/ directory and could not be moved back automatically: %v",
		"Do not start the server until it is restored; the volume's old/ directory holds your data."
}

// restorePreviousCluster puts the pre-upgrade cluster back at the volume root
// so the previous compose file can start it again.
//
// A failed pg_upgrade leaves the volume as old/ (the original cluster) plus
// new/ (a partial target cluster) with no PG_VERSION at the root. The restored
// compose file then points an older postgres at a non-empty directory without
// one, and it crash-loops on
//
//	initdb: directory "/var/lib/postgresql/data" exists but is not empty
//
// so recovery is part of the failure path, not an afterthought. It is a no-op
// when there is nothing to restore.
func (i *Installer) restorePreviousCluster(volumeName string) (bool, error) {
	const script = `
set -e
cd /pgdata
rm -rf new
rm -f upgrade_in_progress.lock
[ -d old ] || exit 0

# PG_VERSION is what makes the root look like a cluster to the previous image, so
# it moves last: an interrupted restore then leaves a root without one, and the
# old image refuses to start instead of starting on a half-restored directory.
for f in old/* old/.[!.]*; do
  [ -e "$f" ] || continue
  case "$f" in old/PG_VERSION) continue ;; esac
  mv "$f" .
done
if [ -f old/PG_VERSION ]; then
  mv old/PG_VERSION .
fi
if [ ! -f PG_VERSION ]; then
  echo "restore incomplete: no PG_VERSION at the volume root" >&2
  exit 1
fi
rmdir old
echo restored
`
	out, err := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/pgdata",
		"alpine:3.21",
		"sh", "-c", script,
	).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "restore incomplete:") {
			return false, fmt.Errorf("%w: %s", errRestoreIncomplete, strings.TrimSpace(string(out)))
		}
		return false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	// The script prints "restored" only when it actually moved a cluster back.
	return strings.Contains(string(out), "restored"), nil
}

// refreshCollationVersions records the running image's collation version on
// every database after a major upgrade.
//
// pg_upgrade rebuilds the indexes inside the upgrade image and copies each
// database's recorded collation version from the old cluster. When the base
// distro moved (a bookworm 16/17 install upgraded by the trixie 18 image), the
// copies record the old glibc and every connection logs
//
//	WARNING: database "emergent" has a collation version mismatch
//
// even though the indexes were just rebuilt against the collation now in use.
// REFRESH COLLATION VERSION is the documented fix and is exactly right here:
// it records the truth for objects built by this image. It is a no-op when the
// versions already agree, so a same-distro upgrade costs nothing.
//
// Best effort: a failure is reported and the upgrade still succeeds.
func (i *Installer) refreshCollationVersions(docker *DockerManager) error {
	pgUser, _ := readPgCredsFromEnv(docker.envPath())

	dbListCmd := exec.Command("docker", "compose",
		"-f", docker.composePath(),
		"--env-file", docker.envPath(),
		"exec", "-T", "db",
		"psql", "-U", pgUser, "-d", "postgres", "-Atc",
		// template0 is skipped: it is not connectable and refreshing it buys
		// nothing. template1 is included so databases created from it later
		// inherit the current version.
		"SELECT datname FROM pg_database WHERE datname <> 'template0'",
	)
	// stdout only: the mismatch we are about to fix is printed on stderr, and
	// merging the two would turn WARNING/DETAIL/HINT lines into database names.
	var stderr bytes.Buffer
	dbListCmd.Stderr = &stderr
	dbList, err := dbListCmd.Output()
	if err != nil {
		return fmt.Errorf("list databases: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	for _, db := range strings.Split(strings.TrimSpace(string(dbList)), "\n") {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		// %I quoting happens server-side; the name never reaches a shell.
		stmt := fmt.Sprintf("ALTER DATABASE %s REFRESH COLLATION VERSION",
			quoteIdent(db))
		out, err := exec.Command("docker", "compose",
			"-f", docker.composePath(),
			"--env-file", docker.envPath(),
			"exec", "-T", "db",
			"psql", "-U", pgUser, "-d", "postgres", "-c", stmt,
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("refresh collation version on %q: %w: %s", db, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// quoteIdent quotes a Postgres identifier the way %I would.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// readPgCredsFromEnv parses POSTGRES_USER and POSTGRES_PASSWORD from the
// .env.local file at envPath. Falls back to ("postgres", "") if not found,
// which matches the pgautoupgrade image's own default.
//
// It reads through apps/cli/internal/envfile like every other env-file reader:
// the installer's own writer quotes a password that contains a "#", and passing
// the quotes through to pg meant the upgrade failed to authenticate against its
// own database (#2170). A last assignment wins, as it does for the service.
func readPgCredsFromEnv(envPath string) (user, password string) {
	user = "postgres"
	password = ""

	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	if v, ok := envfile.Read(data, "POSTGRES_USER"); ok && v != "" {
		user = v
	}
	if v, ok := envfile.Read(data, "POSTGRES_PASSWORD"); ok {
		password = v
	}
	return
}

// computePgTuning calculates optimal PostgreSQL configuration parameters
// based on the host machine's total system memory and CPU core count.
//
// Tuning tiers:
//   - High-end  (>= 64 GB): production-grade servers
//   - Standard  (>= 16 GB): typical dev/staging servers
//   - Minimal   (<  16 GB): laptops and small VMs
func computePgTuning() pgTuningConfig {
	totalRAM := getTotalRAMBytes()
	cpuCores := runtime.NumCPU()

	gb := uint64(1024 * 1024 * 1024)

	switch {
	case totalRAM >= 64*gb:
		// High-end server (e.g. 125 GB RAM, 32 cores)
		return pgTuningConfig{
			SharedBuffers:      "16GB",
			EffectiveCacheSize: "48GB",
			MaintenanceWorkMem: "2GB",
			WorkMem:            "64MB",
			MaxWalSize:         "16GB",
			CheckpointTimeout:  "15min",
			MaxParallelWorkers: cpuCores,
		}
	case totalRAM >= 16*gb:
		// Standard server (16–64 GB)
		return pgTuningConfig{
			SharedBuffers:      "4GB",
			EffectiveCacheSize: "12GB",
			MaintenanceWorkMem: "1GB",
			WorkMem:            "32MB",
			MaxWalSize:         "4GB",
			CheckpointTimeout:  "10min",
			MaxParallelWorkers: max(cpuCores, 4),
		}
	default:
		// Laptop / small VM (< 16 GB) — conservative defaults
		return pgTuningConfig{
			SharedBuffers:      "512MB",
			EffectiveCacheSize: "1536MB",
			MaintenanceWorkMem: "256MB",
			WorkMem:            "8MB",
			MaxWalSize:         "1GB",
			CheckpointTimeout:  "5min",
			MaxParallelWorkers: max(cpuCores, 2),
		}
	}
}

// max returns the larger of two ints (Go 1.21+ has this built-in but keeping
// explicit for compatibility with older toolchains that may be in CI).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getTotalRAMBytes returns the host's total physical memory in bytes.
// It reads from /proc/meminfo on Linux (covers all common server deployments).
// Falls back to a conservative 4 GB if the file cannot be parsed.
func getTotalRAMBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 4 * 1024 * 1024 * 1024 // 4 GB fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 4 * 1024 * 1024 * 1024 // 4 GB fallback
}
