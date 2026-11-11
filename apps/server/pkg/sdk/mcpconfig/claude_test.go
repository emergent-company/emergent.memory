package mcpconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClaudeConfigMixed(t *testing.T) {
	data := []byte(`{
		"globalShortcut": "Cmd+Space",
		"mcpServers": {
			"local": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}
			},
			"remote-http": {
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {"Authorization": "Bearer abc"}
			},
			"remote-sse": {
				"type": "sse",
				"url": "https://example.com/sse"
			}
		}
	}`)

	res, err := ParseClaudeConfig(data)
	require.NoError(t, err)
	require.Len(t, res.Servers, 3)
	assert.Empty(t, res.Errors)

	byName := map[string]Server{}
	for _, s := range res.Servers {
		byName[s.Name] = s
	}

	local := byName["local"]
	assert.Equal(t, TransportStdio, local.Transport)
	assert.Equal(t, "npx", local.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, local.Args)
	assert.Equal(t, "Bearer abc", byName["remote-http"].Headers["Authorization"])
	assert.Equal(t, "https://example.com/mcp", byName["remote-http"].URL)
	assert.Equal(t, TransportSSE, byName["remote-sse"].Transport)

	// Sorted by name for deterministic output.
	assert.Equal(t, []string{"local", "remote-http", "remote-sse"}, []string{
		res.Servers[0].Name, res.Servers[1].Name, res.Servers[2].Name,
	})

	// Placeholder warning is emitted for the local entry.
	found := false
	for _, w := range res.Warnings {
		if w.Name == "local" && w.Message != "" {
			found = true
		}
	}
	assert.True(t, found, "expected a placeholder warning for local")
}

func TestParseClaudeConfigTransportAliases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Transport
	}{
		{"streamable-http", `{"url":"https://x","type":"streamable-http"}`, TransportHTTP},
		{"streamableHttp", `{"url":"https://x","type":"streamableHttp"}`, TransportHTTP},
		{"uppercase", `{"url":"https://x","type":"HTTP"}`, TransportHTTP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ParseClaudeConfigWithOptions([]byte(tc.in), ParseOptions{Name: "x"})
			require.NoError(t, err)
			require.Len(t, res.Servers, 1)
			assert.Equal(t, tc.want, res.Servers[0].Transport)
		})
	}
}

func TestParseClaudeConfigInfersTransport(t *testing.T) {
	res, err := ParseClaudeConfigWithOptions([]byte(`{"command":"uvx","args":["pkg"]}`), ParseOptions{Name: "s"})
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, TransportStdio, res.Servers[0].Transport)

	res, err = ParseClaudeConfigWithOptions([]byte(`{"url":"https://x"}`), ParseOptions{Name: "s"})
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, TransportHTTP, res.Servers[0].Transport)
}

func TestParseClaudeConfigEntryErrors(t *testing.T) {
	data := []byte(`{
		"mcpServers": {
			"ok": {"command": "run"},
			"both": {"command": "run", "url": "https://x"},
			"empty": {},
			"bad-type": {"type": "carrier-pigeon", "url": "https://x"},
			"nested-env": {"command": "run", "env": {"X": {"nested": true}}},
			"no-url": {"type": "sse"}
		}
	}`)
	res, err := ParseClaudeConfig(data)
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, "ok", res.Servers[0].Name)
	require.Len(t, res.Errors, 5)
}

func TestParseClaudeConfigSingleServer(t *testing.T) {
	data := []byte(`{"command":"npx","args":["-y","pkg"]}`)

	_, err := ParseClaudeConfig(data)
	require.Error(t, err, "single-server document without a name must fail")

	res, err := ParseClaudeConfigWithOptions(data, ParseOptions{Name: "github"})
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, "github", res.Servers[0].Name)

	// An embedded name is honored when the option is empty.
	res, err = ParseClaudeConfig([]byte(`{"name":"embedded","command":"run"}`))
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, "embedded", res.Servers[0].Name)
}

func TestParseClaudeConfigRejectsBadDocument(t *testing.T) {
	for _, tc := range []string{``, `[]`, `null`, `{"globalShortcut":"x"}`, `{"mcpServers":{}}`, `{"mcpServers":[]}`, `{"mcpServers":null}`} {
		t.Run(tc, func(t *testing.T) {
			_, err := ParseClaudeConfig([]byte(tc))
			require.Error(t, err)
		})
	}
}

func TestParseClaudeConfigScalarCoercion(t *testing.T) {
	data := []byte(`{"mcpServers":{"s":{"command":"run","env":{"A":1,"B":true,"C":"x","D":null}}}}`)
	res, err := ParseClaudeConfig(data)
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	assert.Equal(t, map[string]string{"A": "1", "B": "true", "C": "x"}, res.Servers[0].Env)
}

func TestParseClaudeConfigWarnsOnInapplicableFields(t *testing.T) {
	data := []byte(`{"mcpServers":{
		"stdio-with-headers":{"command":"run","headers":{"X":"y"}},
		"remote-with-env":{"url":"https://x","env":{"X":"y"}}
	}}`)
	res, err := ParseClaudeConfig(data)
	require.NoError(t, err)
	require.Len(t, res.Servers, 2)
	require.Len(t, res.Warnings, 2)
}
