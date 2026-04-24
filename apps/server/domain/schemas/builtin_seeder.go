package schemas

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// BuiltinSeeder seeds system-level schema definitions at server startup.
// These schemas (Session, Message) are available globally and enable conversation
// storage without any per-project setup.
type BuiltinSeeder struct {
	repo *Repository
	log  *slog.Logger
}

// NewBuiltinSeeder creates a new BuiltinSeeder.
func NewBuiltinSeeder(repo *Repository, log *slog.Logger) *BuiltinSeeder {
	return &BuiltinSeeder{
		repo: repo,
		log:  log.With(logger.Scope("schemas.builtin_seeder")),
	}
}

// RegisterBuiltinSeederLifecycle registers the seeder with fx lifecycle.
func RegisterBuiltinSeederLifecycle(lc fx.Lifecycle, seeder *BuiltinSeeder) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return seeder.Seed(ctx)
		},
	})
}

// builtinSessionObjectSchemas contains Session and Message schema definitions.
var builtinSessionObjectSchemas = []map[string]any{
	{
		"name":        "Session",
		"label":       "Session",
		"description": "A conversation session storing a sequence of messages",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Title or name for this session",
			},
			"started_at": map[string]any{
				"type":        "string",
				"format":      "date-time",
				"description": "When the session started",
			},
			"ended_at": map[string]any{
				"type":        "string",
				"format":      "date-time",
				"description": "When the session ended (optional)",
			},
			"message_count": map[string]any{
				"type":        "integer",
				"description": "Number of messages in this session",
				"default":     0,
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "A summary of the session content (optional)",
			},
			"agent_version": map[string]any{
				"type":        "string",
				"description": "Version of the agent that ran this session (optional)",
			},
		},
		"required": []string{"title"},
	},
	{
		"name":        "Message",
		"label":       "Message",
		"description": "A single message within a conversation session",
		"properties": map[string]any{
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"user", "assistant", "system"},
				"description": "The role of the message author",
			},
			"content": map[string]any{
				"type":               "string",
				"description":        "The text content of the message",
				"x-embedding-target": true,
			},
			"sequence_number": map[string]any{
				"type":        "integer",
				"description": "Order of this message within the session (1-indexed)",
			},
			"timestamp": map[string]any{
				"type":        "string",
				"format":      "date-time",
				"description": "When the message was created",
			},
			"token_count": map[string]any{
				"type":        "integer",
				"description": "Number of tokens in the message (optional)",
			},
			"tool_calls": map[string]any{
				"type":        "array",
				"description": "Tool calls made in this message (optional)",
				"items":       map[string]any{"type": "object"},
			},
		},
		"required": []string{"role", "content", "sequence_number"},
	},
}

// builtinSessionRelationshipSchemas contains the has_message relationship type.
var builtinSessionRelationshipSchemas = []map[string]any{
	{
		"name":        "has_message",
		"description": "Links a Session to its Messages in order",
		"sourceType":  "Session",
		"targetType":  "Message",
	},
}

// Seed upserts the built-in Session/Message schema definitions.
// It is idempotent — safe to call on every server boot.
func (s *BuiltinSeeder) Seed(ctx context.Context) error {
	objectSchemas, err := json.Marshal(builtinSessionObjectSchemas)
	if err != nil {
		return err
	}

	relSchemas, err := json.Marshal(builtinSessionRelationshipSchemas)
	if err != nil {
		return err
	}

	now := time.Now()
	source := "builtin"
	version := "1.0.0"
	description := "Built-in conversation primitives: Session and Message types for storing agent interactions"
	visibility := "global"

	// Check if the built-in schema already exists.
	var existingID string
	err = s.repo.DB().NewRaw(`
		SELECT id FROM kb.graph_schemas
		WHERE name = 'session-message-types' AND version = ?
		LIMIT 1
	`, version).Scan(ctx, &existingID)

	if err != nil || existingID == "" {
		// Insert new built-in schema.
		_, insertErr := s.repo.DB().NewRaw(`
			INSERT INTO kb.graph_schemas (
				name, version, description, source, visibility,
				object_type_schemas, relationship_type_schemas,
				published_at, created_at, updated_at
			) VALUES (
				'session-message-types', ?, ?, ?, ?,
				?, ?,
				?, ?, ?
			)
		`, version, description, source, visibility,
			string(objectSchemas), string(relSchemas),
			now, now, now).Exec(ctx)
		if insertErr != nil {
			s.log.Error("failed to insert builtin session-message schemas", logger.Error(insertErr))
			return insertErr
		}
	} else {
		// Update existing schema to pick up any definition changes.
		_, updateErr := s.repo.DB().NewRaw(`
			UPDATE kb.graph_schemas SET
				object_type_schemas = ?,
				relationship_type_schemas = ?,
				updated_at = ?
			WHERE name = 'session-message-types' AND version = ?
		`, string(objectSchemas), string(relSchemas), now, version).Exec(ctx)
		if updateErr != nil {
			s.log.Error("failed to update builtin session-message schemas", logger.Error(updateErr))
			return updateErr
		}
	}

	s.log.Info("builtin session-message schemas seeded successfully")
	return nil
}
