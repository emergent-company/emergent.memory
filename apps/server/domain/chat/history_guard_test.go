package chat_test

import (
	"log/slog"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/chat"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestGetConversationHistoryOwnershipGuard directly exercises the exfiltration
// step the stream path uses (repo.GetConversationHistory, handler.go stream
// path): a foreign caller must get zero messages for a private conversation,
// while the owner gets them.
func (s *ChatOwnershipSuite) TestGetConversationHistoryOwnershipGuard() {
	convUUID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, NOW(), NOW())
	`, convUUID, "private-owner-secret", s.ProjectID, testutil.AdminUser.ID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_messages (id, conversation_id, role, content, created_at)
		VALUES (?, ?, 'user', ?, NOW())
	`, uuid.New(), convUUID, "private-message-secret").Exec(s.Ctx)
	s.Require().NoError(err)

	repo := chat.NewRepository(s.DB(), slog.Default())

	foreign, err := repo.GetConversationHistory(s.Ctx, s.ProjectID, s.userBID, convUUID, 10)
	s.Require().NoError(err)
	s.Require().Len(foreign, 0, "foreign caller must not read a private conversation's history")

	owner, err := repo.GetConversationHistory(s.Ctx, s.ProjectID, testutil.AdminUser.ID, convUUID, 10)
	s.Require().NoError(err)
	s.Require().Len(owner, 1, "owner must read their own history")
	s.Require().Equal("private-message-secret", owner[0].Content)
}
