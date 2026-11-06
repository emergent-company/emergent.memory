package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMergePreviewPackObjectTypeKeys pins the key conventions accepted by
// merge-preview. A snake_case key that the sibling pack endpoints accept must
// not bind to nothing and turn the collision gate into a silent "no conflicts".
func TestMergePreviewPackObjectTypeKeys(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantErr string
	}{
		{
			name:    "camelCase objectTypes",
			payload: `{"name":"p","objectTypes":[{"name":"Camel"}]}`,
			want:    `[{"name":"Camel"}]`,
		},
		{
			name:    "object_type_schemas array",
			payload: `{"name":"p","object_type_schemas":[{"name":"Long"}]}`,
			want:    `[{"name":"Long"}]`,
		},
		{
			name:    "object_types map",
			payload: `{"name":"p","object_types":{"Short":{"properties":{}}}}`,
			want:    `{"Short":{"properties":{}}}`,
		},
		{
			name:    "snake wins over camel when both present",
			payload: `{"name":"p","objectTypes":[{"name":"Camel"}],"object_type_schemas":[{"name":"Snake"}]}`,
			want:    `[{"name":"Snake"}]`,
		},
		{
			name:    "long snake wins over short snake when both present",
			payload: `{"name":"p","object_types":[{"name":"Short"}],"object_type_schemas":[{"name":"Long"}]}`,
			want:    `[{"name":"Long"}]`,
		},
		{
			name:    "unknown key is rejected",
			payload: `{"name":"p","objectTypesTypo":[{"name":"Typo"}]}`,
			wantErr: "unknown field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var pack MergePreviewPack
			err := json.Unmarshal([]byte(tc.payload), &pack)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error for %s", tc.payload)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := string(pack.GetObjectTypes()); got != tc.want {
				t.Errorf("GetObjectTypes() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestMergePreviewPackGetObjectTypesLiteral keeps the in-process blueprint path
// working: callers there construct the struct directly with ObjectTypes.
func TestMergePreviewPackGetObjectTypesLiteral(t *testing.T) {
	pack := MergePreviewPack{Name: "p", ObjectTypes: json.RawMessage(`[{"name":"Literal"}]`)}
	if got, want := string(pack.GetObjectTypes()), `[{"name":"Literal"}]`; got != want {
		t.Errorf("GetObjectTypes() = %s, want %s", got, want)
	}
}
