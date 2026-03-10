package blueprints

import (
	"os"
	"testing"
)

func TestLoadEnvFiles_BasicParsing(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(dir+"/.env", []byte(`
# comment line
BRAVE_SEARCH_API_KEY=
REDDIT_CLIENT_ID=from_env_file
QUOTED_DOUBLE="double quoted"
QUOTED_SINGLE='single quoted'
WITH_COMMENT=value # inline comment
`), 0644)
	os.WriteFile(dir+"/.env.local", []byte(`
BRAVE_SEARCH_API_KEY=mybravekey123
`), 0644)

	vars := LoadEnvFiles(dir)

	cases := []struct{ key, want string }{
		{"BRAVE_SEARCH_API_KEY", "mybravekey123"},   // .env.local overrides .env
		{"REDDIT_CLIENT_ID", "from_env_file"},        // from .env
		{"QUOTED_DOUBLE", "double quoted"},            // quotes stripped
		{"QUOTED_SINGLE", "single quoted"},            // quotes stripped
		{"WITH_COMMENT", "value"},                     // inline comment stripped
	}
	for _, c := range cases {
		got := vars[c.key]
		if got != c.want {
			t.Errorf("LoadEnvFiles[%q] = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestExpandEnvVars_SubstitutesFromMap(t *testing.T) {
	fileVars := map[string]string{
		"API_KEY":   "secret123",
		"CLIENT_ID": "myid",
	}

	src := []byte("api_key: ${API_KEY}\nclient_id: $CLIENT_ID\nunchanged: ${MISSING_VAR}")
	os.Unsetenv("MISSING_VAR")
	expanded := ExpandEnvVars(src, fileVars)

	got := string(expanded)
	if got != "api_key: secret123\nclient_id: myid\nunchanged: " {
		t.Errorf("unexpected expansion:\n%s", got)
	}
}

func TestExpandEnvVars_FallsBackToOsEnv(t *testing.T) {
	t.Setenv("SHELL_VAR", "fromshell")
	fileVars := map[string]string{} // empty — should fall back

	src := []byte("value: ${SHELL_VAR}")
	expanded := ExpandEnvVars(src, fileVars)

	if string(expanded) != "value: fromshell" {
		t.Errorf("got %q, want %q", string(expanded), "value: fromshell")
	}
}
