package mcp_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// SessionTodoToolsOwnershipSuite proves that, once wired, the MCP session-todo
// tools enforce the shared conversation-ownership predicate (#1010) through the
// same data-access path as the REST surface: a foreign member's session is
// refused (404, no existence oracle) while the owner still succeeds. The tools
// were dead code before issue #1051 item (a); this pins the enforcement on the
// wired dispatch path so a future regression cannot silently bypass it.
type SessionTodoToolsOwnershipSuite struct {
	testutil.BaseSuite

	mcpSvc *mcp.Service
}

func TestSessionTodoToolsOwnershipSuite(t *testing.T) {
	suite.Run(t, new(SessionTodoToolsOwnershipSuite))
}

func (s *SessionTodoToolsOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("mcp_session_todo_tools")
	s.BaseSuite.SetupSuite()
}

func (s *SessionTodoToolsOwnershipSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	log := slog.Default()
	todoSvc := sessiontodos.NewService(sessiontodos.NewRepository(s.DB(), log), log)
	s.mcpSvc = mcp.NewService(mcp.ServiceParams{
		DB:             s.DB(),
		Cfg:            s.TestDB.Config,
		Log:            log,
		SessionTodoSvc: todoSvc,
	})
	// Populate the tool index ExecuteTool's authority gate reads, and surface a
	// registration regression loudly.
	_ = s.mcpSvc.GetToolDefinitions()
}

// seedSession inserts a private ACP session + chat conversation owned by ownerID
// with one todo, returning the session and todo ids.
func (s *SessionTodoToolsOwnershipSuite) seedSession(ownerID string) (string, string) {
	sessionID := uuid.New()
	todoID := uuid.New()
	convID := uuid.New()

	_, err := s.DB().NewRaw(`
		INSERT INTO kb.acp_sessions (id, project_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, sessionID, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, acp_session_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, ?, NOW(), NOW())
	`, convID, "owner-secret", s.ProjectID, ownerID, sessionID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.session_todos (id, session_id, content, status, "order", created_at, updated_at)
		VALUES (?, ?, ?, 'draft', 0, NOW(), NOW())
	`, todoID, sessionID, "secret-todo").Exec(s.Ctx)
	s.Require().NoError(err)

	return sessionID.String(), todoID.String()
}

func ownerCtx() context.Context {
	return auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: testutil.AdminUser.ID})
}

func foreignCtx() context.Context {
	return auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: testutil.RegularUser.ID})
}

// assertNotFound404 pins the refusal to the ownership predicate's fail-closed
// 404 (not a transport/scope/unknown-tool error).
func (s *SessionTodoToolsOwnershipSuite) assertNotFound404(err error) {
	s.Require().Error(err)
	appErr, ok := err.(*apperror.Error)
	s.Require().Truef(ok, "expected *apperror.Error, got %T: %v", err, err)
	s.Require().Equal(404, appErr.HTTPStatus,
		"foreign session must fail closed to 404, got %d: %v", appErr.HTTPStatus, err)
}

func (s *SessionTodoToolsOwnershipSuite) TestListForeignSessionRefused() {
	sessionID, _ := s.seedSession(testutil.AdminUser.ID)
	_, err := s.mcpSvc.ExecuteTool(foreignCtx(), s.ProjectID, "session-todo-list",
		map[string]any{"session_id": sessionID})
	s.assertNotFound404(err)
}

func (s *SessionTodoToolsOwnershipSuite) TestListOwnerSucceeds() {
	sessionID, _ := s.seedSession(testutil.AdminUser.ID)
	result, err := s.mcpSvc.ExecuteTool(ownerCtx(), s.ProjectID, "session-todo-list",
		map[string]any{"session_id": sessionID})
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().NotNil(result.StructuredContent)
}

func (s *SessionTodoToolsOwnershipSuite) TestUpdateForeignSessionRefused() {
	sessionID, todoID := s.seedSession(testutil.AdminUser.ID)
	_, err := s.mcpSvc.ExecuteTool(foreignCtx(), s.ProjectID, "session-todo-update",
		map[string]any{"session_id": sessionID, "todo_id": todoID, "status": "completed"})
	s.assertNotFound404(err)
}

func (s *SessionTodoToolsOwnershipSuite) TestUpdateOwnerSucceeds() {
	sessionID, todoID := s.seedSession(testutil.AdminUser.ID)
	result, err := s.mcpSvc.ExecuteTool(ownerCtx(), s.ProjectID, "session-todo-update",
		map[string]any{"session_id": sessionID, "todo_id": todoID, "status": "completed"})
	s.Require().NoError(err)
	s.Require().NotNil(result)
}
