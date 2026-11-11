package main

import "testing"

func TestMCPInstallURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   string
		mcpURL string
		token  string
		want   string
	}{
		{
			name:   "escapes the token and endpoint and sorts the query",
			base:   "https://mcp.example.com",
			mcpURL: "https://mcp.example.com/api/mcp/agents/a1",
			token:  "emt_secret&once",
			want:   "https://mcp.example.com/api/mcp/install?name=memory&token=emt_secret%26once&url=https%3A%2F%2Fmcp.example.com%2Fapi%2Fmcp%2Fagents%2Fa1",
		},
		{
			name:   "trims a trailing slash from the base",
			base:   "https://mcp.example.com/",
			mcpURL: "https://mcp.example.com/api/mcp",
			token:  "emt_key",
			want:   "https://mcp.example.com/api/mcp/install?name=memory&token=emt_key&url=https%3A%2F%2Fmcp.example.com%2Fapi%2Fmcp",
		},
		{
			name:   "omits the link when any part is missing",
			base:   "https://mcp.example.com",
			mcpURL: "",
			token:  "emt_key",
			want:   "",
		},
		{
			name:   "omits the link without a base",
			base:   " ",
			mcpURL: "https://mcp.example.com/api/mcp",
			token:  "emt_key",
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mcpInstallURL(tc.base, tc.mcpURL, tc.token); got != tc.want {
				t.Fatalf("mcpInstallURL(%q, %q, %q) = %q, want %q", tc.base, tc.mcpURL, tc.token, got, tc.want)
			}
		})
	}
}
