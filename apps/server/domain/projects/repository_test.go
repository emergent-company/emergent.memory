package projects

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Error Helper Tests
// =============================================================================

func TestContainsErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			code: "23505",
			want: false,
		},
		{
			name: "error contains code directly",
			err:  errors.New("ERROR: duplicate key value violates unique constraint (23505)"),
			code: "23505",
			want: true,
		},
		{
			name: "error contains SQLSTATE prefix",
			err:  errors.New("ERROR: SQLSTATE 23505 duplicate key value"),
			code: "23505",
			want: true,
		},
		{
			name: "error does not contain code",
			err:  errors.New("some other error"),
			code: "23505",
			want: false,
		},
		{
			name: "empty error message",
			err:  errors.New(""),
			code: "23505",
			want: false,
		},
		{
			name: "foreign key violation code",
			err:  errors.New("ERROR: insert or update violates foreign key constraint SQLSTATE 23503"),
			code: "23503",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsErrorCode(tt.err, tt.code)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unique violation error with SQLSTATE",
			err:  errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"),
			want: true,
		},
		{
			name: "unique violation error with code only",
			err:  errors.New("ERROR: duplicate key 23505"),
			want: true,
		},
		{
			name: "foreign key error",
			err:  errors.New("ERROR: violates foreign key constraint (SQLSTATE 23503)"),
			want: false,
		},
		{
			name: "other error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUniqueViolation(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "foreign key violation error with SQLSTATE",
			err:  errors.New("ERROR: violates foreign key constraint (SQLSTATE 23503)"),
			want: true,
		},
		{
			name: "foreign key violation with code only",
			err:  errors.New("insert failed: 23503 foreign key"),
			want: true,
		},
		{
			name: "unique violation error",
			err:  errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"),
			want: false,
		},
		{
			name: "other error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isForeignKeyViolation(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// isValidUUID Tests
// =============================================================================

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{
			name: "valid UUID lowercase",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			want: true,
		},
		{
			name: "valid UUID uppercase",
			id:   "550E8400-E29B-41D4-A716-446655440000",
			want: true,
		},
		{
			name: "valid UUID mixed case",
			id:   "550e8400-E29B-41d4-A716-446655440000",
			want: true,
		},
		{
			name: "empty string",
			id:   "",
			want: false,
		},
		{
			name: "too short",
			id:   "550e8400-e29b-41d4-a716",
			want: false,
		},
		{
			name: "too long",
			id:   "550e8400-e29b-41d4-a716-446655440000-extra",
			want: false,
		},
		{
			name: "missing hyphens",
			id:   "550e8400e29b41d4a716446655440000",
			want: false,
		},
		{
			name: "invalid characters",
			id:   "550e8400-e29b-41d4-a716-44665544000g",
			want: false,
		},
		{
			name: "spaces",
			id:   "550e8400 e29b 41d4 a716 446655440000",
			want: false,
		},
		{
			name: "nil UUID",
			id:   "00000000-0000-0000-0000-000000000000",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUUID(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// Project ToDTO Tests
// =============================================================================

func TestProject_ToDTO(t *testing.T) {
	t.Run("basic project without optional fields", func(t *testing.T) {
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			AutoExtractObjects: false,
		}

		dto := project.ToDTO()

		assert.Equal(t, "project-123", dto.ID)
		assert.Equal(t, "Test Project", dto.Name)
		assert.Equal(t, "org-456", dto.OrgID)
		assert.Nil(t, dto.KBPurpose)
		assert.Nil(t, dto.AutoExtractObjects) // false should result in nil
		assert.Nil(t, dto.AutoExtractConfig)
	})

	t.Run("project with kb purpose", func(t *testing.T) {
		purpose := "Testing knowledge base"
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			KBPurpose:        &purpose,
		}

		dto := project.ToDTO()

		assert.NotNil(t, dto.KBPurpose)
		assert.Equal(t, purpose, *dto.KBPurpose)
	})

	t.Run("project with auto extract enabled", func(t *testing.T) {
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			AutoExtractObjects: true,
		}

		dto := project.ToDTO()

		assert.NotNil(t, dto.AutoExtractObjects)
		assert.True(t, *dto.AutoExtractObjects)
	})

	t.Run("project with auto extract config", func(t *testing.T) {
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			AutoExtractConfig: map[string]any{
				"enabled": true,
				"types":   []string{"Person", "Organization"},
			},
		}

		dto := project.ToDTO()

		assert.NotNil(t, dto.AutoExtractConfig)
		assert.Equal(t, true, dto.AutoExtractConfig["enabled"])
	})

	t.Run("project with empty auto extract config", func(t *testing.T) {
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			AutoExtractConfig: map[string]any{},
		}

		dto := project.ToDTO()

		assert.Nil(t, dto.AutoExtractConfig) // empty map should result in nil
	})

	t.Run("project with chat prompt template", func(t *testing.T) {
		template := "You are a helpful assistant for {{project_name}}"
		project := &Project{
			ID:               "project-123",
			Name:             "Test Project",
			OrganizationID:   "org-456",
			ChatPromptTemplate: &template,
		}

		dto := project.ToDTO()

		assert.NotNil(t, dto.ChatPromptTemplate)
		assert.Equal(t, template, *dto.ChatPromptTemplate)
	})
}
