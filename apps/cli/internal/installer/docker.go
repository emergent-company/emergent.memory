package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DockerManager struct {
	installDir string
	output     OutputWriter
}

func NewDockerManager(installDir string, output OutputWriter) *DockerManager {
	return &DockerManager{
		installDir: installDir,
		output:     output,
	}
}

func (d *DockerManager) composePath() string {
	return filepath.Join(d.installDir, "docker", "docker-compose.yml")
}

func (d *DockerManager) envPath() string {
	return filepath.Join(d.installDir, "config", ".env.local")
}

// composeArgs returns the full argument list for a `docker compose` invocation:
// the binary-relative prefix (compose file + env file) followed by args.
//
// The env file is passed explicitly and unconditionally so container
// recreation always reads the installation's single secrets file
// (config/.env.local) and never silently falls back to a stray
// docker/.env left over from an older layout (#2004). install and upgrade
// share this path, so they cannot diverge.
func (d *DockerManager) composeArgs(args ...string) []string {
	baseArgs := []string{
		"compose",
		"-f", d.composePath(),
		"--env-file", d.envPath(),
	}
	return append(baseArgs, args...)
}

// upArgs is the `compose up` argument list used by install and upgrade.
// --remove-orphans drops containers that belonged to the compose project but
// are no longer in the (regenerated) compose file — memory-minio,
// memory-minio-init and memory-kreuzberg after the object-store/pipeline
// transition (#2004). Without it they keep holding ports and RAM.
func (d *DockerManager) upArgs() []string {
	return []string{"up", "-d", "--remove-orphans"}
}

func (d *DockerManager) runCompose(args ...string) error {
	cmd := exec.Command("docker", d.composeArgs(args...)...)
	cmd.Dir = filepath.Join(d.installDir, "docker")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %v failed: %w\n%s", args, err, string(output))
	}
	return nil
}

func (d *DockerManager) Pull() error {
	return d.runCompose("pull")
}

func (d *DockerManager) Up() error {
	if orphans := d.legacyOrphans(); len(orphans) > 0 {
		d.output.Warn("Removing legacy containers left behind by the MinIO/Kreuzberg stack: %s",
			strings.Join(orphans, ", "))
	}
	return d.runCompose(d.upArgs()...)
}

// legacyServiceNames are the compose service names that older self-hosted
// installs ran and the current template no longer defines. They are removed by
// `up --remove-orphans`; listing them first tells the user what is going away.
var legacyServiceNames = []string{
	"memory-minio",
	"memory-minio-init",
	"memory-kreuzberg",
}

// legacyOrphans returns the still-present legacy containers, best-effort: a
// docker failure (no daemon, no permission) yields no warning rather than an
// error, since --remove-orphans still does the right thing.
func (d *DockerManager) legacyOrphans() []string {
	out, err := exec.Command("docker", "ps", "-a", "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	return filterLegacyOrphans(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

// filterLegacyOrphans returns the legacy container names present in names, in
// the stable order of legacyServiceNames. Extracted so the selection is
// testable without a Docker daemon.
func filterLegacyOrphans(names []string) []string {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[strings.TrimSpace(n)] = true
	}

	var orphans []string
	for _, legacy := range legacyServiceNames {
		if present[legacy] {
			orphans = append(orphans, legacy)
		}
	}
	return orphans
}

func (d *DockerManager) Down(removeVolumes bool) error {
	// --remove-orphans keeps `down` consistent with `up`: legacy containers
	// that are no longer in the compose file are removed too instead of being
	// left running with no compose project owning them (#2004).
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "-v")
	}
	return d.runCompose(args...)
}

func (d *DockerManager) RemoveVolumes() error {
	// Volume names are prefixed with the Docker Compose project name,
	// which defaults to the directory name ("docker" from ~/.memory/docker/).
	volumes := []string{
		"docker_postgres_data",
		"docker_object_store_data",
		"docker_memory_cli_config",
	}

	var lastErr error
	for _, vol := range volumes {
		cmd := exec.Command("docker", "volume", "rm", vol)
		if err := cmd.Run(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (d *DockerManager) WaitForHealth(port int, timeoutSeconds int) error {
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	client := &http.Client{Timeout: 5 * time.Second}

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	time.Sleep(5 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		<-ticker.C
	}

	return fmt.Errorf("health check timed out after %d seconds", timeoutSeconds)
}

// BuildSandboxImages builds a Docker image only when it is missing or its
// Dockerfile content changed (tracked via the emergent.dockerfile.sha label).
//
// gitTokenEnv is the name of an environment variable holding a GitHub token
// (e.g. "GH_TOKEN" or "GITHUB_TOKEN"); when non-empty it is passed to the
// build as the github_token BuildKit secret so the Dockerfile can clone the
// private memory repo. Empty means no secret is passed (unauthenticated).
func (d *DockerManager) BuildSandboxImages(dockerfileContent, imageName, gitTokenEnv string) error {
	contentHash := dockerfileContentHash(dockerfileContent)
	if d.imageHasDockerfileHash(imageName, contentHash) {
		d.output.Info("Sandbox image %s is up to date, skipping build", imageName)
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "memory-sdk-build-*")
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	args := []string{"build", "-t", imageName, "--label", "emergent.dockerfile.sha=" + contentHash}
	if gitTokenEnv != "" {
		args = append(args, "--secret", "id=github_token,env="+gitTokenEnv)
	}
	args = append(args, tmpDir)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build %s failed: %w\n%s", imageName, err, string(output))
	}
	return nil
}

// gitTokenSecretEnv returns the name of the GitHub token env var to expose to
// docker builds as the github_token BuildKit secret: "GH_TOKEN" if set, else
// "GITHUB_TOKEN", else "" (no secret). The CLI passes the var NAME (not the
// value) so docker reads the token from the CLI process's own environment via
// --secret id=github_token,env=<NAME>.
func gitTokenSecretEnv() string {
	if os.Getenv("GH_TOKEN") != "" {
		return "GH_TOKEN"
	}
	if os.Getenv("GITHUB_TOKEN") != "" {
		return "GITHUB_TOKEN"
	}
	return ""
}

// dockerfileContentHash returns the sha256 hex digest of a Dockerfile's content.
func dockerfileContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// imageHasDockerfileHash reports whether imageName exists locally and was built
// from a Dockerfile whose content hash matches wantHash.
func (d *DockerManager) imageHasDockerfileHash(imageName, wantHash string) bool {
	cmd := exec.Command("docker", "image", "inspect", imageName, "--format", `{{index .Config.Labels "emergent.dockerfile.sha"}}`)
	out, err := cmd.Output()
	if err != nil {
		return false // image absent or inspect failed → needs a build
	}
	return strings.TrimSpace(string(out)) == wantHash
}

func (d *DockerManager) Logs(service string, lines int) (string, error) {
	args := []string{"logs", "--tail", fmt.Sprintf("%d", lines)}
	if service != "" {
		args = append(args, service)
	}

	cmd := exec.Command("docker", d.composeArgs(args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	return string(output), nil
}
