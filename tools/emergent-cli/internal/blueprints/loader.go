package blueprints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDir walks the packs/ and agents/ subdirectories inside dir and returns
// all successfully parsed PackFile and AgentFile values.
//
// Files with unknown extensions are silently skipped.
// Files that fail to parse or fail validation are recorded in the returned
// results with BlueprintsActionError — processing continues for remaining files.
// Missing packs/ or agents/ subdirectories are not an error.
func LoadDir(dir string) (packs []PackFile, agents []AgentFile, results []BlueprintsResult, err error) {
	packs, packResults := loadPacks(filepath.Join(dir, "packs"))
	agents, agentResults := loadAgents(filepath.Join(dir, "agents"))
	results = append(packResults, agentResults...)
	return packs, agents, results, nil
}

// ──────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────

func loadPacks(dir string) ([]PackFile, []BlueprintsResult) {
	entries, ok := readDir(dir)
	if !ok {
		return nil, nil
	}

	var packs []PackFile
	var results []BlueprintsResult

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue // silently skip non-supported extensions
		}

		var pack PackFile
		if err := decodeFile(path, ext, &pack); err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "pack",
				Name:         name,
				SourceFile:   path,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("parse error: %w", err),
			})
			continue
		}
		pack.SourceFile = path

		if err := validatePack(&pack); err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "pack",
				Name:         pack.Name,
				SourceFile:   path,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("validation error: %w", err),
			})
			continue
		}

		packs = append(packs, pack)
	}

	return packs, results
}

func loadAgents(dir string) ([]AgentFile, []BlueprintsResult) {
	entries, ok := readDir(dir)
	if !ok {
		return nil, nil
	}

	var agents []AgentFile
	var results []BlueprintsResult

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		var agent AgentFile
		if err := decodeFile(path, ext, &agent); err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "agent",
				Name:         name,
				SourceFile:   path,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("parse error: %w", err),
			})
			continue
		}
		agent.SourceFile = path

		if err := validateAgent(&agent); err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "agent",
				Name:         agent.Name,
				SourceFile:   path,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("validation error: %w", err),
			})
			continue
		}

		agents = append(agents, agent)
	}

	return agents, results
}

// readDir reads directory entries. Returns (entries, true) on success,
// (nil, false) when the directory does not exist (not treated as error).
func readDir(dir string) ([]os.DirEntry, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		// Treat other errors as "no entries" — caller will get empty slice.
		return nil, false
	}
	return entries, true
}

// decodeFile reads the file at path and decodes it into v using JSON or YAML
// depending on ext (.json, .yaml/.yml).
func decodeFile(path, ext string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	switch ext {
	case ".json":
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("json decode: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, v); err != nil {
			return fmt.Errorf("yaml decode: %w", err)
		}
	default:
		return fmt.Errorf("unsupported extension: %s", ext)
	}
	return nil
}

// validatePack checks required fields for a PackFile.
func validatePack(p *PackFile) error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if p.Version == "" {
		return fmt.Errorf("version is required")
	}
	if len(p.ObjectTypes) == 0 {
		return fmt.Errorf("objectTypes must have at least one entry")
	}
	return nil
}

// validateAgent checks required fields for an AgentFile.
func validateAgent(a *AgentFile) error {
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
