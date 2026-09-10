package orgs

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// fakeOrgRepo is an in-memory orgRepository double for service/handler tests.
// Only the fields exercised by a given test need to be set.
type fakeOrgRepo struct {
	org  *Org   // returned by UpdateName when updateErr is nil
	err  error  // UpdateName failure

	updatedID   string // last id passed to UpdateName
	updatedName string // last name passed to UpdateName
	updateCalls int
}

func (f *fakeOrgRepo) List(context.Context, string) ([]OrgDTO, error) { return nil, nil }

func (f *fakeOrgRepo) GetByID(context.Context, string) (*Org, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.org, nil
}

func (f *fakeOrgRepo) Create(context.Context, string, string) (*Org, error) { return nil, nil }

func (f *fakeOrgRepo) UpdateName(_ context.Context, id, name string) (*Org, error) {
	f.updateCalls++
	f.updatedID = id
	f.updatedName = name
	if f.err != nil {
		return nil, f.err
	}
	return f.org, nil
}

func (f *fakeOrgRepo) Delete(context.Context, string) (bool, error) { return false, nil }

func (f *fakeOrgRepo) ListMembers(context.Context, string) ([]OrgMemberDTO, error) {
	return nil, nil
}

func (f *fakeOrgRepo) CountUserMemberships(context.Context, string) (int, error) { return 0, nil }

func (f *fakeOrgRepo) IsUserMember(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeOrgRepo) FindOrgToolSettings(context.Context, string) ([]OrgToolSetting, error) {
	return nil, nil
}

func (f *fakeOrgRepo) UpsertOrgToolSetting(context.Context, *OrgToolSetting) (*OrgToolSetting, error) {
	return nil, nil
}

func (f *fakeOrgRepo) DeleteOrgToolSetting(context.Context, string, string) (bool, error) {
	return false, nil
}

func testOrgService(repo orgRepository) *Service {
	return &Service{
		repo: repo,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestServiceUpdate_Success(t *testing.T) {
	repo := &fakeOrgRepo{org: &Org{ID: "org-1", Name: "Renamed"}}
	svc := testOrgService(repo)

	dto, err := svc.Update(context.Background(), "org-1", "  Renamed  ")

	require.NoError(t, err)
	require.NotNil(t, dto)
	assert.Equal(t, "org-1", dto.ID)
	assert.Equal(t, "Renamed", dto.Name)
	// The service trims before persisting.
	assert.Equal(t, "Renamed", repo.updatedName)
	assert.Equal(t, "org-1", repo.updatedID)
}

func TestServiceUpdate_InvalidName(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "whitespace only", in: "   "},
		{name: "too long", in: string(make([]byte, MaxOrgNameLength+1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeOrgRepo{}
			svc := testOrgService(repo)

			dto, err := svc.Update(context.Background(), "org-1", tt.in)

			assert.Nil(t, dto)
			require.Error(t, err)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, 400, appErr.HTTPStatus)
			// Validation short-circuits before any repository write.
			assert.Zero(t, repo.updateCalls)
		})
	}
}

func TestServiceUpdate_NotFound(t *testing.T) {
	repo := &fakeOrgRepo{err: apperror.ErrNotFound.WithMessage("Organization not found")}
	svc := testOrgService(repo)

	dto, err := svc.Update(context.Background(), "missing", "New name")

	assert.Nil(t, dto)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.HTTPStatus)
	assert.Equal(t, "missing", repo.updatedID)
}
