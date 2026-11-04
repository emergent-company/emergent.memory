package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/envfile"
	"github.com/joho/godotenv"
)

// envLocalMode is the only mode this CLI accepts for .env.local: the file holds
// a live project credential, so it must not be world-readable on a shared
// machine.
const envLocalMode os.FileMode = 0o600

// writeEnvLocal merges updates into .env.local in dir, preserving every other
// key, and returns the absolute path of the file. The file is forced to 0600
// because it holds a live credential: the shared writer sets the mode on the
// replacement, so an existing 0644 file is tightened by the same rewrite.
//
// Every key goes through internal/envfile, the CLI's one .env writer: comments
// and key order survive (a marshal of the parsed map dropped both), the replace
// is atomic, and an existing file that cannot be parsed is refused rather than
// silently reduced to the keys this command knows about.
func writeEnvLocal(dir string, updates map[string]string) (string, error) {
	path := filepath.Join(dir, ".env.local")

	// The writer edits an existing file; a fresh project context starts from an
	// empty one. Creating it 0600 first keeps the credential unreadable even if
	// the update below refuses, and os.WriteFile's perm only applies on create.
	existing, statErr := os.ReadFile(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", fmt.Errorf("failed to read %s: %w", path, statErr)
	}
	if os.IsNotExist(statErr) {
		if err := os.WriteFile(path, nil, envLocalMode); err != nil {
			return "", fmt.Errorf("failed to create %s: %w", path, err)
		}
		existing = nil
	}

	// Refuse a file the loader cannot read. The shared writer deliberately does
	// not gate on a file that did not parse before (its rewrite is byte-preserving
	// apart from the target span), but this command merges whole keys into a
	// credential file: appending into an unreadable file would hide every key it
	// still holds. `memory config set` keeps the looser rule on purpose.
	if _, parseErr := godotenv.Unmarshal(string(existing)); parseErr != nil {
		return "", fmt.Errorf("failed to read existing %s (refusing to overwrite it): %w", path, parseErr)
	}

	if err := envfile.UpdateAll(path, updates, envLocalMode); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
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
