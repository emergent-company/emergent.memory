package blueprints

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDir walks the packs/, agents/, skills/, seed/objects/, and
// seed/relationships/ subdirectories inside dir and returns all successfully
// parsed records.
//
// Skills follow the agentskills.io open standard: each skill is a subdirectory
// containing a SKILL.md file with YAML frontmatter and Markdown content.
//
// Files with unknown extensions are silently skipped.
// Files that fail to parse or fail validation are recorded in the returned
// results with BlueprintsActionError — processing continues for remaining files.
// Missing subdirectories are not an error.
func LoadDir(dir string, envVars map[string]string) (
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	objects []SeedObjectRecord,
	rels []SeedRelationshipRecord,
	results []BlueprintsResult,
	err error,
) {
	packs, packResults := loadPacks(filepath.Join(dir, "packs"), envVars)
	agents, agentResults := loadAgents(filepath.Join(dir, "agents"), envVars)
	skills, skillResults := loadSkills(filepath.Join(dir, "skills"), envVars)
	objects, objResults := loadSeedObjects(filepath.Join(dir, "seed", "objects"), envVars)
	rels, relResults := loadSeedRelationships(filepath.Join(dir, "seed", "relationships"), envVars)
	results = append(packResults, agentResults...)
	results = append(results, skillResults...)
	results = append(results, objResults...)
	results = append(results, relResults...)
	return packs, agents, skills, objects, rels, results, nil
}

// ──────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────

func loadPacks(dir string, envVars map[string]string) ([]PackFile, []BlueprintsResult) {
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
		if err := decodeFile(path, ext, envVars, &pack); err != nil {
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

func loadAgents(dir string, envVars map[string]string) ([]AgentFile, []BlueprintsResult) {
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
		if err := decodeFile(path, ext, envVars, &agent); err != nil {
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

// loadSkills reads all skill subdirectories inside dir. Each subdirectory must
// contain a SKILL.md file with YAML frontmatter (name, description) followed by
// Markdown content. This follows the agentskills.io open standard where a skill
// is a directory containing SKILL.md (plus optional scripts/, references/, assets/).
func loadSkills(dir string, envVars map[string]string) ([]SkillFile, []BlueprintsResult) {
	entries, ok := readDir(dir)
	if !ok {
		return nil, nil
	}

	var skills []SkillFile
	var results []BlueprintsResult

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // only process subdirectories
		}
		skillDir := filepath.Join(dir, entry.Name())
		skillMD := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillMD)
		if err != nil {
			if os.IsNotExist(err) {
				// subdirectory has no SKILL.md — silently skip
				continue
			}
			results = append(results, BlueprintsResult{
				ResourceType: "skill",
				Name:         entry.Name(),
				SourceFile:   skillMD,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("read SKILL.md: %w", err),
			})
			continue
		}

		data = ExpandEnvVars(data, envVars)
		skill, err := parseSkillMD(data)
		if err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "skill",
				Name:         entry.Name(),
				SourceFile:   skillMD,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("parse error: %w", err),
			})
			continue
		}
		skill.SourceFile = skillMD

		if err := validateSkill(skill); err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "skill",
				Name:         skill.Name,
				SourceFile:   skillMD,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("validation error: %w", err),
			})
			continue
		}

		skills = append(skills, *skill)
	}

	return skills, results
}

// parseSkillMD splits a SKILL.md file into YAML frontmatter and Markdown body.
// The file must start with "---\n", contain YAML, and close with "---\n" or "---".
func parseSkillMD(data []byte) (*SkillFile, error) {
	const delim = "---"

	trimmed := bytes.TrimSpace(data)
	if !bytes.HasPrefix(trimmed, []byte(delim)) {
		return nil, fmt.Errorf("file does not begin with YAML frontmatter (expected '---')")
	}

	// Skip past the opening ---
	rest := trimmed[len(delim):]
	if len(rest) > 0 && rest[0] == '\r' {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}

	// Find the closing ---
	closeIdx := bytes.Index(rest, []byte("\n"+delim))
	if closeIdx < 0 {
		return nil, fmt.Errorf("frontmatter closing '---' not found")
	}

	yamlPart := rest[:closeIdx]
	after := rest[closeIdx+1+len(delim):]
	after = bytes.TrimPrefix(after, []byte("\r"))
	after = bytes.TrimPrefix(after, []byte("\n"))

	var sf SkillFile
	if err := yaml.Unmarshal(yamlPart, &sf); err != nil {
		return nil, fmt.Errorf("failed to parse YAML frontmatter: %w", err)
	}
	sf.Content = string(after)

	return &sf, nil
}

// validateSkill checks required fields for a SkillFile.
func validateSkill(s *SkillFile) error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.Description == "" {
		return fmt.Errorf("description is required")
	}
	return nil
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
func decodeFile(path, ext string, envVars map[string]string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	data = ExpandEnvVars(data, envVars)

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

// ──────────────────────────────────────────────
// Seed loaders
// ──────────────────────────────────────────────

// loadSeedObjects reads all *.jsonl files (including split files *.001.jsonl,
// *.002.jsonl, …) from dir and returns parsed SeedObjectRecord values.
// Missing directory is not an error.
func loadSeedObjects(dir string, envVars map[string]string) ([]SeedObjectRecord, []BlueprintsResult) {
	return loadSeedJSONL(dir, envVars, func(line []byte, path string) (SeedObjectRecord, error) {
		var rec SeedObjectRecord
		if err := decodeJSONLine(line, &rec); err != nil {
			return rec, err
		}
		rec.SourceFile = path
		return rec, validateSeedObject(&rec)
	})
}

// loadSeedRelationships reads all *.jsonl files from dir and returns parsed
// SeedRelationshipRecord values. Missing directory is not an error.
func loadSeedRelationships(dir string, envVars map[string]string) ([]SeedRelationshipRecord, []BlueprintsResult) {
	return loadSeedJSONL(dir, envVars, func(line []byte, path string) (SeedRelationshipRecord, error) {
		var rec SeedRelationshipRecord
		if err := decodeJSONLine(line, &rec); err != nil {
			return rec, err
		}
		rec.SourceFile = path
		return rec, validateSeedRelationship(&rec)
	})
}

// loadSeedJSONL is the generic JSONL reader. It reads all *.jsonl files in dir
// (sorted, so split files arrive in order), calls parse for each non-empty line,
// and accumulates results and errors.
func loadSeedJSONL[T any](dir string, envVars map[string]string, parse func([]byte, string) (T, error)) ([]T, []BlueprintsResult) {
	entries, ok := readDir(dir)
	if !ok {
		return nil, nil
	}

	var records []T
	var results []BlueprintsResult

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, name)

		data, err := os.ReadFile(path)
		if err != nil {
			results = append(results, BlueprintsResult{
				ResourceType: "seed",
				Name:         name,
				SourceFile:   path,
				Action:       BlueprintsActionError,
				Error:        fmt.Errorf("read file: %w", err),
			})
			continue
		}

		lineNum := 0
		for _, raw := range splitLines(data) {
			lineNum++
			if len(raw) == 0 {
				continue
			}
			raw = ExpandEnvVars(raw, envVars)
			rec, err := parse(raw, path)
			if err != nil {
				results = append(results, BlueprintsResult{
					ResourceType: "seed",
					Name:         fmt.Sprintf("%s:%d", name, lineNum),
					SourceFile:   path,
					Action:       BlueprintsActionError,
					Error:        err,
				})
				continue
			}
			records = append(records, rec)
		}
	}

	return records, results
}

// splitLines splits data on newlines, trimming carriage returns.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := bytes.TrimRight(data[start:i], "\r")
			lines = append(lines, line)
			start = i + 1
		}
	}
	// trailing line without newline
	if start < len(data) {
		line := bytes.TrimRight(data[start:], "\r")
		lines = append(lines, line)
	}
	return lines
}

// decodeJSONLine unmarshals a single JSON line into v.
func decodeJSONLine(line []byte, v any) error {
	if err := json.Unmarshal(line, v); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	return nil
}

// validateSeedObject checks required fields for a SeedObjectRecord.
func validateSeedObject(r *SeedObjectRecord) error {
	if r.Type == "" {
		return fmt.Errorf("type is required")
	}
	return nil
}

// validateSeedRelationship checks required fields for a SeedRelationshipRecord.
func validateSeedRelationship(r *SeedRelationshipRecord) error {
	if r.Type == "" {
		return fmt.Errorf("type is required")
	}
	hasSrcKey := r.SrcKey != ""
	hasDstKey := r.DstKey != ""
	hasSrcID := r.SrcID != ""
	hasDstID := r.DstID != ""
	if !((hasSrcKey && hasDstKey) || (hasSrcID && hasDstID)) {
		return fmt.Errorf("relationship must have either (srcKey+dstKey) or (srcId+dstId)")
	}
	return nil
}
