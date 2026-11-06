package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// Repository handles chat database operations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new chat repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("chat.repo")),
	}
}

// ownerOrSharedScope is the owner-or-shared access predicate every conversation
// accessor applies (see the chat-conversation-ownership capability). prefix
// qualifies the columns for queries that alias the conversation table (e.g.
// "conversation.").
//
// An empty owner means the caller carries no user identity: an ownerless,
// project-bound device credential, which the gateway uses for the iOS session
// log. Such a caller may see only project-shared (non-private) conversations
// and must never be compared against the uuid owner column — the comparison
// `owner_user_id = ”` fails in Postgres with SQLSTATE 22P02.
func ownerOrSharedScope(prefix, ownerUserID string) (string, []any) {
	owner := prefix + "owner_user_id"
	shared := prefix + "is_private = false"
	if ownerUserID == "" {
		return shared, nil
	}
	return "(" + owner + " = ? OR " + shared + ")", []any{ownerUserID}
}

// ListConversations retrieves conversations with pagination
func (r *Repository) ListConversations(ctx context.Context, params ListConversationsParams) (*ListConversationsResult, error) {
	// Default and max limits
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Build query with RLS context via project_id
	query := r.db.NewSelect().
		Model((*Conversation)(nil)).
		Where("project_id = ?", params.ProjectID)

	// Apply the owner-or-shared filter. An ownerless caller (device credential)
	// still gets the project-shared predicate rather than an unfiltered list.
	if params.OwnerUserID != nil {
		condition, args := ownerOrSharedScope("", *params.OwnerUserID)
		query = query.Where(condition, args...)
	}

	// Default-exclude archived conversations unless the caller explicitly opts
	// in. This is applied before the total count so Total reflects the filtered
	// set, not the unfiltered table.
	if !params.IncludeArchived {
		query = query.Where("is_archived = false")
	}

	// Get total count using same filters
	total, err := query.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count conversations: %w", err)
	}

	// Order by updated_at DESC (most recent first)
	query = query.Order("updated_at DESC").
		Offset(params.Offset).
		Limit(params.Limit)

	conversations := []Conversation{}
	if err := query.Scan(ctx, &conversations); err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	// A sub-chat can be newer than the parent that spawned it, so the parent may
	// fall outside the page (or the limit). The rail groups by parent id, and a
	// page that references a parent it does not contain cannot be grouped. Pull
	// in the visible ancestor chain; the query is opt-in so other list callers
	// keep the exact page contract.
	if params.IncludeAncestors && len(conversations) > 0 {
		ancestors, err := r.listMissingAncestors(ctx, params, conversations)
		if err != nil {
			return nil, err
		}
		if len(ancestors) > 0 {
			conversations = append(conversations, ancestors...)
			sort.SliceStable(conversations, func(i, j int) bool {
				return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
			})
		}
	}

	return &ListConversationsResult{
		Conversations: conversations,
		Total:         total,
	}, nil
}

// listMissingAncestors walks parent links from the current page and returns the
// ancestors that are not already present. Each level re-applies the same
// project/owner/archive predicates, so an inaccessible or archived parent is
// never revealed; the depth cap and seen set make a corrupt cycle terminate.
func (r *Repository) listMissingAncestors(ctx context.Context, params ListConversationsParams, page []Conversation) ([]Conversation, error) {
	seen := make(map[uuid.UUID]bool, len(page))
	frontier := make(map[uuid.UUID]bool)
	for _, c := range page {
		seen[c.ID] = true
		if c.ParentConversationID != nil && !seen[*c.ParentConversationID] {
			frontier[*c.ParentConversationID] = true
		}
	}

	var out []Conversation
	for depth := 0; depth < 16 && len(frontier) > 0; depth++ {
		ids := make([]uuid.UUID, 0, len(frontier))
		for id := range frontier {
			ids = append(ids, id)
		}

		query := r.db.NewSelect().
			Model((*Conversation)(nil)).
			Where("id IN (?)", bun.In(ids)).
			Where("project_id = ?", params.ProjectID)
		if params.OwnerUserID != nil {
			condition, args := ownerOrSharedScope("", *params.OwnerUserID)
			query = query.Where(condition, args...)
		}
		if !params.IncludeArchived {
			query = query.Where("is_archived = false")
		}

		var parents []Conversation
		if err := query.Scan(ctx, &parents); err != nil {
			return nil, fmt.Errorf("list conversation ancestors: %w", err)
		}

		next := make(map[uuid.UUID]bool)
		for _, p := range parents {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
			if p.ParentConversationID != nil && !seen[*p.ParentConversationID] {
				next[*p.ParentConversationID] = true
			}
		}
		frontier = next
	}

	return out, nil
}

// GetByID retrieves a conversation by ID, scoped to the caller: the caller
// must own the conversation or it must be non-private (project-shared).
func (r *Repository) GetByID(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	condition, args := ownerOrSharedScope("", ownerUserID)
	err := r.db.NewSelect().
		Model(&conv).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Where(condition, args...).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	return &conv, nil
}

// OwnedConversationIDForRun resolves the conversation a run belongs to via
// kb.agent_runs.session_id -> kb.chat_conversations.session_id, but ONLY when
// that conversation is in projectID and owned by ownerUserID. It returns ""
// for an unknown run, a run with no session/conversation (scheduled or
// background runs), a run in another project, or a conversation owned by
// another member — the caller cannot tell "not yours" from "does not exist".
//
// The strict owner predicate (rather than the owner-or-shared read predicate)
// is deliberate: a project-scoped device credential must not be able to stop a
// run in another member's conversation, even when that conversation is shared.
func (r *Repository) OwnedConversationIDForRun(ctx context.Context, projectID, ownerUserID, runID string) (string, error) {
	var conversationID string
	err := r.db.NewRaw(`
		SELECT cc.id::text
		FROM kb.agent_runs ar
		JOIN kb.chat_conversations cc ON cc.session_id = ar.session_id
		WHERE ar.id = ?
		  AND cc.project_id = ?
		  AND cc.owner_user_id = ?`, runID, projectID, ownerUserID).Scan(ctx, &conversationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("owned conversation for run: %w", err)
	}
	return conversationID, nil
}

// GetByIDWithMessages retrieves a conversation with all its messages, scoped
// to the caller (owner or non-private).
func (r *Repository) GetByIDWithMessages(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	condition, args := ownerOrSharedScope("conversation.", ownerUserID)
	err := r.db.NewSelect().
		Model(&conv).
		Relation("Messages", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Order("created_at ASC")
		}).
		Where("conversation.id = ?", conversationID).
		Where("conversation.project_id = ?", projectID).
		Where(condition, args...).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation with messages: %w", err)
	}

	return &conv, nil
}

// GetByCanonicalID retrieves a conversation by canonical ID (for object
// refinement chats), scoped to the caller: the caller must own the
// conversation or it must be non-private (project-shared).
func (r *Repository) GetByCanonicalID(ctx context.Context, projectID, ownerUserID string, canonicalID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	condition, args := ownerOrSharedScope("", ownerUserID)
	err := r.db.NewSelect().
		Model(&conv).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Where(condition, args...).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation by canonical ID: %w", err)
	}

	return &conv, nil
}

// Create creates a new conversation
func (r *Repository) Create(ctx context.Context, conv *Conversation) error {
	_, err := r.db.NewInsert().
		Model(conv).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "duplicate", "A conversation with this canonical ID already exists")
		}
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.New(400, "invalid-reference", "Referenced project or object not found")
		}
		r.log.Error("failed to create conversation", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Update updates a conversation, scoped to the caller (owner or non-private).
func (r *Repository) Update(ctx context.Context, projectID, ownerUserID string, conv *Conversation) error {
	condition, args := ownerOrSharedScope("", ownerUserID)
	result, err := r.db.NewUpdate().
		Model(conv).
		WherePK().
		Where("project_id = ?", projectID).
		Where(condition, args...).
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to update conversation", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Conversation not found")
	}

	return nil
}

// Delete deletes a conversation (cascades to messages via FK), scoped to the
// caller (owner or non-private).
func (r *Repository) Delete(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (bool, error) {
	condition, args := ownerOrSharedScope("", ownerUserID)
	result, err := r.db.NewDelete().
		Model((*Conversation)(nil)).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Where(condition, args...).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete conversation", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// SetArchived sets or clears the archive state of a conversation, scoped to the
// caller (owner or non-private) within the caller's project. It returns whether
// a row matched so the service can map a no-match to not-found. Setting a state
// that is already in effect is a no-op that still reports a match (idempotent).
func (r *Repository) SetArchived(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, archived bool) (bool, error) {
	condition, args := ownerOrSharedScope("", ownerUserID)
	q := r.db.NewUpdate().
		Model((*Conversation)(nil)).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Where(condition, args...)

	if archived {
		q = q.Set("is_archived = ?", true).
			Set("archived_at = NOW()")
	} else {
		q = q.Set("is_archived = ?", false).
			Set("archived_at = NULL")
	}

	result, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to set conversation archived", logger.Error(err))
		return false, apperror.NewDatabase("set conversation archived", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// subchatTreeCTE is the recursive, access-scoped descendant set rooted at one
// conversation. The root must match the caller's owner-or-shared predicate; each
// descendant must too, so a shared parent can never expose (or mutate) another
// user's private sub-chat. project_id is repeated in both terms because the
// table is RLS-scoped and the predicate is part of every conversation accessor.
func subchatTreeCTE(condition string) string {
	return `
WITH RECURSIVE tree AS (
    SELECT id, 0 AS depth
    FROM kb.chat_conversations
    WHERE id = ? AND project_id = ? AND ` + condition + `
    UNION ALL
    SELECT c.id, t.depth + 1
    FROM kb.chat_conversations c
    JOIN tree t ON c.parent_conversation_id = t.id
    WHERE c.project_id = ? AND ` + condition + ` AND t.depth < 64
)`
}

// subchatTreeArgs returns the argument list for subchatTreeCTE: the root id and
// project, then the access args twice (anchor + recursive term).
func subchatTreeArgs(conversationID uuid.UUID, projectID string, accessArgs []any) []any {
	args := []any{conversationID, projectID}
	args = append(args, accessArgs...)
	args = append(args, projectID)
	args = append(args, accessArgs...)
	return args
}

// DeleteCascade deletes a conversation and every descendant sub-chat visible to
// the caller. Messages cascade via the messages FK. It returns whether the root
// matched so the service can map a no-match to not-found.
func (r *Repository) DeleteCascade(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID) (bool, error) {
	condition, accessArgs := ownerOrSharedScope("", ownerUserID)
	query := subchatTreeCTE(condition) + `
DELETE FROM kb.chat_conversations WHERE id IN (SELECT id FROM tree)`
	result, err := r.db.NewRaw(query, subchatTreeArgs(conversationID, projectID, accessArgs)...).Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete conversation subtree", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// SetArchivedCascade sets or clears the archive state on a conversation and
// every descendant sub-chat visible to the caller. Archiving a parent hides the
// whole group (a visible child under a hidden parent cannot be grouped), and
// unarchiving restores it symmetrically. A no-op state change still reports a
// match, so the service can keep archive/unarchive idempotent.
func (r *Repository) SetArchivedCascade(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, archived bool) (bool, error) {
	condition, accessArgs := ownerOrSharedScope("", ownerUserID)
	setClause := "is_archived = false, archived_at = NULL"
	if archived {
		setClause = "is_archived = true, archived_at = NOW()"
	}
	query := subchatTreeCTE(condition) + `
UPDATE kb.chat_conversations SET ` + setClause + ` WHERE id IN (SELECT id FROM tree)`
	result, err := r.db.NewRaw(query, subchatTreeArgs(conversationID, projectID, accessArgs)...).Exec(ctx)
	if err != nil {
		r.log.Error("failed to set conversation subtree archived", logger.Error(err))
		return false, apperror.NewDatabase("set conversation subtree archived", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// AddMessage adds a message to a conversation
func (r *Repository) AddMessage(ctx context.Context, msg *Message) error {
	_, err := r.db.NewInsert().
		Model(msg).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.ErrNotFound.WithMessage("Conversation not found")
		}
		r.log.Error("failed to add message", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Update conversation's updated_at
	_, err = r.db.NewUpdate().
		Model((*Conversation)(nil)).
		Set("updated_at = NOW()").
		Where("id = ?", msg.ConversationID).
		Exec(ctx)

	if err != nil {
		r.log.Warn("failed to update conversation timestamp", logger.Error(err))
		// Don't fail the operation for this
	}

	return nil
}

// CreateConversationWithMessage creates a conversation and its first message in a transaction
func (r *Repository) CreateConversationWithMessage(ctx context.Context, conv *Conversation, msg *Message) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Create conversation
		_, err := tx.NewInsert().
			Model(conv).
			Returning("*").
			Exec(ctx)
		if err != nil {
			if pgutils.IsUniqueViolation(err) {
				return apperror.New(409, "duplicate", "A conversation with this canonical ID already exists")
			}
			if pgutils.IsForeignKeyViolation(err) {
				return apperror.New(400, "invalid-reference", "Referenced project or object not found")
			}
			return apperror.ErrDatabase.WithInternal(err)
		}

		// Set conversation ID on message
		msg.ConversationID = conv.ID

		// Create message
		_, err = tx.NewInsert().
			Model(msg).
			Returning("*").
			Exec(ctx)
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}

		return nil
	})
}

// GetConversationHistory retrieves the last N messages for a conversation,
// scoped to the caller: it returns no messages when the caller cannot access
// the conversation (owner or non-private within the caller's project).
func (r *Repository) GetConversationHistory(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	// Guard the read: never return messages from a conversation the caller
	// cannot see. This is the exfiltration step the canonical-id path could
	// otherwise feed — a foreign conversation id must yield no history.
	condition, args := ownerOrSharedScope("", ownerUserID)
	visible, err := r.db.NewSelect().
		Model((*Conversation)(nil)).
		Where("id = ?", conversationID).
		Where("project_id = ?", projectID).
		Where(condition, args...).
		Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("check conversation visibility: %w", err)
	}
	if !visible {
		return nil, nil
	}

	messages := []Message{}
	err = r.db.NewSelect().
		Model(&messages).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}

	// Reverse to chronological order (oldest first)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// SetAgentDefinitionID updates the agent_definition_id on a conversation,
// scoped to the caller (owner or non-private).
func (r *Repository) SetAgentDefinitionID(ctx context.Context, projectID, ownerUserID string, conversationID uuid.UUID, agentDefID *uuid.UUID) error {
	condition, args := ownerOrSharedScope("", ownerUserID)
	q := r.db.NewUpdate().
		Model((*Conversation)(nil)).
		Set("agent_definition_id = ?", agentDefID).
		Set("updated_at = NOW()").
		Where("id = ?", conversationID).
		Where(condition, args...)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	} else {
		q = q.Where("project_id IS NULL")
	}
	_, err := q.Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}
