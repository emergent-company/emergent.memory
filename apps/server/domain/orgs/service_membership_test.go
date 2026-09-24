package orgs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// expectHTTPStatus asserts err is an *apperror.Error with the given HTTP status.
func expectHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, status, appErr.HTTPStatus)
}

// TestServiceGetByID_Membership covers the membership gate added to GetByID:
// no user ⇒ 401, non-member ⇒ 403, member ⇒ allowed (issue #851).
func TestServiceGetByID_Membership(t *testing.T) {
	svc := testOrgService(&fakeOrgRepo{org: &Org{ID: "org-1", Name: "Org"}})

	_, err := svc.GetByID(context.Background(), "org-1", "")
	expectHTTPStatus(t, err, 401)

	_, err = svc.GetByID(context.Background(), "org-1", "user-2")
	expectHTTPStatus(t, err, 403)

	svc = testOrgService(&fakeOrgRepo{org: &Org{ID: "org-1", Name: "Org"}, member: true})
	dto, err := svc.GetByID(context.Background(), "org-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "org-1", dto.ID)
}

// TestServiceUpdate_Membership covers the membership gate added to Update.
func TestServiceUpdate_Membership(t *testing.T) {
	svc := testOrgService(&fakeOrgRepo{org: &Org{ID: "org-1", Name: "Renamed"}})

	_, err := svc.Update(context.Background(), "org-1", "", "Renamed")
	expectHTTPStatus(t, err, 401)

	_, err = svc.Update(context.Background(), "org-1", "user-2", "Renamed")
	expectHTTPStatus(t, err, 403)

	repo := &fakeOrgRepo{org: &Org{ID: "org-1", Name: "Renamed"}, member: true}
	svc = testOrgService(repo)
	_, err = svc.Update(context.Background(), "org-1", "user-1", "Renamed")
	require.NoError(t, err)
	assert.Equal(t, 1, repo.updateCalls)
}

// TestServiceDelete_Membership covers the membership gate added to Delete.
func TestServiceDelete_Membership(t *testing.T) {
	svc := testOrgService(&fakeOrgRepo{})

	expectHTTPStatus(t, svc.Delete(context.Background(), "org-1", ""), 401)

	expectHTTPStatus(t, svc.Delete(context.Background(), "org-1", "user-2"), 403)

	repo := &fakeOrgRepo{member: true, deleteResult: true}
	svc = testOrgService(repo)
	require.NoError(t, svc.Delete(context.Background(), "org-1", "user-1"))
	assert.Equal(t, 1, repo.deleteCalls)
	assert.Equal(t, "org-1", repo.deletedID)
}

// TestServiceListMembers_Membership covers the membership gate added to ListMembers.
func TestServiceListMembers_Membership(t *testing.T) {
	svc := testOrgService(&fakeOrgRepo{})

	_, err := svc.ListMembers(context.Background(), "org-1", "")
	expectHTTPStatus(t, err, 401)

	_, err = svc.ListMembers(context.Background(), "org-1", "user-2")
	expectHTTPStatus(t, err, 403)

	svc = testOrgService(&fakeOrgRepo{member: true})
	_, err = svc.ListMembers(context.Background(), "org-1", "user-1")
	require.NoError(t, err)
}
