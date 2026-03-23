package sdk

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// envConfig holds discovered configuration from all sources.
type envConfig struct {
	ServerURL    string
	APIKey       string
	OrgID        string
	ProjectID    string
	ProjectToken string
}

// loadEnvConfig discovers configuration from multiple sources in priority order.
// Higher-priority sources overwrite lower-priority ones.
func loadEnvConfig() envConfig {
	cfg := envConfig{}

	// 1. ~/.memory/config.yaml (lowest priority)
	if home, err := os.UserHomeDir(); err == nil {
		yamlPath := filepath.Join(home, ".memory", "config.yaml")
		if raw, err := parseSimpleYAML(yamlPath); err == nil {
			applyYAMLConfig(&cfg, raw)
		}
	}

	// 2. .env (walk up from cwd)
	if p := walkUpFind(".env"); p != "" {
		if raw, err := parseDotenv(p); err == nil {
			applyEnvMap(&cfg, raw)
		}
	}

	// 3. .env.local (walk up from cwd, overrides .env)
	if p := walkUpFind(".env.local"); p != "" {
		if raw, err := parseDotenv(p); err == nil {
			applyEnvMap(&cfg, raw)
		}
	}

	// 4. Actual environment variables (highest priority)
	applyEnvMap(&cfg, nil) // nil means read from os.Getenv

	return cfg
}

// walkUpFind searches for filename starting from cwd, walking up to root.
func walkUpFind(filename string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// parseDotenv parses a .env file into a map. Handles KEY=VALUE and KEY="VALUE".
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		idx := strings.Index(line, "=")
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result, scanner.Err()
}

// parseSimpleYAML parses a simple flat YAML file (key: value lines only).
// This avoids importing a YAML library.
func parseSimpleYAML(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, ":") {
			continue
		}
		// Only process lines that are not indented
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		idx := strings.Index(line, ":")
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if val != "" && !strings.HasPrefix(val, "{") && !strings.HasPrefix(val, "[") {
			result[key] = val
		}
	}
	return result, scanner.Err()
}

// applyYAMLConfig maps YAML config keys to envConfig fields.
func applyYAMLConfig(cfg *envConfig, raw map[string]string) {
	if v := raw["server_url"]; v != "" {
		cfg.ServerURL = v
	}
	if v := raw["api_key"]; v != "" {
		cfg.APIKey = v
	}
	if v := raw["org_id"]; v != "" {
		cfg.OrgID = v
	}
	if v := raw["project_id"]; v != "" {
		cfg.ProjectID = v
	}
	if v := raw["project_token"]; v != "" {
		cfg.ProjectToken = v
	}
}

// applyEnvMap applies a map of env vars (or os.Getenv if map is nil) to cfg.
func applyEnvMap(cfg *envConfig, m map[string]string) {
	get := func(key string) string {
		if m != nil {
			return m[key]
		}
		return os.Getenv(key)
	}

	if v := get("MEMORY_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	} else if v := get("MEMORY_API_URL"); v != "" && cfg.ServerURL == "" {
		cfg.ServerURL = v
	}
	if v := get("MEMORY_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := get("MEMORY_ORG_ID"); v != "" {
		cfg.OrgID = v
	}
	if v := get("MEMORY_PROJECT_ID"); v != "" {
		cfg.ProjectID = v
	}
	if v := get("MEMORY_PROJECT_TOKEN"); v != "" {
		cfg.ProjectToken = v
	}
}
