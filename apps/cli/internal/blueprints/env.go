package blueprints

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/envfile"
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
		maps.Copy(vars, envfile.ReadAll(data))
	}
	return vars
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
