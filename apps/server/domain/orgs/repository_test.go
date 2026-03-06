package orgs

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSearchSubstring(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "substring at start",
			s:      "hello world",
			substr: "hello",
			want:   true,
		},
		{
			name:   "substring at end",
			s:      "hello world",
			substr: "world",
			want:   true,
		},
		{
			name:   "substring in middle",
			s:      "hello world",
			substr: "lo wo",
			want:   true,
		},
		{
			name:   "exact match",
			s:      "hello",
			substr: "hello",
			want:   true,
		},
		{
			name:   "not found",
			s:      "hello world",
			substr: "xyz",
			want:   false,
		},
		{
			name:   "empty substring",
			s:      "hello",
			substr: "",
			want:   true,
		},
		{
			name:   "substring longer than string",
			s:      "hi",
			substr: "hello",
			want:   false,
		},
		{
			name:   "empty string",
			s:      "",
			substr: "hello",
			want:   false,
		},
		{
			name:   "both empty",
			s:      "",
			substr: "",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchSubstring(tt.s, tt.substr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "contains substring",
			s:      "hello world",
			substr: "world",
			want:   true,
		},
		{
			name:   "does not contain",
			s:      "hello world",
			substr: "xyz",
			want:   false,
		},
		{
			name:   "string shorter than substring",
			s:      "hi",
			substr: "hello",
			want:   false,
		},
		{
			name:   "exact match",
			s:      "test",
			substr: "test",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substr)
			assert.Equal(t, tt.want, got)
		})
	}
}

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
			name: "unique violation error",
			err:  errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"),
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
			name: "foreign key violation error",
			err:  errors.New("ERROR: violates foreign key constraint (SQLSTATE 23503)"),
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
		{
			name: "error with code embedded",
			err:  errors.New("insert failed: 23503 foreign key"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isForeignKeyViolation(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOrg_ToDTO(t *testing.T) {
	now := time.Now()
	deletedAt := now.Add(-time.Hour)
	deletedBy := "user-123"

	tests := []struct {
		name string
		org  *Org
	}{
		{
			name: "basic org",
			org: &Org{
				ID:        "org-123",
				Name:      "Test Organization",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			name: "org with empty name",
			org: &Org{
				ID:        "org-456",
				Name:      "",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			name: "soft deleted org",
			org: &Org{
				ID:        "org-789",
				Name:      "Deleted Org",
				CreatedAt: now,
				UpdatedAt: now,
				DeletedAt: &deletedAt,
				DeletedBy: &deletedBy,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dto := tt.org.ToDTO()

			assert.Equal(t, tt.org.ID, dto.ID, "ID should match")
			assert.Equal(t, tt.org.Name, dto.Name, "Name should match")
		})
	}
}

func TestOrgDTO_Fields(t *testing.T) {
	dto := OrgDTO{
		ID:   "org-123",
		Name: "Test Org",
	}

	assert.Equal(t, "org-123", dto.ID)
	assert.Equal(t, "Test Org", dto.Name)
}

func TestOrganizationMembership_Fields(t *testing.T) {
	now := time.Now()

	membership := OrganizationMembership{
		ID:             "mem-123",
		OrganizationID: "org-456",
		UserID:         "user-789",
		Role:           "admin",
		CreatedAt:      now,
	}

	assert.Equal(t, "mem-123", membership.ID)
	assert.Equal(t, "org-456", membership.OrganizationID)
	assert.Equal(t, "user-789", membership.UserID)
	assert.Equal(t, "admin", membership.Role)
	assert.Equal(t, now, membership.CreatedAt)
}

func TestCreateOrgRequest_Fields(t *testing.T) {
	req := CreateOrgRequest{
		Name: "New Organization",
	}

	assert.Equal(t, "New Organization", req.Name)
}
