package agents

import (
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// WorkTerminatorSubmitted is the terminator kind recorded by the mini-mode
// submit tool. beforeModelCb finalizes the run on the first terminator call.
const WorkTerminatorSubmitted = "submitted"

// buildSubmitTool returns the mini-mode run terminator. It lets the agent end
// the run explicitly after it has pushed its work, instead of looping until the
// step limit. The first call wins; later calls are ignored by the terminator.
func buildSubmitTool(terminator *WorkTerminatorState) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "submit",
			Description: `Finish the task and end the run. Call this once, after you have
committed and pushed your changes and verified the result. Passing a short
summary is recommended. Do not call it before your work is saved and pushed;
after this call the run stops and cannot be resumed.`,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"summary": {Type: "string", Description: "Short summary of what was done and how it was verified."},
				},
			},
		},
		func(_ agent.Context, args map[string]any) (map[string]any, error) {
			summary, _ := args["summary"].(string)
			finalizeSubmit(terminator, summary)
			return map[string]any{
				"status":  "submitted",
				"message": "Task submitted. The run will end after this turn.",
			}, nil
		},
	)
}

// finalizeSubmit records the mini-mode submission on the run terminator. The
// first call wins (later submit calls are ignored), matching work_complete.
func finalizeSubmit(terminator *WorkTerminatorState, summary string) {
	terminator.Finalize(WorkTerminatorSubmitted, map[string]any{
		"work_terminator": WorkTerminatorSubmitted,
		"submission":      summary,
	})
}
