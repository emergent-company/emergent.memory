package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	// pgUpgradeImage is the Docker image used to perform the in-place pg_upgrade.
	// It handles the full pg_upgrade orchestration from pg16 → pg17 automatically.
	pgUpgradeImage = "pgautoupgrade/pgautoupgrade:17-bookworm"

	// pgVolumeBaseName is the logical volume name declared in docker-compose.yml.
	// Docker Compose prepends the project name to produce the real Docker volume name,
	// e.g. "<project>_postgres_data".
	pgVolumeBaseName = "postgres_data"

	// pgVersionFile is the path inside the data volume that stores the major version.
	pgVersionFile = "/var/lib/postgresql/data/PG_VERSION"
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
// postgres data volume from pg16 to pg17 using pgautoupgrade.
//
// It:
//  1. Stops the db service so no writes occur during upgrade
//  2. Pulls the pgautoupgrade image
//  3. Runs the upgrade container against the data volume
//  4. Streams progress to the output writer
//
// The caller (Upgrade) is responsible for starting services back up afterwards.
func (i *Installer) RunPostgresUpgrade(docker *DockerManager, volumeName string) error {
	i.output.Step("Stopping database for upgrade...")
	// Stop only the db service to avoid disrupting minio/kreuzberg unnecessarily.
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

	i.output.Step("Pulling PostgreSQL upgrade image (%s)...", pgUpgradeImage)
	pullCmd := exec.Command("docker", "pull", pgUpgradeImage)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull upgrade image: %w", err)
	}
	i.output.Success("Upgrade image ready")

	i.output.Step("Running pg_upgrade (this may take a minute)...")
	i.output.Info("Data volume: %s", volumeName)

	upgradeCmd := exec.Command("docker", "run", "--rm",
		"-v", volumeName+":/var/lib/postgresql/data",
		"-e", "PGAUTO_ONESHOT=yes",
		pgUpgradeImage,
	)
	upgradeCmd.Stdout = os.Stdout
	upgradeCmd.Stderr = os.Stderr

	if err := upgradeCmd.Run(); err != nil {
		return fmt.Errorf("pg_upgrade failed: %w\n\nYour original data is intact. Check the output above for details.", err)
	}

	i.output.Success("pg_upgrade completed — database upgraded to PostgreSQL 17")
	return nil
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
