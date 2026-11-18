package graphexplore

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNodeDetailContentNameIsStoredTitleOnly pins the detail panel's "Name"
// heading: it shows the stored `name` column, and a nameless object renders a
// dash rather than a synthesized `type + short id` label (which would read like
// a stored name). Refs #918.
func TestNodeDetailContentNameIsStoredTitleOnly(t *testing.T) {
	var named bytes.Buffer
	require.NoError(t, NodeDetailContent(NodeDetail{
		ID:   "0123abcd-0000-0000-0000-000000000000",
		Type: "Task",
		Name: "Fix login loop",
	}).Render(context.Background(), &named))
	assert.Contains(t, named.String(), "Fix login loop")
	assert.NotContains(t, named.String(), ">—</div>", "a named object must not render the nameless dash")

	var nameless bytes.Buffer
	require.NoError(t, NodeDetailContent(NodeDetail{
		ID:   "0123abcd-0000-0000-0000-000000000000",
		Type: "Task",
	}).Render(context.Background(), &nameless))
	assert.Contains(t, nameless.String(), ">—</div>", "a nameless object renders a dash under Name")
	assert.NotContains(t, nameless.String(), "Task 0123ab", "a synthesized label belongs on the graph node, not under Name")
}

// TestHandleNodeDetailUsesStoredName proves the handler does not synthesize a
// label for the Name field: the graph node label is still useful, but the panel
// must not present a pseudo-name as a stored one.
func TestHandleNodeDetailUsesStoredName(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "stored title",
			body:         `{"id":"0123abcd-0000-0000-0000-000000000000","type":"Task","name":"Fix login loop"}`,
			wantContains: []string{"Fix login loop"},
			wantAbsent:   []string{">—</div>"},
		},
		{
			name:         "nameless object with a machine key",
			body:         `{"id":"0123abcd-0000-0000-0000-000000000000","type":"Task","key":"task-0123456789ab"}`,
			wantContains: []string{">—</div>"},
			wantAbsent:   []string{"Task 0123ab"},
		},
		{
			name:         "whitespace-only title is not a name",
			body:         `{"id":"0123abcd-0000-0000-0000-000000000000","type":"Task","name":"   "}`,
			wantContains: []string{">—</div>"},
			wantAbsent:   []string{"Task 0123ab"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()

			s := NewServer("proj-1", upstream.URL, "", "")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/htmx/node-detail?nodeId=node-1", nil)
			s.handleNodeDetail(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)

			for _, want := range tc.wantContains {
				assert.Contains(t, rec.Body.String(), want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, rec.Body.String(), absent)
			}
		})
	}
}

// TestObjectLabel pins the label rule the graph surface uses: a stored name,
// else a non-machine key, else `type + short id`. The machine-key shapes must
// stay in step with migration 00311 and the web-ui gateway (#824, #918).
func TestObjectLabel(t *testing.T) {
	name := "Fix login loop"
	tests := []struct {
		name    string
		objName *string
		key     string
		objType string
		id      string
		want    string
	}{
		{name: "stored name wins", objName: &name, key: "human-key", objType: "Task", id: "0123456789", want: "Fix login loop"},
		{name: "human key is a label", key: "human-key", objType: "Task", id: "0123456789", want: "human-key"},
		{name: "current machine key is not a label", key: "task-0123456789ab", objType: "Task", id: "0123456789", want: "Task 012345"},
		{name: "legacy machine key is not a label", key: "task-01234567", objType: "Task", id: "0123456789", want: "Task 012345"},
		{name: "source-scoped key is not a label", key: "github:357590e8-6cbf-46bb-8c29-d8f839dd4ba8:org/repo#918", objType: "Task", id: "0123456789", want: "Task 012345"},
		{name: "no type falls back to the id", id: "0123456789", want: "01234567"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, objectLabel(tc.objName, tc.key, tc.objType, tc.id))
		})
	}
}
