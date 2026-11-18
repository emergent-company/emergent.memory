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
			Description:  "Read the diff and changed files for one commit SHA of a GitHub repository connected to this project. Returns the full file index plus a slice of the patch text; when the response ends with `next_offset=N` (N>0), call again with offset=N to read on. Review the exact SHA named by the review subject; do not assume the PR's current head.",
			OutputSchema: objectOutputSchema(),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"repo":     {Type: "string", Description: "Repository as owner/name"},
					"head_sha": {Type: "string", Description: "Commit SHA to review (the reviewed head)"},
					"offset":   {Type: "number", Description: "Byte offset into the patch text to start reading from (default 0). Use the offset the previous page reported; the tool will not approve a verdict until every page has been read."},
					"limit":    {Type: "number", Description: "Maximum patch bytes in this response (default 80000, max 100000); the page is further capped so the whole response fits the model's tool-result bound."},
					"path":     {Type: "string", Description: "Optional substring filter on changed file names, to read one file or directory in full. A filtered read is not review evidence, so read the unfiltered diff for the reviewed SHA before submitting a verdict."},
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
