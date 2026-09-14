package blueprints

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// LoadEnvFiles reads ".env" then ".env.local" from dir (if they exist) and
// returns a merged map of key→value pairs. ".env.local" values override ".env".
// Neither file's absence is an error. Shell environment variables are NOT
// automatically merged here — callers combine via ExpandEnvVars.
func LoadEnvFiles(dir string) map[string]string {
	vars := make(map[string]string)
	for _, name := range []string{".env", ".env.local"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // missing file is fine
		}
		parseEnvFileInto(data, vars)
	}
	return vars
}

// parseEnvFileInto parses a .env file (KEY=VALUE pairs, # comments, blank
// lines) and merges results into dst. Values may be single- or double-quoted;
// surrounding quotes are stripped. Inline # comments on unquoted values are
// stripped. No variable interpolation is performed at parse time.
func parseEnvFileInto(data []byte, dst map[string]string) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Lines may start with "export " — strip it.
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue // no '=' — skip
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		val = stripEnvValue(val)
		dst[key] = val
	}
}

// stripEnvValue unquotes a raw env value and strips inline comments.
//
// Rules:
//   - Double-quoted:  "value"  -> value  (no comment stripping inside)
//   - Single-quoted:  'value'  -> value  (no comment stripping inside)
//   - Unquoted:       value # comment -> value
func stripEnvValue(raw string) string {
	if len(raw) >= 2 {
		if raw[0] == '"' && raw[len(raw)-1] == '"' {
			return raw[1 : len(raw)-1]
		}
		if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
			return raw[1 : len(raw)-1]
		}
	}
	// Unquoted: strip trailing inline comment.
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	return raw
}

// ExpandEnvVars replaces ${VAR} and $VAR references in src with values looked
// up first in fileVars (from .env / .env.local), then in the shell environment
// via os.Getenv. Unresolved references are left as-is.
func ExpandEnvVars(src []byte, fileVars map[string]string) []byte {
	if len(fileVars) == 0 && !bytes.ContainsAny(src, "$") {
		return src
	}
	result := os.Expand(string(src), func(key string) string {
		if v, ok := fileVars[key]; ok {
			return v
		}
		return os.Getenv(key)
	})
	return []byte(result)
}
