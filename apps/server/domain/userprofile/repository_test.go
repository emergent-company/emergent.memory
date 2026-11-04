package userprofile

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func newRepoMock(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	db := bun.NewDB(sqldb, pgdialect.New())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRepository(db, log), mock
}

// TestGetEmail_NoRowsReturnsEmpty pins the deliberate mapping: a user with no
// verified email is a legitimate state, so GetEmail returns ("", nil) rather
// than surfacing sql.ErrNoRows as a failure.
func TestGetEmail_NoRowsReturnsEmpty(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectQuery(`SELECT[\s\S]*FROM core\.user_emails[\s\S]*verified = true[\s\S]*`).
		WillReturnError(sql.ErrNoRows)

	email, err := repo.GetEmail(context.Background(), "u1")
	require.NoError(t, err)
	require.Equal(t, "", email)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEmail_OtherErrorMapsToDatabase pins the other half of the mapping: any
// non-ErrNoRows failure becomes an apperror with the database_error code (HTTP
// 500) instead of silently returning an empty email. That distinction was
// untested, which is what made the original ambiguity hard to diagnose (#1888).
func TestGetEmail_OtherErrorMapsToDatabase(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectQuery(`SELECT[\s\S]*FROM core\.user_emails[\s\S]*verified = true[\s\S]*`).
		WillReturnError(errors.New("connection reset"))

	email, err := repo.GetEmail(context.Background(), "u1")
	require.Error(t, err)
	require.Equal(t, "", email)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "database_error", appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
