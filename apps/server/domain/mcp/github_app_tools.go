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
			Description:  "Read the diff and changed files for one commit SHA of a GitHub repository connected to this project. Review the exact SHA named by the review subject; do not assume the PR's current head.",
			OutputSchema: objectOutputSchema(),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"repo":     {Type: "string", Description: "Repository as owner/name"},
					"head_sha": {Type: "string", Description: "Commit SHA to review (the reviewed head)"},
				},
				Required: []string{"repo", "head_sha"},
			},
		},
		{
			Name:         "github-review-submit",
			AgentOnly:    true,
			Description:  "Submit a review verdict for a GitHub pull request at an exact commit SHA. Posts the review, a head-bound `reviewed` check, and the `Reviewed-SHA` merge-guard marker. Fails closed if the PR head moved or the diff was not read in this run.",
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
