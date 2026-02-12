package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

type ChatConversationHistorySuite struct {
	testutil.BaseSuite
}

func TestChatConversationHistorySuite(t *testing.T) {
	suite.Run(t, new(ChatConversationHistorySuite))
}

func (s *ChatConversationHistorySuite) SetupSuite() {
	s.SetDBSuffix("chat_history")
	s.BaseSuite.SetupSuite()
}

func (s *ChatConversationHistorySuite) TestMultiTurnConversation_BuildsContextSummary() {
	conv := s.createTestConversation("Multi-turn Chat", "Hello, what is your name?")

	s.addMessage(conv["id"].(string), "assistant", "I am an AI assistant. How can I help you?")
	s.addMessage(conv["id"].(string), "user", "What is the weather like?")
	s.addMessage(conv["id"].(string), "assistant", "I don't have access to weather data.")
	s.addMessage(conv["id"].(string), "user", "Can you summarize our conversation?")

	resp := s.Client.GET("/api/v2/chat/"+conv["id"].(string),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	messages := body["messages"].([]any)
	s.Len(messages, 5)

	lastMsg := messages[4].(map[string]any)
	s.NotNil(lastMsg["contextSummary"])
	contextSummary := lastMsg["contextSummary"].(string)
	s.Contains(contextSummary, "Previous conversation:")
	s.Contains(contextSummary, "user:")
	s.Contains(contextSummary, "assistant:")
}

func (s *ChatConversationHistorySuite) TestConversationHistory_Limit() {
	conv := s.createTestConversation("History Limit Test", "Message 1")

	for i := 2; i <= 10; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		s.addMessage(conv["id"].(string), role, fmt.Sprintf("Message %d", i))
	}

	s.addMessage(conv["id"].(string), "user", "Message 11")

	resp := s.Client.GET("/api/v2/chat/"+conv["id"].(string),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	messages := body["messages"].([]any)
	s.Len(messages, 11)

	lastMsg := messages[10].(map[string]any)
	if lastMsg["contextSummary"] != nil {
		contextSummary := lastMsg["contextSummary"].(string)

		msgCount := 0
		for i := 0; i < len(contextSummary); i++ {
			if i > 0 && contextSummary[i-1:i+1] == "\n" {
				msgCount++
			}
		}
		s.LessOrEqual(msgCount, 10)
	}
}

func (s *ChatConversationHistorySuite) TestConversationHistory_EmptyForFirstMessage() {
	conv := s.createTestConversation("First Message Test", "Hello")

	resp := s.Client.GET("/api/v2/chat/"+conv["id"].(string),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	messages := body["messages"].([]any)
	s.Len(messages, 1)

	firstMsg := messages[0].(map[string]any)
	s.Nil(firstMsg["contextSummary"])
}

func (s *ChatConversationHistorySuite) TestConversationHistory_ProjectIsolation() {
	project2ID := s.createProjectViaAPI("Second Project")

	conv := s.createTestConversation("Project 1 Chat", "Hello from project 1")
	s.addMessage(conv["id"].(string), "assistant", "Response in project 1")

	req := map[string]any{
		"role":    "user",
		"content": "Another message",
	}

	resp := s.Client.POST("/api/v2/chat/"+conv["id"].(string)+"/messages",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(project2ID),
		testutil.WithJSONBody(req),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ChatConversationHistorySuite) TestConversationHistory_ChronologicalOrder() {
	conv := s.createTestConversation("Order Test", "Message 1")
	s.addMessage(conv["id"].(string), "assistant", "Message 2")
	s.addMessage(conv["id"].(string), "user", "Message 3")

	resp := s.Client.GET("/api/v2/chat/"+conv["id"].(string),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	messages := body["messages"].([]any)
	s.Len(messages, 3)

	s.Equal("Message 1", messages[0].(map[string]any)["content"])
	s.Equal("Message 2", messages[1].(map[string]any)["content"])
	s.Equal("Message 3", messages[2].(map[string]any)["content"])

	lastMsg := messages[2].(map[string]any)
	if lastMsg["contextSummary"] != nil {
		contextSummary := lastMsg["contextSummary"].(string)
		s.Contains(contextSummary, "Message 1")
		s.Contains(contextSummary, "Message 2")
	}
}

func (s *ChatConversationHistorySuite) createTestConversation(title, message string) map[string]any {
	req := map[string]any{
		"title":   title,
		"message": message,
	}

	resp := s.Client.POST("/api/v2/chat/conversations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(req),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create conversation")

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	return body
}

func (s *ChatConversationHistorySuite) addMessage(conversationID, role, content string) map[string]any {
	req := map[string]any{
		"role":    role,
		"content": content,
	}

	resp := s.Client.POST("/api/v2/chat/"+conversationID+"/messages",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(req),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	return body
}

func (s *ChatConversationHistorySuite) createProjectViaAPI(name string) string {
	req := map[string]any{
		"name":  name,
		"orgId": s.OrgID,
	}

	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(req),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create project: %s", resp.String())

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	return body["id"].(string)
}
