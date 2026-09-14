package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAgentQuestionsCommands verifies that the questions commands are properly registered
func TestAgentQuestionsCommands(t *testing.T) {
	// Verify questions command exists
	assert.NotNil(t, questionsCmd, "questions command should be registered")
	assert.Equal(t, "questions", questionsCmd.Use)

	// Verify subcommands
	commands := questionsCmd.Commands()
	assert.Len(t, commands, 3, "questions command should have 3 subcommands")

	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		commandNames[cmd.Name()] = true
	}

	assert.True(t, commandNames["list"], "should have list subcommand")
	assert.True(t, commandNames["list-project"], "should have list-project subcommand")
	assert.True(t, commandNames["respond"], "should have respond subcommand")
}

// TestListQuestionsCommandArgs verifies that list command requires exactly 1 arg
func TestListQuestionsCommandArgs(t *testing.T) {
	assert.NotNil(t, listQuestionsCmd)
	assert.Equal(t, "list [run-id]", listQuestionsCmd.Use)

	// Verify Args validation
	err := listQuestionsCmd.Args(listQuestionsCmd, []string{})
	assert.Error(t, err, "should require exactly 1 argument")

	err = listQuestionsCmd.Args(listQuestionsCmd, []string{"run_test123"})
	assert.NoError(t, err, "should accept 1 argument")

	err = listQuestionsCmd.Args(listQuestionsCmd, []string{"run_test123", "extra"})
	assert.Error(t, err, "should reject more than 1 argument")
}

// TestListProjectQuestionsCommand verifies the list-project command
func TestListProjectQuestionsCommand(t *testing.T) {
	assert.NotNil(t, listProjectQuestionsCmd)
	assert.Equal(t, "list-project", listProjectQuestionsCmd.Use)

	// Verify it has status flag
	statusFlag := listProjectQuestionsCmd.Flags().Lookup("status")
	assert.NotNil(t, statusFlag, "should have status flag")
	assert.Equal(t, "", statusFlag.DefValue, "status flag should default to empty string")
}

// TestRespondToQuestionCommandArgs verifies that respond command requires exactly 2 args
func TestRespondToQuestionCommandArgs(t *testing.T) {
	assert.NotNil(t, respondToQuestionCmd)
	assert.Equal(t, "respond [question-id] [response]", respondToQuestionCmd.Use)

	// Verify Args validation
	err := respondToQuestionCmd.Args(respondToQuestionCmd, []string{})
	assert.Error(t, err, "should require exactly 2 arguments")

	err = respondToQuestionCmd.Args(respondToQuestionCmd, []string{"question_test123"})
	assert.Error(t, err, "should require exactly 2 arguments")

	err = respondToQuestionCmd.Args(respondToQuestionCmd, []string{"question_test123", "Blue"})
	assert.NoError(t, err, "should accept 2 arguments")

	err = respondToQuestionCmd.Args(respondToQuestionCmd, []string{"question_test123", "Blue", "extra"})
	assert.Error(t, err, "should reject more than 2 arguments")
}

// TestAgentQuestionsRegistration verifies questions command is registered under agents
func TestAgentQuestionsRegistration(t *testing.T) {
	agentCommands := agentsCmd.Commands()
	var questionsFound bool
	for _, cmd := range agentCommands {
		if cmd.Name() == "questions" {
			questionsFound = true
			break
		}
	}

	assert.True(t, questionsFound, "questions command should be registered under agents command")
}
