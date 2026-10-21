package gdrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

func fileJSON(id, name, mime, rev string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"mimeType":%q,"headRevisionId":%q,"trashed":false,"modifiedTime":"2024-01-02T03:04:05.000Z"}`,
		id, name, mime, rev)
}

func TestListFolderItems(t *testing.T) {
	var queries []string
	var pageTokens []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		pt := r.URL.Query().Get("pageToken")
		queries = append(queries, q)
		pageTokens = append(pageTokens, pt)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "'root' in parents") && pt == "":
			fmt.Fprint(w, `{"nextPageToken":"tok2","files":[`+
				fileJSON("pdf1", "Report", "application/pdf", "rev1")+`,`+
				fileJSON("folderA", "Sub", "application/vnd.google-apps.folder", "")+`]}`)
		case strings.Contains(q, "'root' in parents") && pt == "tok2":
			fmt.Fprint(w, `{"files":[`+fileJSON("pdf2", "Slides", "application/pdf", "rev2")+`]}`)
		case strings.Contains(q, "'folderA' in parents"):
			fmt.Fprint(w, `{"files":[`+fileJSON("pdf3", "Nested", "application/pdf", "rev3")+`]}`)
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	items, err := c.ListFolderItems(context.Background(), "root", []string{"application/pdf"}, true, "")
	require.NoError(t, err)

	require.Len(t, items, 3)
	require.Equal(t, []string{"pdf1", "pdf2", "pdf3"}, []string{items[0].ID, items[1].ID, items[2].ID})
	require.Equal(t, "Report", items[0].Name)
	require.Equal(t, "application/pdf", items[0].MimeType)
	require.Equal(t, "rev1", items[0].RevisionID)
	require.False(t, items[0].Trashed)
	require.True(t, items[0].ModifiedTime.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)))

	// Three list calls: root page 1, root page 2, folderA.
	require.Len(t, queries, 3)
	require.Contains(t, queries[0], "'root' in parents")
	require.Contains(t, queries[0], "trashed = false")
	require.Contains(t, queries[0], "mimeType = 'application/pdf'")
	require.Contains(t, queries[0], "mimeType = 'application/vnd.google-apps.folder'")
	require.Equal(t, "tok2", pageTokens[1])
	require.Contains(t, queries[2], "'folderA' in parents")
}

func TestListFolderItemsNonRecursiveNoFolderClause(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("q")
		require.Equal(t, "true", r.URL.Query().Get("supportsAllDrives"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"files":[`+fileJSON("pdf1", "Report", "application/pdf", "rev1")+`]}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	items, err := c.ListFolderItems(context.Background(), "root", []string{"application/pdf"}, false, "drive123")
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.Contains(t, query, "'root' in parents")
	require.Contains(t, query, "mimeType = 'application/pdf'")
	require.NotContains(t, query, "application/vnd.google-apps.folder")
}

func TestGetRevision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/file1" {
			fmt.Fprint(w, `{"id":"file1","headRevisionId":"rev-123","version":"5","modifiedTime":"2024-01-02T03:04:05.000Z"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	rev, err := c.GetRevision(context.Background(), "file1")
	require.NoError(t, err)
	require.Equal(t, "rev-123", rev)
}

func TestExportText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/files/doc1/export", r.URL.Path)
		require.Equal(t, "media", r.URL.Query().Get("alt"))
		fmt.Fprint(w, "hello exported")
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	b, err := c.ExportText(context.Background(), "doc1", "application/vnd.google-apps.document")
	require.NoError(t, err)
	require.Equal(t, "hello exported", string(b))
}

func TestExportTextUnsupportedType(t *testing.T) {
	c, err := NewWithHTTPClient(http.DefaultClient, option.WithEndpoint("http://127.0.0.1:1"))
	require.NoError(t, err)

	_, err = c.ExportText(context.Background(), "doc1", "application/pdf")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported export MIME type")
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/files/bin1", r.URL.Path)
		require.Equal(t, "media", r.URL.Query().Get("alt"))
		fmt.Fprint(w, "pdf-bytes")
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	b, err := c.DownloadFile(context.Background(), "bin1")
	require.NoError(t, err)
	require.Equal(t, "pdf-bytes", string(b))
}

func TestExportTextMaxBytesAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 100))
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)
	c = c.WithConfig(Config{MaxBytes: 10, RequestTimeout: time.Second})

	_, err = c.ExportText(context.Background(), "doc1", "application/vnd.google-apps.document")
	require.Error(t, err)

	var se *SizeError
	require.True(t, errors.As(err, &se), "expected SizeError, got %T: %v", err, err)
	require.Equal(t, int64(10), se.Limit)
}

func TestDownloadFileMaxBytesAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("y", 50))
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)
	c = c.WithConfig(Config{MaxBytes: 5, RequestTimeout: time.Second})

	_, err = c.DownloadFile(context.Background(), "bin1")
	require.Error(t, err)

	var se *SizeError
	require.True(t, errors.As(err, &se))
}

func TestTestConnectionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/about", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"user":{"permissionId":"123"}}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	require.NoError(t, c.TestConnection(context.Background()))
}

func TestTestConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":401,"message":"Invalid Credentials"}}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	err = c.TestConnection(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection test failed")
}

func TestListFoldersNameFilterEscapesQuery(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.Query().Get("q"))
		require.Equal(t, "1000", r.URL.Query().Get("pageSize"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"files":[`+fileJSON("f1", "Budget Q3", folderMimeType, "")+`]}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	items, truncated, err := c.ListFolders(context.Background(), "", "Budget O'Brien")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, items, 1)
	require.Equal(t, "f1", items[0].ID)

	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "mimeType = '"+folderMimeType+"'")
	require.Contains(t, queries[0], "trashed = false")
	require.Contains(t, queries[0], `name contains 'Budget O\'Brien'`)
}

func TestEscapeDriveQueryValue(t *testing.T) {
	// Drive's q clause quotes values with single quotes and escapes with
	// backslashes; a backslash in the name must be doubled too, or the clause
	// ends up malformed.
	for in, want := range map[string]string{
		"Reports":        "Reports",
		`Budget O'Brien`: `Budget O\'Brien`,
		`back\slash`:     `back\\slash`,
		`both'and\mixed`: `both\'and\\mixed`,
	} {
		require.Equal(t, want, escapeDriveQueryValue(in), "input %q", in)
	}
}

func TestListFoldersCompleteListingNotTruncated(t *testing.T) {
	// Exactly maxListFolderPages pages, the last one carrying no next-page
	// token, is a complete listing: truncated must be false.
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("Content-Type", "application/json")
		if pages < maxListFolderPages {
			fmt.Fprint(w, `{"nextPageToken":"tok","files":[`+fileJSON("f", "Folder", folderMimeType, "")+`]}`)
			return
		}
		fmt.Fprint(w, `{"files":[`+fileJSON("f", "Folder", folderMimeType, "")+`]}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	items, truncated, err := c.ListFolders(context.Background(), "", "")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, items, maxListFolderPages)
}

func TestListFoldersCappedTruncated(t *testing.T) {
	// Always return a next-page token: the walk hits the page cap and must
	// report truncated.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"nextPageToken":"more","files":[`+fileJSON("f", "Folder", folderMimeType, "")+`]}`)
	}))
	defer srv.Close()

	c, err := NewWithHTTPClient(srv.Client(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	items, truncated, err := c.ListFolders(context.Background(), "", "")
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, items, maxListFolderPages)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0xAB}, 32)

	creds := Credentials{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "refresh-token",
	}

	blob, err := EncryptCredentials(key, creds)
	require.NoError(t, err)
	require.NotEmpty(t, blob)

	// Blob must not contain plaintext secret material.
	require.NotContains(t, string(blob), "client-secret")
	require.NotContains(t, string(blob), "refresh-token")

	got, err := DecryptCredentials(key, blob)
	require.NoError(t, err)
	require.Equal(t, creds, got)
}

func TestDecryptCredentialsWrongKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x01}, 32)
	other := bytes.Repeat([]byte{0x02}, 32)

	creds := Credentials{ClientID: "a", ClientSecret: "b", RefreshToken: "c"}
	blob, err := EncryptCredentials(key, creds)
	require.NoError(t, err)

	_, err = DecryptCredentials(other, blob)
	require.Error(t, err)
}

func TestDecryptCredentialsMalformed(t *testing.T) {
	key := bytes.Repeat([]byte{0x01}, 32)

	_, err := DecryptCredentials(key, []byte{0x01})
	require.Error(t, err)
}
