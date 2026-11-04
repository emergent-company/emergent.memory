package extraction

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPermanentEmbeddingError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "missing embedding model configured",
			err:  errors.New("no embedding model configured for project 1a3dece4-ca40-4ed3-b88c-80c554a4d058"),
			want: true,
		},
		{
			name: "credential resolver wraps missing model",
			err:  errors.New("embedding credential resolver failed: no embedding model configured for project 1a3dece4-ca40-4ed3-b88c-80c554a4d058 — run 'memory projects set-models --embedding ...'"),
			want: true,
		},
		{
			name: "API error 400",
			err:  errors.New("API error 400: invalid model name"),
			want: true,
		},
		{
			name: "API error 401",
			err:  errors.New("API error 401: invalid credentials"),
			want: true,
		},
		{
			name: "API error 403",
			err:  errors.New("API error 403: forbidden"),
			want: true,
		},
		{
			name: "API error 404",
			err:  errors.New("API error 404: model not found"),
			want: true,
		},
		{
			name: "rate limit is transient",
			err:  errors.New("API error 429: quota exceeded"),
			want: false,
		},
		{
			name: "server error is transient",
			err:  errors.New("API error 500: internal server error"),
			want: false,
		},
		{
			name: "generic network error is transient",
			err:  errors.New("dial tcp: connection refused"),
			want: false,
		},
		{
			name: "nil error is not permanent",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPermanentEmbeddingError(tt.err))
		})
	}
}
