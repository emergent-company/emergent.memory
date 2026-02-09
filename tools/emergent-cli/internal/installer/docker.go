package installer

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
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
	volumes := []string{
		"emergent_postgres_data",
		"emergent_minio_data",
		"emergent_cli_config",
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
