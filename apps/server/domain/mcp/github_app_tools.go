package mcp

// githubAppToolDefinitions is the static catalog entry for the GitHub review
// tools. They are static (not project-conditional) so the builtin tool pool
// seeded by mcpregistry can resolve them for review agents; execution still
// requires an enabled installation and a same-run diff of the exact SHA.
func githubAppToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:         "github-pr-diff",
			AgentOnly:    true,
			Description:  "Read the changed files of a GitHub pull request at one commit SHA (or, without a pull request number, one commit's own diff for context). Review the exact SHA named by the review subject; do not assume the PR's current head. The result marks itself truncated when a patch or the whole payload was cut; re-fetch the files named in truncatedFiles with the path (or files) argument to read them in full before submitting a verdict.",
			OutputSchema: objectOutputSchema(),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"repo":     {Type: "string", Description: "Repository as owner/name"},
					"head_sha": {Type: "string", Description: "Commit SHA to review (the reviewed head)"},
					"number":   {Type: "number", Description: "Pull request number. When omitted, the run's verified review subject supplies it; only a diff with a number is review evidence."},
					"path":     {Type: "string", Description: "Optional changed file path to render on its own, at the whole-result budget. Use it when the previous result marked that file truncated."},
					"files": {
						Type:        "array",
						Description: "Optional changed file paths to render instead of the whole diff, at the whole-result budget.",
						Items:       &PropertySchema{Type: "string"},
					},
				},
				Required: []string{"repo", "head_sha"},
			},
		},
		{
			Name:         "github-review-submit",
			AgentOnly:    true,
			Description:  "Submit a review verdict for a GitHub pull request at an exact commit SHA. Posts the review, a head-bound `reviewed` check, and the `Reviewed-SHA` merge-guard marker. Fails closed if the PR head moved or the full diff of every changed file was not read in this run.",
			OutputSchema: objectOutputSchema(),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"repo":     {Type: "string", Description: "Repository as owner/name"},
					"number":   {Type: "number", Description: "Pull request number"},
					"head_sha": {Type: "string", Description: "Reviewed commit SHA"},
					"verdict":  {Type: "string", Description: "MERGE or CHANGES_REQUESTED", Enum: []string{"MERGE", "CHANGES_REQUESTED"}},
					"summary":  {Type: "string", Description: "Review summary, including blocking findings when rejecting"},
				},
				Required: []string{"repo", "number", "head_sha", "verdict", "summary"},
			},
		},
	}
}
