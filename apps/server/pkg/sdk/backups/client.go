package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Backups and Restores API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new backups client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// setHeaders adds auth and context headers to the request.
func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// CreateBackup initiates an async backup for the given project.
// The server returns 202 with a backup in status "creating".
func (c *Client) CreateBackup(ctx context.Context, projectID string, req *CreateBackupRequest) (*Backup, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/api/v1/projects/"+url.PathEscape(projectID)+"/backups", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var backup Backup
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &backup, nil
}

// ListBackups lists backups for the given organization, optionally filtered by
// project and paginated.
func (c *Client) ListBackups(ctx context.Context, orgID string, opts *ListBackupsOptions) (*ListBackupsResult, error) {
	u, err := url.Parse(c.base + "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.ProjectID != "" {
			q.Set("project_id", opts.ProjectID)
		}
		if opts.Limit > 0 {
			q.Set("limit", strconv.Itoa(opts.Limit))
		}
		if opts.Cursor != "" {
			q.Set("cursor", opts.Cursor)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ListBackupsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetBackup retrieves a single backup by ID.
func (c *Client) GetBackup(ctx context.Context, orgID, backupID string) (*Backup, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.base+"/api/v1/organizations/"+url.PathEscape(orgID)+"/backups/"+url.PathEscape(backupID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var backup Backup
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &backup, nil
}

// DownloadBackup streams the backup archive, following the server's 302
// redirect to the presigned URL. It returns the response body and the archive
// filename parsed from Content-Disposition (fallback "backup-<id>.zip").
//
// The download is NOT bounded by the SDK client's default timeout: it uses an
// http.Client sharing the same Transport but with no Timeout, relying on ctx for
// cancellation, so large archives can stream. Authorization is stripped by
// net/http automatically on the cross-host redirect.
func (c *Client) DownloadBackup(ctx context.Context, orgID, backupID string) (io.ReadCloser, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.base+"/api/v1/organizations/"+url.PathEscape(orgID)+"/backups/"+url.PathEscape(backupID)+"/download", nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, "", err
	}

	resp, err := c.downloadClient().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, "", sdkerrors.ParseErrorResponse(resp)
	}

	filename := filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename == "" {
		filename = "backup-" + backupID + ".zip"
	}

	return resp.Body, filename, nil
}

// downloadClient returns an http.Client with no Timeout that shares the SDK
// client's Transport (and any custom CheckRedirect), so large archives can
// stream without hitting the default 30s total timeout.
func (c *Client) downloadClient() *http.Client {
	transport := c.http.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	dl := &http.Client{
		Transport: transport,
		// Timeout intentionally unset — rely on ctx for cancellation.
	}
	if c.http.CheckRedirect != nil {
		dl.CheckRedirect = c.http.CheckRedirect
	}
	return dl
}

// filenameFromContentDisposition extracts the filename parameter from a
// Content-Disposition header. Returns "" when absent or unparseable.
func filenameFromContentDisposition(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return params["filename"]
}

// DeleteBackup deletes a backup by ID (HTTP 204).
func (c *Client) DeleteBackup(ctx context.Context, orgID, backupID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		c.base+"/api/v1/organizations/"+url.PathEscape(orgID)+"/backups/"+url.PathEscape(backupID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ImportBackup uploads an archive produced by another deployment, streaming the
// multipart body via io.Pipe (the server cap is 1 GiB, so the archive is never
// buffered in memory). The multipart field is "file" with an optional
// "retentionDays" field. Returns the registered (ready, imported) backup.
func (c *Client) ImportBackup(ctx context.Context, orgID string, input *ImportBackupInput) (*Backup, error) {
	if input == nil || input.Reader == nil {
		return nil, fmt.Errorf("ImportBackupInput with a non-nil Reader is required")
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	done := make(chan error, 1)
	go func() {
		var werr error
		part, err := writer.CreateFormFile("file", input.Filename)
		if err != nil {
			werr = err
		} else if _, err := io.Copy(part, input.Reader); err != nil {
			werr = fmt.Errorf("failed to write file content: %w", err)
		}
		if werr == nil && input.RetentionDays > 0 {
			if err := writer.WriteField("retentionDays", strconv.Itoa(input.RetentionDays)); err != nil {
				werr = fmt.Errorf("failed to write retentionDays field: %w", err)
			}
		}
		if werr == nil {
			werr = writer.Close()
		}
		_ = pw.CloseWithError(werr)
		done <- werr
	}()

	u := c.base + "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups/import"
	req, err := http.NewRequestWithContext(ctx, "POST", u, pr)
	if err != nil {
		_ = pw.CloseWithError(err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := c.setHeaders(req); err != nil {
		_ = pw.CloseWithError(err)
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if werr := <-done; werr != nil {
		return nil, werr
	}

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var backup Backup
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &backup, nil
}

// CreateCloneRestore creates a clone restore into the given organization. The
// server forces clone mode; the request body carries only backupId,
// includeJournal, and targetProjectName.
func (c *Client) CreateCloneRestore(ctx context.Context, orgID string, req *RestoreRequest) (*Restore, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/api/v1/organizations/"+url.PathEscape(orgID)+"/restore", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var restore Restore
	if err := json.NewDecoder(resp.Body).Decode(&restore); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &restore, nil
}

// GetRestore retrieves a restore job status by ID.
func (c *Client) GetRestore(ctx context.Context, restoreID string) (*Restore, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.base+"/api/v1/restores/"+url.PathEscape(restoreID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var restore Restore
	if err := json.NewDecoder(resp.Body).Decode(&restore); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &restore, nil
}
