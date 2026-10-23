package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// envLocalMode is the only mode this CLI accepts for .env.local: the file holds
// a live project credential, so it must not be world-readable on a shared
// machine.
const envLocalMode os.FileMode = 0o600

// writeEnvLocal merges updates into .env.local in dir, preserving every other
// key, and returns the absolute path of the file. The file is forced to 0600
// because it holds a live credential: os.WriteFile only applies perm on
// creation, so an existing 0644 file is chmodded afterwards.
//
// An existing file that cannot be parsed is never overwritten — doing so would
// silently drop every key it currently holds, including other credentials.
func writeEnvLocal(dir string, updates map[string]string) (string, error) {
	path := filepath.Join(dir, ".env.local")

	envMap, readErr := godotenv.Read(path)
	if readErr != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			// The file exists but cannot be parsed: overwriting it would silently
			// drop every key it currently holds, including other credentials.
			return "", fmt.Errorf("failed to read existing %s (refusing to overwrite it): %w", path, readErr)
		}
	}
	if envMap == nil {
		envMap = make(map[string]string)
	}
	for key, value := range updates {
		envMap[key] = value
	}

	data, err := godotenv.Marshal(envMap)
	if err != nil {
		return "", fmt.Errorf("failed to encode %s: %w", path, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data += "\n"
	}
	if err := os.WriteFile(path, []byte(data), envLocalMode); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := os.Chmod(path, envLocalMode); err != nil {
		return "", fmt.Errorf("failed to tighten permissions on %s: %w", path, err)
	}

	if abs, absErr := filepath.Abs(path); absErr == nil {
		return abs, nil
	}
	return path, nil
}

// writeProjectEnvLocal merges the active-project keys (plus any extras such as
// MEMORY_SERVER_URL) into dir/.env.local, prints the absolute path of the file
// once, and returns it. Both `memory init` and `memory projects set` write the
// same key set, so they share this so the mode and the message cannot drift.
func writeProjectEnvLocal(w io.Writer, dir, projectID, projectName, token string, extra map[string]string) (string, error) {
	updates := make(map[string]string, len(extra)+3)
	for key, value := range extra {
		updates[key] = value
	}
	updates["MEMORY_PROJECT_ID"] = projectID
	updates["MEMORY_PROJECT_NAME"] = projectName
	updates["MEMORY_PROJECT_API_KEY"] = token

	path, err := writeEnvLocal(dir, updates)
	if err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(w, "Wrote project context to %s (mode 0600)\n", path)
	return path, nil
}
