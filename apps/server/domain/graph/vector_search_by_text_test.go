package graph

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// TestVectorSearchByTextDegradesWhenNotConfigured pins #682 item 3: a project
// with no usable embedding configuration yields an empty result set and no
// error, the way unified search degrades to lexical-only, because the only
// caller (the MCP similarity check) treats "no vectors" as "skip the check".
// A real provider failure must still surface.
func TestVectorSearchByTextDegradesWhenNotConfigured(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantErr     bool
		wantEmptyOK bool
	}{
		{
			name:        "no embedding model configured",
			err:         fmt.Errorf("embedding credential resolver failed: %w", embeddings.ErrEmbeddingModelNotSet),
			wantEmptyOK: true,
		},
		{
			name:        "unusable provider credential",
			err:         fmt.Errorf("embed query text: %w", embeddings.ErrEmbeddingCredentialUnusable),
			wantEmptyOK: true,
		},
		{
			name:        "no project in context",
			err:         embeddings.ErrNoProjectContext,
			wantEmptyOK: true,
		},
		{
			name:    "provider failure still surfaces",
			err:     errors.New("API error 429: quota exceeded"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{embeddings: &mockEmbeddingService{err: tt.err}}

			resp, err := svc.VectorSearchByText(context.Background(), uuid.New(), "entity text", &VectorSearchRequest{Limit: 1})

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				return
			}
			if !tt.wantEmptyOK {
				t.Fatalf("test case %q sets neither wantErr nor wantEmptyOK", tt.name)
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Empty(t, resp.Data)
			assert.Zero(t, resp.Total)
		})
	}
}
