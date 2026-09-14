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

func (d *DockerManager) runCompose(args ...string) error {
	baseArgs := []string{
		"compose",
		"-f", d.composePath(),
		"--env-file", d.envPath(),
	}
	baseArgs = append(baseArgs, args...)

	cmd := exec.Command("docker", baseArgs...)
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
	return d.runCompose("up", "-d")
}

func (d *DockerManager) Down(removeVolumes bool) error {
	args := []string{"down"}
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
		"docker_minio_data",
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
func (d *DockerManager) BuildSandboxImages(dockerfileContent, imageName string) error {
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

	cmd := exec.Command("docker", "build", "-t", imageName, "--label", "emergent.dockerfile.sha="+contentHash, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build %s failed: %w\n%s", imageName, err, string(output))
	}
	return nil
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

	baseArgs := []string{
		"compose",
		"-f", d.composePath(),
		"--env-file", d.envPath(),
	}
	baseArgs = append(baseArgs, args...)

	cmd := exec.Command("docker", baseArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	return string(output), nil
}
