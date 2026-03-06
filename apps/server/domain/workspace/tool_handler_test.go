package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGrepOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []GrepMatch
	}{
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "   \n  \n",
			expected: nil,
		},
		{
			name:  "single match",
			input: "/workspace/src/main.go:42:func main() {",
			expected: []GrepMatch{
				{FilePath: "/workspace/src/main.go", LineNumber: 42, Line: "func main() {"},
			},
		},
		{
			name:  "multiple matches",
			input: "/workspace/src/main.go:1:package main\n/workspace/src/main.go:3:import \"fmt\"\n/workspace/src/util.go:10:func helper() {}",
			expected: []GrepMatch{
				{FilePath: "/workspace/src/main.go", LineNumber: 1, Line: "package main"},
				{FilePath: "/workspace/src/main.go", LineNumber: 3, Line: "import \"fmt\""},
				{FilePath: "/workspace/src/util.go", LineNumber: 10, Line: "func helper() {}"},
			},
		},
		{
			name:  "line with colons in content",
			input: "/workspace/config.yaml:5:database: host:port:5432",
			expected: []GrepMatch{
				{FilePath: "/workspace/config.yaml", LineNumber: 5, Line: "database: host:port:5432"},
			},
		},
		{
			name:     "malformed line - no colon",
			input:    "no-colon-here",
			expected: nil,
		},
		{
			name:     "malformed line - one colon no line number",
			input:    "file:content",
			expected: nil,
		},
		{
			name:     "line number zero is skipped",
			input:    "/workspace/file.go:0:zero line",
			expected: nil,
		},
		{
			name:  "trailing newline",
			input: "/workspace/a.go:1:test\n",
			expected: []GrepMatch{
				{FilePath: "/workspace/a.go", LineNumber: 1, Line: "test"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGrepOutput(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.Len(t, result, len(tt.expected))
				for i, exp := range tt.expected {
					assert.Equal(t, exp.FilePath, result[i].FilePath)
					assert.Equal(t, exp.LineNumber, result[i].LineNumber)
					assert.Equal(t, exp.Line, result[i].Line)
				}
			}
		})
	}
}

func TestSanitizeGitOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no credentials - unchanged",
			input:    "On branch main\nnothing to commit",
			expected: "On branch main\nnothing to commit",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "https token masked",
			input:    "remote: https://ghp_abc123token@github.com/org/repo.git",
			expected: "remote: https://***@github.com/org/repo.git",
		},
		{
			name:     "http token masked",
			input:    "Pushing to http://x-access-token:ghs_secret@github.com/org/repo",
			expected: "Pushing to http://***@github.com/org/repo",
		},
		{
			name:     "no token - github.com without @",
			input:    "Cloning from https://github.com/org/repo",
			expected: "Cloning from https://github.com/org/repo",
		},
		{
			name:     "multiple lines mixed",
			input:    "line1\nhttps://token@github.com/r\nline3",
			expected: "line1\nhttps://***@github.com/r\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGitOutput(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractToolName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"bash tool", "/api/v1/agent/workspaces/:id/bash", "bash"},
		{"read tool", "/api/v1/agent/workspaces/:id/read", "read"},
		{"write tool", "/api/v1/agent/workspaces/:id/write", "write"},
		{"edit tool", "/api/v1/agent/workspaces/:id/edit", "edit"},
		{"glob tool", "/api/v1/agent/workspaces/:id/glob", "glob"},
		{"grep tool", "/api/v1/agent/workspaces/:id/grep", "grep"},
		{"git tool", "/api/v1/agent/workspaces/:id/git", "git"},
		{"root path", "/", "unknown"},
		{"empty path", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToolName(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{"simple path", "/api/v1/test", []string{"api", "v1", "test"}},
		{"no leading slash", "api/v1/test", []string{"api", "v1", "test"}},
		{"single segment", "/bash", []string{"bash"}},
		{"empty", "", nil},
		{"root", "/", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPath(tt.path)
			if tt.expected == nil {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDefaultBashTimeout(t *testing.T) {
	assert.Equal(t, 120000, defaultBashTimeoutMs, "default bash timeout should be 120000ms (2 minutes)")
}
