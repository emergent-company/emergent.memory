package installer

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	// pgUpgradeImage is the Docker image used to perform the in-place pg_upgrade.
	// It handles the full pg_upgrade orchestration from pg16 → pg17 automatically.
	pgUpgradeImage = "pgautoupgrade/pgautoupgrade:17-bookworm"

	// postgresVolumeName is the Docker Compose-generated volume name.
	// Docker Compose prefixes volumes with the project name, which defaults
	// to the directory name containing docker-compose.yml ("docker").
	postgresVolumeName = "docker_postgres_data"

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

// detectPostgresVersion reads the PG_VERSION file from the postgres data volume
// by running a short-lived alpine container. Returns the major version integer
// (e.g. 16) or 0 if the volume does not exist / is not readable.
func detectPostgresVersion() (int, error) {
	cmd := exec.Command("docker", "run", "--rm",
		"-v", postgresVolumeName+":/data:ro",
		"alpine:3.21",
		"cat", pgVersionFile,
	)
	out, err := cmd.Output()
	if err != nil {
		// Volume may not exist yet (fresh install path) — that's fine.
		return 0, nil
	}
	versionStr := strings.TrimSpace(string(out))
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return 0, fmt.Errorf("unexpected PG_VERSION content %q: %w", versionStr, err)
	}
	return version, nil
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
func (i *Installer) RunPostgresUpgrade(docker *DockerManager) error {
	i.output.Step("Stopping database for upgrade...")
	// Stop only the db service to avoid disrupting minio/kreuzberg unnecessarily.
	stopCmd := exec.Command("docker", "compose",
		"-f", docker.composePath(),
		"--env-file", docker.envPath(),
		"stop", "db",
	)
	stopCmd.Dir = docker.composePath()
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
	i.output.Info("Data volume: %s", postgresVolumeName)

	upgradeCmd := exec.Command("docker", "run", "--rm",
		"-v", postgresVolumeName+":/var/lib/postgresql/data",
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
